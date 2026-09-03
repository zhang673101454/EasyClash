package main

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"easy-clash/backend"
)

// App 是 Wails 绑定到前端的应用入口。
type App struct {
	ctx          context.Context
	mu           sync.Mutex
	quitting     atomic.Bool
	compact      atomic.Bool
	manager      *backend.ProxyManager
	client       *backend.MihomoClient
	lastUp       int64
	lastDown     int64
	lastAt       time.Time
	lastMainW    int
	lastMainH    int
	warmupMu          sync.Mutex
	warmupCancel      context.CancelFunc
	autoSelectMu      sync.Mutex
	autoSelectCancel  context.CancelFunc
	reloadMu          sync.Mutex
	refreshMu         sync.Mutex
	refreshCancels    map[string]context.CancelFunc
	refreshGeneration map[string]uint64
	speedTestMu       sync.Mutex
	speedTestCancel   context.CancelFunc
	applyMu           sync.Mutex
	applyGeneration   uint64
}

// NewApp 创建应用实例。
func NewApp() *App {
	return &App{
		client: backend.NewMihomoClient(),
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	slog.Info("EasyClash 启动")
	a.startTray()

	manager, err := backend.NewProxyManager()
	if err != nil {
		slog.Error("初始化 ProxyManager 失败", "error", err)
		return
	}
	a.manager = manager
	manager.SetExitHandler(a.onMihomoUnexpectedExit)
	if err := backend.SyncProvidersConfig(manager.ConfigDir()); err != nil {
		slog.Warn("启动时同步订阅配置失败", "error", err)
	}
	if _, err := backend.SyncSubscriptionRoutingRules(manager.ConfigDir()); err != nil {
		slog.Warn("启动时同步订阅路由规则失败", "error", err)
	}
	// 异常退出可能留下指向 7890 的系统代理；启动时尚未连接，仅在仍是本软件代理时清理。
	if err := backend.DisableSystemProxyIfOurs(); err != nil {
		slog.Warn("启动时清理残留系统代理失败", "error", err)
	}
	backend.RefreshAutoStartCommand()
}

func ensureActiveSubscription(configDir string) error {
	items, err := backend.ListSubscriptions(configDir)
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.Enabled {
			return nil
		}
	}
	if len(items) == 0 {
		return fmt.Errorf("请先添加订阅")
	}
	_, err = backend.SetSubscriptionEnabled(configDir, items[0].ID, true)
	return err
}

func (a *App) onMihomoUnexpectedExit() {
	if a.quitting.Load() {
		return
	}
	slog.Error("mihomo 意外退出，正在关闭系统代理并同步状态")
	if err := backend.SetSystemProxy(false); err != nil {
		slog.Error("崩溃后关闭系统代理失败", "error", err)
		if forceErr := backend.ForceDisableEasyClashProxy(); forceErr != nil {
			slog.Error("崩溃后强制关闭系统代理失败", "error", forceErr)
		}
	}
	a.onProxyDisabled()
	status := a.decorateStatus(ProxyStatus{Connected: false, Message: "内核已退出，代理已关闭"})
	a.emitStatus(status)
}

func (a *App) shutdown(ctx context.Context) {
	slog.Info("EasyClash 正在关闭", "hasCtx", ctx != nil)
	a.quitting.Store(true)
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, err := a.disableLocked(); err != nil {
		slog.Error("退出时清理代理失败", "error", err)
	}
}

func (a *App) beforeClose(ctx context.Context) bool {
	if a.quitting.Load() {
		return false
	}
	runtime.WindowHide(ctx)
	return true
}

func (a *App) emitStatus(status ProxyStatus) {
	updateTrayProxyMenu(status.Connected)
	if a.ctx == nil {
		return
	}
	runtime.EventsEmit(a.ctx, "proxy:status", status)
}

// ProxyStatus 描述当前代理状态，供前端展示。
type ProxyStatus struct {
	Connected bool   `json:"connected"`
	NodeName  string `json:"nodeName"`
	LatencyMs int    `json:"latencyMs"`
	Message   string `json:"message"`
	Mode      string `json:"mode"`
	Tun       bool   `json:"tun"`
}

// TrafficInfo 侧边栏用的实时网速与延迟。
type TrafficInfo struct {
	Connected bool   `json:"connected"`
	NodeName  string `json:"nodeName"`
	LatencyMs int    `json:"latencyMs"`
	UpRate    int64  `json:"upRate"`
	DownRate  int64  `json:"downRate"`
}

// waitWithTimeout 在独立 goroutine 中执行 fn，超时后返回错误，避免 Wails 调用永久阻塞。
func waitWithTimeout[T any](timeout time.Duration, fn func() (T, error)) (T, error) {
	type result struct {
		value T
		err   error
	}
	ch := make(chan result, 1)
	go func() {
		v, err := fn()
		ch <- result{value: v, err: err}
	}()
	select {
	case r := <-ch:
		return r.value, r.err
	case <-time.After(timeout):
		var zero T
		return zero, fmt.Errorf("操作超时（超过 %s），请稍后重试", timeout)
	}
}

func (a *App) stopStartProxyWithTimeout(manager *backend.ProxyManager, tunMode bool, timeout time.Duration) error {
	ch := make(chan error, 1)
	go func() {
		ch <- a.stopStartProxy(manager, tunMode)
	}()
	select {
	case err := <-ch:
		return err
	case <-time.After(timeout):
		return fmt.Errorf("重载内核超过 %s", timeout)
	}
}

// ToggleProxy 开启或关闭代理（托盘使用；窗口内通过点击订阅控制）。
func (a *App) ToggleProxy() (ProxyStatus, error) {
	return waitWithTimeout(75*time.Second, a.toggleProxyInner)
}

func (a *App) toggleProxyInner() (ProxyStatus, error) {
	a.mu.Lock()
	if a.manager == nil {
		a.mu.Unlock()
		return ProxyStatus{}, fmt.Errorf("后端尚未初始化")
	}
	if a.manager.Running() {
		status, err := a.disableLocked()
		a.mu.Unlock()
		if err != nil {
			return status, err
		}
		a.emitStatus(status)
		return status, nil
	}
	configDir := a.manager.ConfigDir()
	a.mu.Unlock()

	if err := ensureActiveSubscription(configDir); err != nil {
		return ProxyStatus{Message: "未连接"}, err
	}
	status, err := a.enableWithoutAppLock()
	if err != nil {
		return status, err
	}
	a.emitStatus(status)
	a.onProxyEnabled()
	return status, nil
}

func (a *App) onProxyEnabled() {
	a.startWarmup()
	a.restartAutoSelectLoop()
}

func (a *App) onProxyDisabled() {
	a.stopWarmup()
	a.stopAutoSelectLoop()
}

func (a *App) restartAutoSelectLoop() {
	a.stopAutoSelectLoop()
	if a.manager == nil {
		return
	}
	s := backend.LoadSettings(a.manager.ConfigDir())
	if !s.AutoSelectBest {
		return
	}
	a.startAutoSelectLoop()
}

func (a *App) autoSelectInterval() time.Duration {
	if a.manager == nil {
		return time.Duration(backend.DefaultAutoSelectIntervalMin) * time.Minute
	}
	s := backend.LoadSettings(a.manager.ConfigDir())
	return time.Duration(s.AutoSelectIntervalMin) * time.Minute
}

func (a *App) invalidateBackgroundApply() {
	a.applyMu.Lock()
	a.applyGeneration++
	a.applyMu.Unlock()
}

func (a *App) managerRunning() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.manager != nil && a.manager.Running()
}

// GetStatus 返回当前连接状态。
func (a *App) GetStatus() (ProxyStatus, error) {
	return a.fetchStatus()
}

// AutoSelectBestNode 对节点组测速并切换到延迟最低的节点。
func (a *App) AutoSelectBestNode() (ProxyStatus, error) {
	a.cancelSpeedTest()

	a.speedTestMu.Lock()
	testCtx, cancel := context.WithTimeout(a.ctx, 45*time.Second)
	a.speedTestCancel = cancel
	a.speedTestMu.Unlock()
	defer func() {
		cancel()
		a.speedTestMu.Lock()
		a.speedTestCancel = nil
		a.speedTestMu.Unlock()
	}()

	a.mu.Lock()
	running := a.manager != nil && a.manager.Running()
	a.mu.Unlock()
	if !running {
		return ProxyStatus{Message: "未连接"}, fmt.Errorf("请先点击订阅开始使用")
	}

	waitCtx, waitCancel := context.WithTimeout(testCtx, 20*time.Second)
	err := a.client.WaitForProxyNodes(waitCtx)
	waitCancel()
	if err != nil {
		if testCtx.Err() != nil {
			return ProxyStatus{}, fmt.Errorf("测速已取消")
		}
		return ProxyStatus{}, fmt.Errorf("节点还在从订阅加载，请稍后再测")
	}

	sel, err := a.client.AutoSelectBest(testCtx)
	if err != nil {
		if testCtx.Err() != nil {
			return ProxyStatus{}, fmt.Errorf("测速已取消")
		}
		return ProxyStatus{}, err
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.manager == nil || !a.manager.Running() {
		return ProxyStatus{Message: "未连接"}, fmt.Errorf("请先点击订阅开始使用")
	}
	status := a.decorateStatus(connectedStatus(sel.Name, sel.Latency))
	a.emitStatus(status)
	return status, nil
}

func (a *App) cancelSpeedTest() {
	a.speedTestMu.Lock()
	defer a.speedTestMu.Unlock()
	if a.speedTestCancel != nil {
		a.speedTestCancel()
	}
}

func (a *App) enableLocked() (ProxyStatus, error) {
	if a.manager == nil {
		return ProxyStatus{}, fmt.Errorf("后端尚未初始化")
	}
	return a.enableWithManager(a.manager)
}

func (a *App) enableWithoutAppLock() (ProxyStatus, error) {
	a.mu.Lock()
	manager := a.manager
	a.mu.Unlock()
	if manager == nil {
		return ProxyStatus{}, fmt.Errorf("后端尚未初始化")
	}
	return a.enableWithManager(manager)
}

func (a *App) enableWithManager(manager *backend.ProxyManager) (ProxyStatus, error) {
	settings := backend.LoadSettings(manager.ConfigDir())
	if settings.Tun {
		if err := backend.CheckTunPrerequisites(); err != nil {
			return ProxyStatus{}, err
		}
	}
	if err := backend.ApplySettingsToConfig(manager.ConfigDir(), settings); err != nil {
		slog.Warn("写入运行设置失败", "error", err)
	}

	configDir := manager.ConfigDir()
	if err := backend.SyncProvidersConfig(configDir); err != nil {
		slog.Warn("启用前同步订阅配置失败", "error", err)
	}
	if id := enabledSubscriptionID(configDir); id != "" {
		if _, err := backend.ResanitizeProviderFile(configDir, id); err != nil {
			slog.Warn("启动前清洗订阅缓存失败", "id", id, "error", err)
		}
	}

	startCtx, startCancel := context.WithTimeout(a.ctx, 45*time.Second)
	defer startCancel()
	if err := manager.Start(startCtx); err != nil {
		return ProxyStatus{}, err
	}

	if settings.Tun {
		if err := backend.SetSystemProxy(false); err != nil {
			slog.Warn("TUN 模式下关闭系统代理失败", "error", err)
		}
		ok, tunErr := a.client.TunEnabled(a.ctx)
		if tunErr != nil || !ok {
			_ = manager.Stop()
			if tunErr != nil {
				return ProxyStatus{}, fmt.Errorf("TUN 未生效: %w", tunErr)
			}
			return ProxyStatus{}, fmt.Errorf("TUN 未生效，请以管理员运行并确认已放置 wintun.dll")
		}
	} else if err := backend.SetSystemProxy(true); err != nil {
		// 注册表可能已写入 7890，必须先清掉系统代理，避免停核后留下死代理。
		_ = backend.SetSystemProxy(false)
		_ = backend.ForceDisableEasyClashProxy()
		if stopErr := manager.Stop(); stopErr != nil {
			return ProxyStatus{}, fmt.Errorf("开启系统代理失败: %v；回滚停止 mihomo 也失败: %w", err, stopErr)
		}
		return ProxyStatus{}, fmt.Errorf("开启系统代理失败: %w", err)
	}

	if err := a.client.PatchMode(a.ctx, "rule"); err != nil {
		slog.Warn("同步规则模式失败", "error", err)
	}

	if id := enabledSubscriptionID(configDir); id != "" {
		switchCtx, switchCancel := context.WithTimeout(context.Background(), 30*time.Second)
		if err := a.client.SwitchSubscription(switchCtx, id); err != nil {
			slog.Warn("启动后切换订阅失败", "id", id, "error", err)
		}
		switchCancel()
	}

	status, statusErr := a.fetchStatus()
	if statusErr != nil {
		return a.decorateStatus(ProxyStatus{Connected: true, Message: "已连接"}), nil
	}
	if status.NodeName == "" || status.NodeName == "DIRECT" {
		slog.Warn("启动后仍未选中节点", "message", status.Message)
	}
	return status, nil
}

func (a *App) disableLocked() (ProxyStatus, error) {
	a.onProxyDisabled()
	var firstErr error
	if err := backend.SetSystemProxy(false); err != nil {
		slog.Error("关闭系统代理失败", "error", err)
		if forceErr := backend.ForceDisableEasyClashProxy(); forceErr != nil {
			slog.Error("强制关闭系统代理失败", "error", forceErr)
			firstErr = err
		}
	}
	if a.manager != nil {
		if err := a.manager.Stop(); err != nil {
			slog.Error("停止 mihomo 失败", "error", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	if firstErr != nil {
		return a.decorateStatus(ProxyStatus{Message: "未连接"}), firstErr
	}
	return a.decorateStatus(ProxyStatus{Connected: false, Message: "未连接"}), nil
}

func (a *App) statusLocked() (ProxyStatus, error) {
	return a.fetchStatus()
}

func enabledSubscriptionID(configDir string) string {
	items, err := backend.ListSubscriptions(configDir)
	if err != nil {
		return ""
	}
	for _, item := range items {
		if item.Enabled {
			return item.ID
		}
	}
	return ""
}

func (a *App) fetchStatus() (ProxyStatus, error) {
	if !a.managerRunning() {
		return a.decorateStatus(ProxyStatus{Connected: false, Message: "未连接"}), nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sel, err := a.client.CurrentNode(ctx)
	if err != nil {
		slog.Warn("读取当前节点失败", "error", err)
		return a.decorateStatus(ProxyStatus{Connected: true, Message: "已连接"}), nil
	}
	return a.decorateStatus(connectedStatus(sel.Name, sel.Latency)), nil
}

// Subscription 前端订阅项（兼容旧引用）。
type Subscription = SubscriptionItem

// SubscriptionItem 含流量配额的订阅项。
type SubscriptionItem struct {
	ID        string `json:"id"`
	URL       string `json:"url"`
	Remark    string `json:"remark"`
	Enabled   bool   `json:"enabled"`
	Upload    int64  `json:"upload"`
	Download  int64  `json:"download"`
	Total     int64  `json:"total"`
	Expire    int64  `json:"expire"`
	UpdatedAt int64  `json:"updatedAt"`
}

// ProxyNode 前端节点项。
type ProxyNode = backend.ProxyNode

func (a *App) reloadLocked() error {
	if a.manager == nil || !a.manager.Running() {
		return nil
	}
	settings := backend.LoadSettings(a.manager.ConfigDir())
	if err := backend.ApplySettingsToConfig(a.manager.ConfigDir(), settings); err != nil {
		slog.Warn("重载前写入运行设置失败", "error", err)
	}
	tunMode := settings.Tun
	manager := a.manager
	a.mu.Unlock()
	err := a.stopStartProxyWithTimeout(manager, tunMode, 55*time.Second)
	a.mu.Lock()
	return err
}

func (a *App) stopStartProxy(manager *backend.ProxyManager, tunMode bool) error {
	deadline := time.Now().Add(45 * time.Second)
	for {
		if a.reloadMu.TryLock() {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("等待内核重载锁超时")
		}
		time.Sleep(50 * time.Millisecond)
	}
	defer a.reloadMu.Unlock()

	slog.Info("重载内核：关闭系统代理")
	if err := backend.SetSystemProxy(false); err != nil {
		slog.Warn("重载前关闭系统代理失败", "error", err)
		_ = backend.ForceDisableEasyClashProxy()
	}
	slog.Info("重载内核：停止 mihomo")
	if err := manager.Stop(); err != nil {
		_ = backend.SetSystemProxy(false)
		_ = backend.ForceDisableEasyClashProxy()
		return fmt.Errorf("停止内核失败: %w", err)
	}
	slog.Info("重载内核：启动 mihomo")
	startCtx, startCancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer startCancel()
	if err := manager.Start(startCtx); err != nil {
		_ = backend.SetSystemProxy(false)
		_ = backend.ForceDisableEasyClashProxy()
		return fmt.Errorf("重新启动失败: %w", err)
	}
	a.client.InvalidateDelayCache()
	if tunMode {
		if err := backend.SetSystemProxy(false); err != nil {
			slog.Warn("TUN 模式下关闭系统代理失败", "error", err)
			_ = backend.ForceDisableEasyClashProxy()
		}
	} else if err := backend.SetSystemProxy(true); err != nil {
		_ = backend.SetSystemProxy(false)
		_ = backend.ForceDisableEasyClashProxy()
		_ = manager.Stop()
		return fmt.Errorf("重载后开启系统代理失败: %w", err)
	}
	return nil
}

func (a *App) startWarmup() {
	a.warmupMu.Lock()
	if a.warmupCancel != nil {
		a.warmupCancel()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	a.warmupCancel = cancel
	a.warmupMu.Unlock()
	go a.runWarmup(ctx)
}

func (a *App) stopWarmup() {
	a.warmupMu.Lock()
	if a.warmupCancel != nil {
		a.warmupCancel()
		a.warmupCancel = nil
	}
	a.warmupMu.Unlock()
}

func (a *App) startAutoSelectLoop() {
	a.autoSelectMu.Lock()
	if a.autoSelectCancel != nil {
		a.autoSelectCancel()
	}
	if a.ctx == nil {
		a.autoSelectMu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.autoSelectCancel = cancel
	a.autoSelectMu.Unlock()
	go a.runAutoSelectLoop(ctx)
}

func (a *App) stopAutoSelectLoop() {
	a.autoSelectMu.Lock()
	if a.autoSelectCancel != nil {
		a.autoSelectCancel()
		a.autoSelectCancel = nil
	}
	a.autoSelectMu.Unlock()
}

func (a *App) runAutoSelectLoop(ctx context.Context) {
	ticker := time.NewTicker(a.autoSelectInterval())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if a.manager == nil {
				continue
			}
			s := backend.LoadSettings(a.manager.ConfigDir())
			if !s.AutoSelectBest {
				continue
			}
			a.autoSelectOnce(ctx, false, true)
		}
	}
}

func (a *App) autoSelectOnce(ctx context.Context, warnOnFail bool, requireImprovement bool) {
	if !a.managerRunning() {
		return
	}
	if !requireImprovement {
		if current, err := a.client.CurrentNode(ctx); err == nil && current.Name != "" && current.Name != "DIRECT" {
			slog.Debug("已有选中节点，跳过预热测速", "node", current.Name)
			return
		}
	}

	var sel backend.NodeSelection
	var switched bool
	var err error
	if requireImprovement {
		sel, switched, err = a.client.AutoSelectBestIfBetter(ctx, backend.AutoSelectImprovementMs)
	} else {
		sel, err = a.client.AutoSelectBest(ctx)
		switched = err == nil
	}

	if !a.managerRunning() {
		return
	}
	if err != nil {
		current, curErr := a.client.CurrentNode(ctx)
		alreadyConnected := curErr == nil && current.Name != "" && current.Name != "DIRECT"
		if alreadyConnected {
			slog.Debug("后台测速暂不可用但当前节点仍有效", "node", current.Name, "error", err)
		} else if warnOnFail {
			slog.Warn("后台自动测速失败", "error", err)
		} else {
			slog.Debug("自动选择最低延迟节点失败", "error", err)
		}
		if ensureErr := a.client.EnsureProxyNodeSelected(ctx); ensureErr != nil {
			slog.Debug("测速失败后回退默认节点也失败", "error", ensureErr)
		}
		if status, statusErr := a.fetchStatus(); statusErr == nil {
			a.emitStatus(status)
		}
		return
	}
	if !switched {
		slog.Debug("当前节点仍合适，跳过切换", "node", sel.Name, "latency", sel.Latency)
		return
	}
	status := a.decorateStatus(connectedStatus(sel.Name, sel.Latency))
	a.emitStatus(status)
	slog.Info("已自动切换到最低延迟节点", "node", sel.Name, "latency", sel.Latency)
}

func (a *App) runWarmup(ctx context.Context) {
	names := []string{}
	if a.manager != nil {
		if items, err := backend.ListSubscriptions(a.manager.ConfigDir()); err == nil {
			for _, item := range items {
				if item.Enabled {
					names = append(names, item.ID)
				}
			}
		}
	}
	if len(names) > 0 {
		if a.manager != nil {
			for _, name := range names {
				if removed, err := backend.ResanitizeProviderFile(a.manager.ConfigDir(), name); err != nil {
					slog.Warn("清洗订阅缓存失败", "id", name, "error", err)
				} else if removed > 0 {
					slog.Info("已修复本地订阅缓存", "id", name, "removed", removed)
				}
			}
		}
		if err := a.client.UpdateNamedProviders(ctx, names); err != nil {
			slog.Warn("刷新订阅提供者失败", "error", err)
		}
	} else if err := a.client.UpdateHTTPProviders(ctx); err != nil {
		slog.Warn("刷新订阅提供者失败", "error", err)
	}
	waitErr := a.client.WaitForProxyNodes(ctx)
	if waitErr != nil {
		slog.Warn("等待节点列表超时", "error", waitErr)
		a.emitProviderWarning(names)
	}

	if err := a.client.EnsureProxyNodeSelected(ctx); err != nil {
		slog.Debug("预热时切换默认节点失败", "error", err)
	}

	a.mu.Lock()
	running := a.manager != nil && a.manager.Running()
	a.mu.Unlock()
	if !running {
		return
	}
	status, statusErr := a.fetchStatus()
	if statusErr == nil {
		a.emitStatus(status)
	}
	hasNode := statusErr == nil && status.NodeName != "" && status.NodeName != "DIRECT"
	if waitErr != nil && !hasNode {
		return
	}
	if !hasNode {
		return
	}

	selectCtx := ctx
	if ctx.Err() != nil {
		var selectCancel context.CancelFunc
		selectCtx, selectCancel = context.WithTimeout(context.Background(), 45*time.Second)
		defer selectCancel()
	}
	a.autoSelectOnce(selectCtx, true, false)

	sel, err := a.client.CurrentNode(selectCtx)
	if err == nil && (sel.Name == "" || sel.Name == "DIRECT") {
		slog.Warn("当前仍为 DIRECT，再次尝试自动选节点")
		if ensureErr := a.client.EnsureProxyNodeSelected(ctx); ensureErr != nil {
			a.autoSelectOnce(ctx, true, false)
		}
	}
}

func (a *App) emitProviderWarning(providerNames []string) {
	if a.ctx == nil {
		return
	}
	ctx, cancel := context.WithTimeout(a.ctx, 5*time.Second)
	defer cancel()
	for _, name := range providerNames {
		count, err := a.client.CountProviderProxies(ctx, name)
		if err != nil {
			slog.Debug("检查订阅节点失败", "provider", name, "error", err)
			continue
		}
		if count > 0 {
			continue
		}
		msg := fmt.Sprintf("订阅 %s 未加载到节点，请尝试刷新或切换网络", name)
		if a.manager != nil {
			if _, err := backend.ResanitizeProviderFile(a.manager.ConfigDir(), name); err == nil {
				msg = fmt.Sprintf("订阅 %s 节点加载失败，已尝试修复缓存，请点刷新或重新切换", name)
			}
		}
		runtime.EventsEmit(a.ctx, "proxy:error", msg)
	}
}

func (a *App) decorateStatus(status ProxyStatus) ProxyStatus {
	if a.manager == nil {
		if status.Mode == "" {
			status.Mode = "rule"
		}
		return status
	}
	s := backend.LoadSettings(a.manager.ConfigDir())
	status.Mode = s.Mode
	status.Tun = s.Tun
	return status
}

func (a *App) configDir() (string, error) {
	if a.manager != nil {
		return a.manager.ConfigDir(), nil
	}
	return backend.DefaultConfigDir()
}

func subscriptionItemsWithTraffic(configDir string, items []backend.Subscription) ([]SubscriptionItem, error) {
	cache, err := backend.LoadSubscriptionTrafficCache(configDir)
	if err != nil {
		cache = map[string]backend.SubscriptionTraffic{}
	}
	out := make([]SubscriptionItem, 0, len(items))
	for _, item := range items {
		view := SubscriptionItem{
			ID:      item.ID,
			URL:     item.URL,
			Remark:  item.Remark,
			Enabled: item.Enabled,
		}
		if traffic, ok := cache[item.ID]; ok {
			view.Upload = traffic.Upload
			view.Download = traffic.Download
			view.Total = traffic.Total
			view.Expire = traffic.Expire
			view.UpdatedAt = traffic.UpdatedAt
		}
		out = append(out, view)
	}
	return out, nil
}

// GetSubscriptions 返回订阅列表。
func (a *App) GetSubscriptions() ([]SubscriptionItem, error) {
	dir, err := a.configDir()
	if err != nil {
		return []SubscriptionItem{}, err
	}
	items, err := backend.ListSubscriptions(dir)
	if err != nil {
		return []SubscriptionItem{}, err
	}
	return subscriptionItemsWithTraffic(dir, items)
}

// SubscriptionRefreshEvent 订阅刷新完成（含后台应用节点）通知。
type SubscriptionRefreshEvent struct {
	ID        string `json:"id"`
	OK        bool   `json:"ok"`
	Error     string `json:"error,omitempty"`
	Upload    int64  `json:"upload,omitempty"`
	Download  int64  `json:"download,omitempty"`
	Total     int64  `json:"total,omitempty"`
	Expire    int64  `json:"expire,omitempty"`
	UpdatedAt int64  `json:"updatedAt,omitempty"`
}

func (a *App) emitSubscriptionRefreshed(event SubscriptionRefreshEvent) {
	if a.ctx == nil {
		return
	}
	runtime.EventsEmit(a.ctx, "subscription:refreshed", event)
}

// RefreshSubscriptionTraffic 刷新指定订阅的流量与节点缓存（可走当前已开启的代理）。
func (a *App) RefreshSubscriptionTraffic(id string) (SubscriptionItem, error) {
	proxyRunning := a.managerRunning()
	slog.Info("开始刷新订阅", "id", id, "proxyUp", proxyRunning || backend.LocalMixedProxyAvailable())
	ctx, endRefresh := a.beginSubscriptionRefresh(id)
	defer endRefresh()

	dir, err := a.configDir()
	if err != nil {
		slog.Warn("刷新订阅失败", "id", id, "error", err)
		return SubscriptionItem{}, err
	}
	a.prepareSubscriptionRefreshRouting(dir)

	if proxyRunning {
		if removed, resErr := backend.ResanitizeProviderFile(dir, id); resErr != nil {
			slog.Warn("刷新前清洗订阅缓存失败", "id", id, "error", resErr)
		} else if removed > 0 {
			slog.Info("刷新前已清洗无效节点", "id", id, "removed", removed)
			ensureCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
			_ = a.client.UpdateNamedProviders(ensureCtx, []string{id})
			cancel()
		}
		ensureCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		if ensureErr := a.client.EnsureProxyNodeSelected(ensureCtx); ensureErr != nil {
			slog.Warn("刷新前切换可用节点失败", "id", id, "error", ensureErr)
		}
		cancel()
	}

	slog.Info("正在拉取订阅内容", "id", id)
	result, err := backend.RefreshSubscriptionTraffic(ctx, dir, id, backend.SubscriptionFetchOptions{
		PreferProxy: proxyRunning,
	})
	if err != nil {
		if ctx.Err() != nil {
			slog.Info("刷新订阅已取消", "id", id)
			return SubscriptionItem{}, fmt.Errorf("已取消刷新")
		}
		slog.Warn("刷新订阅失败", "id", id, "error", err)
		return SubscriptionItem{}, err
	}

	item, err := a.subscriptionItemByID(dir, id, result.Traffic)
	if err != nil {
		slog.Warn("刷新订阅后读取条目失败", "id", id, "error", err)
		return SubscriptionItem{}, err
	}

	var applyLater bool
	if result.NodesSaved && a.managerRunning() {
		items, listErr := backend.ListSubscriptions(dir)
		if listErr == nil {
			for _, sub := range items {
				if sub.ID == id && sub.Enabled {
					applyLater = true
					break
				}
			}
		}
	}

	slog.Info("订阅拉取完成", "id", id, "applyLater", applyLater, "bytes", item.UpdatedAt)
	if applyLater {
		go a.runRefreshApply(id, dir, result.Traffic)
	} else {
		a.emitSubscriptionRefreshed(subscriptionRefreshEventFromItem(item, true, ""))
		slog.Info("刷新订阅完成（未运行代理，跳过热更新）", "id", id)
	}

	return item, nil
}

func subscriptionRefreshEventFromItem(item SubscriptionItem, ok bool, errMsg string) SubscriptionRefreshEvent {
	return SubscriptionRefreshEvent{
		ID:        item.ID,
		OK:        ok,
		Error:     errMsg,
		Upload:    item.Upload,
		Download:  item.Download,
		Total:     item.Total,
		Expire:    item.Expire,
		UpdatedAt: item.UpdatedAt,
	}
}

func (a *App) runRefreshApply(id, dir string, traffic backend.SubscriptionTraffic) {
	a.applyMu.Lock()
	a.applyGeneration++
	gen := a.applyGeneration
	a.applyMu.Unlock()

	slog.Info("开始应用刷新到运行中的代理", "id", id)
	a.stopWarmup()
	a.stopAutoSelectLoop()
	applyErr := a.applyRefreshToRunningProxy(id)

	a.applyMu.Lock()
	stale := a.applyGeneration != gen
	a.applyMu.Unlock()

	item, err := a.subscriptionItemByID(dir, id, traffic)
	if err != nil {
		slog.Warn("应用刷新后读取条目失败", "id", id, "error", err)
		a.emitSubscriptionRefreshed(SubscriptionRefreshEvent{ID: id, OK: false, Error: err.Error()})
		return
	}
	event := subscriptionRefreshEventFromItem(item, true, "")
	if stale {
		slog.Info("应用刷新已被更新的操作取代", "id", id)
		a.emitSubscriptionRefreshed(event)
		return
	}
	if applyErr != nil {
		event.OK = false
		event.Error = applyErr.Error()
		slog.Warn("应用刷新到代理失败", "id", id, "error", applyErr)
	} else {
		slog.Info("刷新订阅完成（已热更新节点）", "id", id)
	}
	if !stale && applyErr == nil {
		a.startWarmup()
	}
	a.emitSubscriptionRefreshed(event)
}

// CancelAllSubscriptionRefresh 取消全部进行中的订阅刷新。
func (a *App) CancelAllSubscriptionRefresh() {
	a.refreshMu.Lock()
	defer a.refreshMu.Unlock()
	for _, cancel := range a.refreshCancels {
		cancel()
	}
}

// CancelRefreshSubscriptionTraffic 取消指定订阅的刷新。
func (a *App) CancelRefreshSubscriptionTraffic(id string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	a.refreshMu.Lock()
	defer a.refreshMu.Unlock()
	if a.refreshCancels == nil {
		return
	}
	if cancel, ok := a.refreshCancels[id]; ok {
		cancel()
	}
}

func (a *App) beginSubscriptionRefresh(id string) (context.Context, func()) {
	a.refreshMu.Lock()
	if a.refreshCancels == nil {
		a.refreshCancels = map[string]context.CancelFunc{}
	}
	if a.refreshGeneration == nil {
		a.refreshGeneration = map[string]uint64{}
	}
	if old, ok := a.refreshCancels[id]; ok {
		old()
	}
	ctx, cancel := context.WithTimeout(a.ctx, 40*time.Second)
	a.refreshGeneration[id]++
	gen := a.refreshGeneration[id]
	a.refreshCancels[id] = cancel
	a.refreshMu.Unlock()

	cleanup := func() {
		a.refreshMu.Lock()
		if a.refreshGeneration[id] == gen {
			delete(a.refreshCancels, id)
		}
		a.refreshMu.Unlock()
	}
	return ctx, cleanup
}

func (a *App) prepareSubscriptionRefreshRouting(configDir string) {
	changed, err := backend.SyncSubscriptionRoutingRules(configDir)
	if err != nil {
		slog.Warn("同步订阅路由规则失败", "error", err)
		return
	}
	a.mu.Lock()
	running := a.manager != nil && a.manager.Running()
	a.mu.Unlock()
	if !running {
		return
	}
	if !changed {
		return
	}
	slog.Info("已更新订阅路由规则，热重载中")
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := a.client.ReloadConfigFromDisk(ctx); err != nil {
		slog.Warn("热重载路由规则失败", "error", err)
		return
	}
	slog.Debug("已热重载订阅路由规则")
}

func (a *App) applyRefreshToRunningProxy(id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	if !a.managerRunning() {
		slog.Info("代理未运行，跳过刷新应用", "id", id)
		return nil
	}

	// 刷新后优先热更新 provider，避免 force 重载整份配置（会长时间阻塞 API）
	if err := a.client.UpdateNamedProviders(ctx, []string{id}); err != nil {
		slog.Warn("刷新后更新 provider 失败", "id", id, "error", err)
	}
	count, countErr := a.client.CountProviderProxies(ctx, id)
	slog.Info("刷新后 provider 节点数", "id", id, "count", count, "countErr", countErr)
	if countErr == nil && count > 0 {
		if err := a.client.EnsureProxyNodeSelected(ctx); err != nil {
			slog.Warn("刷新后切换默认节点失败", "id", id, "error", err)
		}
		return nil
	}

	slog.Info("热更新未生效，回退完整重载", "id", id)
	if err := a.performReload(); err != nil {
		return fmt.Errorf("热更新失败且重载代理失败: %w", err)
	}
	return nil
}

func (a *App) applyRunningSubscriptionSwitch(configDir string, id string) error {
	a.stopWarmup()
	a.stopAutoSelectLoop()
	a.cancelSpeedTest()
	slog.Info("切换订阅", "id", id)

	if !a.managerRunning() {
		slog.Info("切换订阅时 mihomo 未运行，改走完整启用")
		_, err := a.enableWithoutAppLock()
		return err
	}

	if _, err := backend.ResanitizeProviderFile(configDir, id); err != nil {
		slog.Warn("切换前清洗订阅缓存失败", "id", id, "error", err)
	}

	if err := a.ensureNestedConfigInMihomo(configDir); err != nil {
		slog.Warn("切换前迁移嵌套配置失败", "error", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	if err := a.client.UpdateNamedProviders(ctx, []string{id}); err != nil {
		slog.Warn("切换时更新 provider 失败", "id", id, "error", err)
	}
	if err := a.client.SwitchSubscription(ctx, id); err != nil {
		return fmt.Errorf("切换订阅失败: %w", err)
	}

	a.client.InvalidateDelayCache()
	if status, err := a.fetchStatus(); err == nil {
		a.emitStatus(status)
	}
	slog.Info("切换订阅完成", "id", id)
	return nil
}

// reloadConfigAfterBootstrap 首次拉取订阅后让运行中的 mihomo 加载新 provider/子组（仅 bootstrap 时用）。
func (a *App) reloadConfigAfterBootstrap() error {
	ch := make(chan error, 1)
	go func() {
		ch <- a.client.ReloadConfigFromDisk(context.Background())
	}()
	select {
	case err := <-ch:
		return err
	case <-time.After(20 * time.Second):
		return fmt.Errorf("热重载配置超过 20s")
	}
}

// ensureNestedConfigInMihomo 若内核仍为旧版 PROXY 结构，写盘并热重载一次。
func (a *App) ensureNestedConfigInMihomo(configDir string) error {
	if err := backend.SyncProvidersConfig(configDir); err != nil {
		return err
	}
	if !a.managerRunning() {
		return nil
	}
	items, err := backend.ListSubscriptions(configDir)
	if err != nil {
		return err
	}
	subIDs := make([]string, 0, len(items))
	for _, item := range items {
		subIDs = append(subIDs, item.ID)
	}
	checkCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if a.client.ProxyUsesNestedGroups(checkCtx, subIDs) {
		return nil
	}
	slog.Info("检测到旧版 PROXY 结构，热重载嵌套配置")
	return a.reloadConfigAfterBootstrap()
}

func (a *App) performReloadWithTimeout(timeout time.Duration) error {
	ch := make(chan error, 1)
	go func() {
		ch <- a.performReload()
	}()
	select {
	case err := <-ch:
		return err
	case <-time.After(timeout):
		return fmt.Errorf("重启内核超过 %s", timeout)
	}
}

func (a *App) performReload() error {
	a.mu.Lock()
	if a.manager == nil || !a.manager.Running() {
		slog.Info("跳过重载：mihomo 未在运行")
		a.mu.Unlock()
		return nil
	}
	settings := backend.LoadSettings(a.manager.ConfigDir())
	if err := backend.ApplySettingsToConfig(a.manager.ConfigDir(), settings); err != nil {
		slog.Warn("重载前写入运行设置失败", "error", err)
	}
	tunMode := settings.Tun
	manager := a.manager
	a.mu.Unlock()
	return a.stopStartProxy(manager, tunMode)
}

func (a *App) subscriptionItemByID(dir, id string, traffic backend.SubscriptionTraffic) (SubscriptionItem, error) {
	items, err := backend.ListSubscriptions(dir)
	if err != nil {
		return SubscriptionItem{}, err
	}
	for _, item := range items {
		if item.ID != id {
			continue
		}
		if traffic.UpdatedAt == 0 {
			if cached, ok := backend.GetSubscriptionTrafficCache(dir, id); ok {
				traffic = cached
			}
		}
		return SubscriptionItem{
			ID:        item.ID,
			URL:       item.URL,
			Remark:    item.Remark,
			Enabled:   item.Enabled,
			Upload:    traffic.Upload,
			Download:  traffic.Download,
			Total:     traffic.Total,
			Expire:    traffic.Expire,
			UpdatedAt: traffic.UpdatedAt,
		}, nil
	}
	return SubscriptionItem{}, fmt.Errorf("找不到该订阅")
}

// AddSubscription 新增订阅。
func (a *App) AddSubscription(rawURL string, remark string) ([]SubscriptionItem, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.manager == nil {
		return nil, fmt.Errorf("后端尚未初始化")
	}
	configDir := a.manager.ConfigDir()
	items, err := backend.AddSubscription(configDir, rawURL, remark)
	if err != nil {
		return []SubscriptionItem{}, err
	}
	for _, item := range items {
		if backend.SameSubscribeURL(item.URL, rawURL) {
			go func(id string) {
				ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
				defer cancel()
				if _, fetchErr := backend.RefreshSubscriptionTraffic(ctx, configDir, id, backend.SubscriptionFetchOptions{}); fetchErr != nil {
					slog.Debug("新订阅流量获取失败", "id", id, "error", fetchErr)
				}
			}(item.ID)
			break
		}
	}
	return subscriptionItemsWithTraffic(configDir, items)
}

// SetSubscriptionRemark 更新订阅备注。
func (a *App) SetSubscriptionRemark(id string, remark string) ([]SubscriptionItem, error) {
	return a.UpdateSubscription(id, "", remark)
}

// UpdateSubscription 更新订阅链接与备注。rawURL 为空时保留原链接。
func (a *App) UpdateSubscription(id string, rawURL string, remark string) ([]SubscriptionItem, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.manager == nil {
		return nil, fmt.Errorf("后端尚未初始化")
	}
	configDir := a.manager.ConfigDir()
	items, err := backend.ListSubscriptions(configDir)
	if err != nil {
		return []SubscriptionItem{}, err
	}
	var oldURL string
	wasEnabled := false
	for _, item := range items {
		if item.ID != id {
			continue
		}
		oldURL = item.URL
		wasEnabled = item.Enabled
		break
	}
	items, err = backend.UpdateSubscription(configDir, id, rawURL, remark, false)
	if err != nil {
		return []SubscriptionItem{}, err
	}
	urlChanged := rawURL != "" && !backend.SameSubscribeURL(oldURL, rawURL)
	if wasEnabled && urlChanged && a.manager.Running() {
		if err := a.reloadLocked(); err != nil {
			return []SubscriptionItem{}, fmt.Errorf("订阅已保存，但重载代理失败: %w", err)
		}
		go func(subID string) {
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
			defer cancel()
			if _, fetchErr := backend.RefreshSubscriptionTraffic(ctx, configDir, subID, backend.SubscriptionFetchOptions{PreferProxy: true}); fetchErr != nil {
				slog.Debug("更新订阅后刷新流量失败", "id", subID, "error", fetchErr)
			}
		}(id)
	}
	return subscriptionItemsWithTraffic(configDir, items)
}

// RemoveSubscription 删除订阅。
func (a *App) RemoveSubscription(id string) ([]SubscriptionItem, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.manager == nil {
		return nil, fmt.Errorf("后端尚未初始化")
	}
	configDir := a.manager.ConfigDir()
	wasActive := false
	if current, listErr := backend.ListSubscriptions(configDir); listErr == nil {
		for _, item := range current {
			if item.ID == id && item.Enabled {
				wasActive = true
				break
			}
		}
	}
	items, err := backend.RemoveSubscription(configDir, id)
	if err != nil {
		return []SubscriptionItem{}, err
	}
	if wasActive && a.manager.Running() {
		status, stopErr := a.disableLocked()
		a.emitStatus(status)
		if stopErr != nil {
			out, mergeErr := subscriptionItemsWithTraffic(configDir, items)
			if mergeErr != nil {
				return []SubscriptionItem{}, fmt.Errorf("订阅已删除，但关闭代理失败: %w", stopErr)
			}
			return out, fmt.Errorf("订阅已删除，但关闭代理失败: %w", stopErr)
		}
	}
	return subscriptionItemsWithTraffic(configDir, items)
}

// UseSubscription 点击订阅：未在使用则独占启用并开启代理，再点同一条则关闭。
func (a *App) UseSubscription(id string) (ProxyStatus, error) {
	return waitWithTimeout(35*time.Second, func() (ProxyStatus, error) {
		return a.useSubscriptionInner(id)
	})
}

func (a *App) useSubscriptionInner(id string) (ProxyStatus, error) {
	a.CancelAllSubscriptionRefresh()
	a.cancelSpeedTest()
	a.stopWarmup()
	a.stopAutoSelectLoop()
	a.invalidateBackgroundApply()

	a.mu.Lock()
	if a.manager == nil {
		a.mu.Unlock()
		return ProxyStatus{}, fmt.Errorf("后端尚未初始化")
	}

	configDir := a.manager.ConfigDir()
	items, err := backend.ListSubscriptions(configDir)
	if err != nil {
		a.mu.Unlock()
		return ProxyStatus{}, err
	}
	var target *backend.Subscription
	for i := range items {
		if items[i].ID == id {
			target = &items[i]
			break
		}
	}
	if target == nil {
		a.mu.Unlock()
		return ProxyStatus{}, fmt.Errorf("找不到该订阅")
	}

	if target.Enabled && a.manager.Running() {
		if _, err := backend.SetSubscriptionEnabled(configDir, id, false); err != nil {
			a.mu.Unlock()
			return ProxyStatus{}, err
		}
		status, err := a.disableLocked()
		if err != nil {
			a.mu.Unlock()
			return status, err
		}
		a.emitStatus(status)
		a.mu.Unlock()
		return status, nil
	}

	prevEnabled := ""
	for _, item := range items {
		if item.Enabled {
			prevEnabled = item.ID
			break
		}
	}
	needsBootstrap := a.manager.Running() && backend.NeedsProviderBootstrap(configDir, id)
	wasRunning := a.manager.Running()
	a.mu.Unlock()

	if needsBootstrap {
		if _, syncErr := backend.SyncSubscriptionRoutingRules(configDir); syncErr != nil {
			slog.Warn("引导拉取前同步路由失败", "error", syncErr)
		}
		ctx, cancel := context.WithTimeout(a.ctx, 40*time.Second)
		_, err := backend.RefreshSubscriptionTraffic(ctx, configDir, id, backend.SubscriptionFetchOptions{PreferProxy: true})
		cancel()
		if err != nil {
			return ProxyStatus{}, fmt.Errorf("该订阅尚无节点缓存，拉取失败：%w。请先用其它订阅开代理，再点「刷新流量与节点」", err)
		}
		if wasRunning {
			if reloadErr := a.reloadConfigAfterBootstrap(); reloadErr != nil {
				slog.Warn("首次拉取后重载配置失败", "error", reloadErr)
			}
		}
	}

	a.mu.Lock()
	if a.manager == nil {
		a.mu.Unlock()
		return ProxyStatus{}, fmt.Errorf("后端尚未初始化")
	}
	configDir = a.manager.ConfigDir()

	if _, err := backend.SetSubscriptionEnabled(configDir, id, true); err != nil {
		a.mu.Unlock()
		return ProxyStatus{}, err
	}
	a.mu.Unlock()

	var switchErr error
	if wasRunning {
		switchErr = a.applyRunningSubscriptionSwitch(configDir, id)
	}

	if wasRunning && switchErr != nil {
		a.mu.Lock()
		if a.manager == nil {
			a.mu.Unlock()
			return ProxyStatus{}, fmt.Errorf("后端尚未初始化")
		}
		_, _ = backend.SetSubscriptionEnabled(configDir, id, false)
		if prevEnabled != "" && prevEnabled != id {
			if _, enableErr := backend.SetSubscriptionEnabled(configDir, prevEnabled, true); enableErr == nil {
				rbCtx, rbCancel := context.WithTimeout(context.Background(), 15*time.Second)
				if rbErr := a.client.SwitchSubscription(rbCtx, prevEnabled); rbErr != nil {
					slog.Warn("切换失败后回滚订阅也失败", "error", rbErr)
				}
				rbCancel()
			}
		} else {
			_ = backend.SetSystemProxy(false)
		}
		a.mu.Unlock()
		status := a.decorateStatus(ProxyStatus{Connected: false, Message: "切换失败，已恢复上一订阅"})
		a.emitStatus(status)
		return status, fmt.Errorf("切换订阅失败: %w", switchErr)
	}

	var status ProxyStatus
	if wasRunning {
		status, err = a.fetchStatus()
		if err != nil {
			status = a.decorateStatus(ProxyStatus{Connected: true, Message: "已连接"})
		}
	} else {
		status, err = a.enableWithoutAppLock()
		if err != nil {
			return status, err
		}
	}
	if latest, latestErr := a.fetchStatus(); latestErr == nil && latest.NodeName != "" && latest.NodeName != "DIRECT" {
		status = latest
	} else if status.Connected {
		ensureCtx, ensureCancel := context.WithTimeout(context.Background(), 25*time.Second)
		_ = a.client.WaitAndEnsureProxyNode(ensureCtx)
		ensureCancel()
		if latest, latestErr := a.fetchStatus(); latestErr == nil {
			status = latest
		}
	}
	a.emitStatus(status)
	a.onProxyEnabled()
	return status, nil
}

// GetNodes 返回可用节点列表。
func (a *App) GetNodes() ([]ProxyNode, error) {
	if !a.managerRunning() {
		return []ProxyNode{}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return a.client.ListNodes(ctx)
}

// SelectNode 切换当前节点。
func (a *App) SelectNode(name string) (ProxyStatus, error) {
	if strings.TrimSpace(name) == "" {
		return ProxyStatus{}, fmt.Errorf("节点名称不能为空")
	}
	if !a.managerRunning() {
		return ProxyStatus{Message: "未连接"}, fmt.Errorf("请先点击订阅开始使用")
	}
	ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
	defer cancel()
	nodeGroup := "PROXY"
	if a.manager != nil {
		if subID := enabledSubscriptionID(a.manager.ConfigDir()); subID != "" {
			nodeGroup = backend.SubscriptionGroupName(subID)
		}
	}
	if err := a.client.SwitchProxy(ctx, nodeGroup, name); err != nil {
		return ProxyStatus{}, err
	}
	status, err := a.fetchStatus()
	if err != nil {
		return ProxyStatus{}, err
	}
	a.emitStatus(status)
	return status, nil
}

func connectedStatus(name string, latency int) ProxyStatus {
	if name == "" || name == "DIRECT" {
		return ProxyStatus{
			Connected: true,
			NodeName:  "",
			Message:   "已连接 - 智能模式",
		}
	}
	if latency > 0 {
		return ProxyStatus{
			Connected: true,
			NodeName:  name,
			LatencyMs: latency,
			Message:   fmt.Sprintf("已连接 - %s (%dms)", name, latency),
		}
	}
	return ProxyStatus{
		Connected: true,
		NodeName:  name,
		Message:   fmt.Sprintf("已连接 - %s", name),
	}
}

// GetSettings 返回 TUN / 规则模式设置。
func (a *App) GetSettings() backend.AppSettings {
	if a.manager == nil {
		return backend.DefaultSettings()
	}
	return backend.LoadSettings(a.manager.ConfigDir())
}

// SetAutoSelectSettings 设置自动选最快节点及轮询间隔（分钟）。
func (a *App) SetAutoSelectSettings(enabled bool, intervalMin int) (backend.AppSettings, error) {
	if a.manager == nil {
		return backend.AppSettings{}, fmt.Errorf("后端尚未初始化")
	}
	s := backend.LoadSettings(a.manager.ConfigDir())
	s.AutoSelectBest = enabled
	s.AutoSelectIntervalMin = intervalMin
	if err := backend.SaveSettings(a.manager.ConfigDir(), s); err != nil {
		return backend.AppSettings{}, err
	}
	a.restartAutoSelectLoop()
	return backend.LoadSettings(a.manager.ConfigDir()), nil
}

// SetTunMode 开关 TUN。开启后走虚拟网卡，并关闭系统代理。
func (a *App) SetTunMode(enabled bool) (ProxyStatus, error) {
	return waitWithTimeout(90*time.Second, func() (ProxyStatus, error) {
		return a.setTunModeInner(enabled)
	})
}

func (a *App) setTunModeInner(enabled bool) (ProxyStatus, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.manager == nil {
		return ProxyStatus{}, fmt.Errorf("后端尚未初始化")
	}

	prev := backend.LoadSettings(a.manager.ConfigDir())
	if enabled {
		if err := backend.CheckTunPrerequisites(); err != nil {
			return ProxyStatus{}, err
		}
	}

	s := prev
	s.Tun = enabled
	s.Mode = "rule"
	if err := backend.SaveSettings(a.manager.ConfigDir(), s); err != nil {
		return ProxyStatus{}, err
	}
	if !a.manager.Running() {
		return a.statusLocked()
	}
	if err := a.reloadLocked(); err != nil {
		_ = backend.SaveSettings(a.manager.ConfigDir(), prev)
		_ = backend.SetSystemProxy(false)
		return ProxyStatus{}, fmt.Errorf("切换 TUN 失败: %w", err)
	}
	if enabled {
		ok, tunErr := a.client.TunEnabled(a.ctx)
		if tunErr != nil || !ok {
			_ = backend.SaveSettings(a.manager.ConfigDir(), prev)
			if rollbackErr := a.reloadLocked(); rollbackErr != nil {
				_ = backend.SetSystemProxy(false)
				return ProxyStatus{}, fmt.Errorf("TUN 未生效，回滚也失败: %v / %w", tunErr, rollbackErr)
			}
			if tunErr != nil {
				return ProxyStatus{}, fmt.Errorf("TUN 未生效（需管理员权限与 wintun.dll）: %w", tunErr)
			}
			return ProxyStatus{}, fmt.Errorf("TUN 未生效，请确认以管理员运行，并已放置 wintun.dll")
		}
	}
	if err := a.client.PatchMode(a.ctx, "rule"); err != nil {
		slog.Warn("同步规则模式失败", "error", err)
	}
	status, err := a.statusLocked()
	if err != nil {
		return ProxyStatus{}, err
	}
	a.emitStatus(status)
	a.startWarmup()
	return status, nil
}

// SetRuleMode 与 TUN 二选一：开启规则模式即关闭 TUN。
func (a *App) SetRuleMode(enabled bool) (ProxyStatus, error) {
	return a.SetTunMode(!enabled)
}

// GetAutoStart 是否开机自动启动。
func (a *App) GetAutoStart() (bool, error) {
	return backend.AutoStartEnabled()
}

// SetAutoStart 设置开机自动启动。
func (a *App) SetAutoStart(enabled bool) error {
	return backend.SetAutoStart(enabled)
}

// ShouldStartCompact 开机启动时默认进入悬浮球。
func (a *App) ShouldStartCompact() bool {
	return backend.LaunchedFromAutoStart()
}

// SetCompactMode 缩小为侧边悬浮球，或恢复主窗口。
func (a *App) SetCompactMode(compact bool) error {
	if a.ctx == nil {
		return fmt.Errorf("窗口尚未就绪")
	}
	const (
		mainW = 300
		mainH = 580
		minW  = 300
		minH  = 580
		miniW = 36
		miniH = 148
	)
	a.compact.Store(compact)
	if compact {
		w, h := runtime.WindowGetSize(a.ctx)
		if w >= minW && h >= minH {
			a.lastMainW = w
			a.lastMainH = h
		}
		runtime.WindowSetMaxSize(a.ctx, 0, 0)
		runtime.WindowSetMinSize(a.ctx, miniW, miniH)
		runtime.WindowSetSize(a.ctx, miniW, miniH)
		runtime.WindowSetMaxSize(a.ctx, miniW, miniH)
		runtime.WindowSetAlwaysOnTop(a.ctx, true)
		dockW, dockH := runtime.WindowGetSize(a.ctx)
		if dockW < miniW {
			dockW = miniW
		}
		if dockH < miniH {
			dockH = miniH
		}
		screens, err := runtime.ScreenGetAll(a.ctx)
		if err == nil {
			for _, screen := range screens {
				if !screen.IsCurrent {
					continue
				}
				x, y := compactDockPos(screen, dockW, dockH)
				runtime.WindowSetPosition(a.ctx, x, y)
				break
			}
		}
		return nil
	}
	restoreW, restoreH := a.lastMainW, a.lastMainH
	if restoreW < minW {
		restoreW = mainW
	}
	if restoreH < minH {
		restoreH = mainH
	}
	runtime.WindowSetAlwaysOnTop(a.ctx, false)
	runtime.WindowSetMaxSize(a.ctx, 0, 0)
	runtime.WindowSetMinSize(a.ctx, minW, minH)
	runtime.WindowSetSize(a.ctx, restoreW, restoreH)
	runtime.WindowCenter(a.ctx)
	return nil
}

// compactDockPos 把悬浮窗贴在当前屏幕右侧，并按物理像素换算，避免 DPI 下右半边出屏。
func compactDockPos(screen runtime.Screen, miniW, miniH int) (x, y int) {
	dipW, dipH := screen.Size.Width, screen.Size.Height
	physW, physH := screen.PhysicalSize.Width, screen.PhysicalSize.Height
	if dipW <= 0 {
		dipW = screen.Width
	}
	if dipH <= 0 {
		dipH = screen.Height
	}
	if physW <= 0 {
		physW = screen.Width
	}
	if physH <= 0 {
		physH = screen.Height
	}
	scaleX, scaleY := 1.0, 1.0
	if dipW > 0 && physW > dipW {
		scaleX = float64(physW) / float64(dipW)
	}
	if dipH > 0 && physH > dipH {
		scaleY = float64(physH) / float64(dipH)
	}
	physMiniW := int(math.Round(float64(miniW) * scaleX))
	physMiniH := int(math.Round(float64(miniH) * scaleY))
	margin := int(math.Round(40 * scaleX))
	if margin < 28 {
		margin = 28
	}
	x = physW - physMiniW - margin
	if x < 12 {
		x = 12
	}
	y = (physH - physMiniH) / 2
	if y < 48 {
		y = 48
	}
	return x, y
}

// GetTraffic 返回当前上下行速率。
func (a *App) GetTraffic() (TrafficInfo, error) {
	info := TrafficInfo{}
	if !a.managerRunning() {
		a.mu.Lock()
		a.lastUp, a.lastDown = 0, 0
		a.lastAt = time.Time{}
		a.mu.Unlock()
		return info, nil
	}

	if status, err := a.fetchStatus(); err == nil {
		info.Connected = status.Connected
		info.NodeName = status.NodeName
		info.LatencyMs = status.LatencyMs
	}

	ctx, cancel := context.WithTimeout(a.ctx, 3*time.Second)
	defer cancel()
	snap, err := a.client.ReadTraffic(ctx)
	if err != nil {
		return info, nil
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	if !a.lastAt.IsZero() {
		dt := now.Sub(a.lastAt).Seconds()
		if dt > 0.2 {
			upDelta := snap.UploadTotal - a.lastUp
			downDelta := snap.DownloadTotal - a.lastDown
			if upDelta < 0 {
				upDelta = 0
			}
			if downDelta < 0 {
				downDelta = 0
			}
			info.UpRate = int64(float64(upDelta) / dt)
			info.DownRate = int64(float64(downDelta) / dt)
		}
	}
	a.lastUp = snap.UploadTotal
	a.lastDown = snap.DownloadTotal
	a.lastAt = now
	return info, nil
}

