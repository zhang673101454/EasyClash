package backend

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"context"

	_ "embed"
)

//go:embed config.yaml
var defaultConfig []byte

// ProxyManager 负责 mihomo 进程的启动、停止与运行状态。
type ProxyManager struct {
	mu          sync.Mutex
	cmd         *exec.Cmd
	done        chan struct{}
	logFile     *os.File
	configDir   string
	binaryPath  string
	stopping    atomic.Bool
	exitHandler func()
}

// NewProxyManager 创建管理器，并准备配置目录。
func NewProxyManager() (*ProxyManager, error) {
	configDir, err := DefaultConfigDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建配置目录失败: %w", err)
	}
	if err := ensureConfig(configDir); err != nil {
		return nil, err
	}
	if err := normalizeConfigFile(configDir); err != nil {
		slog.Warn("规范化配置失败", "error", err)
	}
	if err := EnsureGeoDataInConfigDir(configDir); err != nil {
		slog.Warn("准备 geodata 失败", "error", err)
	}
	if err := ApplySettingsToConfig(configDir, LoadSettings(configDir)); err != nil {
		slog.Warn("应用运行设置失败", "error", err)
	}
	items, err := ListSubscriptions(configDir)
	if err != nil {
		slog.Warn("加载订阅列表失败", "error", err)
	} else if err := persistSubscriptions(configDir, items); err != nil {
		slog.Warn("同步订阅配置失败", "error", err)
	}

	killLeftoverMihomo(configDir)
	return &ProxyManager{configDir: configDir}, nil
}

// SetExitHandler 注册内核意外退出回调（预期 Stop 不会触发）。
func (m *ProxyManager) SetExitHandler(fn func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.exitHandler = fn
}

// Start 启动 mihomo，并等待本机 API 就绪。
func (m *ProxyManager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.runningLocked() {
		slog.Info("mihomo 已在运行")
		return nil
	}

	killLeftoverMihomo(m.configDir)

	bin, err := findMihomoBinary()
	if err != nil {
		return err
	}
	m.binaryPath = bin
	if err := EnsureWintunBeside(bin); err != nil {
		slog.Warn("准备 wintun.dll 失败", "error", err)
	}
	if err := EnsureGeoDataInConfigDir(m.configDir); err != nil {
		return fmt.Errorf("准备 geo 数据库失败: %w", err)
	}

	cmd := exec.Command(bin, "-d", m.configDir)
	logPath := filepath.Join(m.configDir, "mihomo.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		return fmt.Errorf("创建 mihomo 日志失败: %w", err)
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	prepareCommand(cmd)

	m.stopping.Store(false)
	slog.Info("启动 mihomo", "binary", bin, "configDir", m.configDir, "log", logPath)
	if err := cmd.Start(); err != nil {
		if closeErr := logFile.Close(); closeErr != nil {
			slog.Warn("关闭日志文件失败", "error", closeErr)
		}
		return fmt.Errorf("启动 mihomo 失败: %w", err)
	}

	done := make(chan struct{})
	go func(proc *exec.Cmd, doneCh chan struct{}) {
		waitErr := proc.Wait()
		if waitErr != nil {
			slog.Warn("mihomo 进程已退出", "error", waitErr)
		} else {
			slog.Info("mihomo 进程已退出")
		}
		close(doneCh)

		unexpected := !m.stopping.Load()
		m.mu.Lock()
		handler := m.exitHandler
		if m.done == doneCh {
			m.cmd = nil
			m.done = nil
			m.closeLogLocked()
			clearMihomoPID(m.configDir)
		}
		m.mu.Unlock()

		if unexpected && handler != nil {
			handler()
		}
	}(cmd, done)

	m.cmd = cmd
	m.done = done
	m.logFile = logFile
	writeMihomoPID(m.configDir, cmd.Process.Pid)

	waitCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	client := NewMihomoClient()
	if err := client.WaitReady(waitCtx); err != nil {
		hint := lastLogLines(logPath, 12)
		m.stopping.Store(true)
		stopErr := m.stopLocked()
		if stopErr != nil {
			return fmt.Errorf("等待 mihomo API 就绪失败: %v；停止进程也失败: %w；内核日志: %s", err, stopErr, hint)
		}
		return fmt.Errorf("等待 mihomo API 就绪失败: %w；内核日志: %s", err, hint)
	}

	slog.Info("mihomo 已就绪")
	return nil
}

// Stop 结束 mihomo 进程。
func (m *ProxyManager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopping.Store(true)
	return m.stopLocked()
}

func (m *ProxyManager) stopLocked() error {
	defer m.closeLogLocked()
	defer clearMihomoPID(m.configDir)
	if m.cmd == nil || m.cmd.Process == nil {
		killLeftoverMihomo(m.configDir)
		if mihomoAPIReachable() {
			return fmt.Errorf("仍有残留 mihomo 占用控制端口")
		}
		return nil
	}

	pid := m.cmd.Process.Pid
	slog.Info("停止 mihomo", "pid", pid)
	if err := m.cmd.Process.Kill(); err != nil {
		slog.Warn("结束 mihomo 进程时出错", "error", err)
	}

	timedOut := false
	if m.done != nil {
		select {
		case <-m.done:
		case <-time.After(1200 * time.Millisecond):
			timedOut = true
			slog.Warn("等待 mihomo 退出超时", "pid", pid)
		}
	}
	m.cmd = nil
	m.done = nil

	if timedOut {
		_ = terminateMihomoPID(pid)
		killLeftoverMihomo(m.configDir)
		if mihomoAPIReachable() {
			return fmt.Errorf("停止 mihomo 超时，进程可能仍在运行 (pid=%d)", pid)
		}
	}
	return nil
}

func (m *ProxyManager) closeLogLocked() {
	if m.logFile == nil {
		return
	}
	if err := m.logFile.Close(); err != nil {
		slog.Warn("关闭 mihomo 日志失败", "error", err)
	}
	m.logFile = nil
}

func lastLogLines(path string, n int) string {
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return "无内核日志"
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " | ")
}

// Running 返回 mihomo 是否仍在运行。
func (m *ProxyManager) Running() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.runningLocked()
}

func (m *ProxyManager) runningLocked() bool {
	if m.cmd == nil || m.cmd.Process == nil || m.done == nil {
		return false
	}
	select {
	case <-m.done:
		m.cmd = nil
		m.done = nil
		return false
	default:
		return true
	}
}

// ConfigDir 返回 mihomo 配置目录。
func (m *ProxyManager) ConfigDir() string {
	return m.configDir
}

// DefaultConfigDir 返回 EasyClash 配置目录，管理器尚未创建时也可读取订阅。
func DefaultConfigDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("获取用户配置目录失败: %w", err)
	}
	next := filepath.Join(dir, "EasyClash")
	legacy := filepath.Join(dir, "SimpleProxy")
	if _, err := os.Stat(next); err == nil {
		return next, nil
	}
	if info, err := os.Stat(legacy); err == nil && info.IsDir() {
		if renameErr := os.Rename(legacy, next); renameErr == nil {
			slog.Info("已将配置目录迁移到 EasyClash", "from", legacy, "to", next)
			return next, nil
		} else {
			slog.Warn("迁移配置目录失败，继续使用旧目录", "from", legacy, "to", next, "error", renameErr)
			return legacy, nil
		}
	}
	return next, nil
}

func ensureConfig(configDir string) error {
	path := filepath.Join(configDir, configFileName)
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("检查配置文件失败: %w", err)
	}

	if err := os.WriteFile(path, defaultConfig, 0o644); err != nil {
		return fmt.Errorf("写入默认配置失败: %w", err)
	}
	slog.Info("已写入默认 config.yaml", "path", path)
	return nil
}

func findMihomoBinary() (string, error) {
	names := []string{"mihomo", "mihomo.exe"}

	var searchDirs []string
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		searchDirs = append(searchDirs, exeDir, filepath.Join(exeDir, "resources"))
	} else {
		slog.Warn("获取可执行文件路径失败，将仅搜索工作目录与 PATH", "error", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		slog.Warn("获取工作目录失败", "error", err)
	} else {
		searchDirs = append(searchDirs, cwd, filepath.Join(cwd, "resources"))
	}

	for _, dir := range searchDirs {
		for _, name := range names {
			candidate := filepath.Join(dir, name)
			info, statErr := os.Stat(candidate)
			if statErr != nil || info.IsDir() {
				continue
			}
			slog.Info("找到 mihomo", "path", candidate)
			return candidate, nil
		}
	}

	path, lookErr := exec.LookPath("mihomo")
	if lookErr == nil {
		return path, nil
	}

	return "", fmt.Errorf("未找到 mihomo 可执行文件，请将其放到程序目录或 resources 文件夹")
}
