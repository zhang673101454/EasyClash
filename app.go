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
	warmupMu     sync.Mutex
	warmupCancel context.CancelFunc
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

	manager, err := backend.NewProxyManager()
	if err != nil {
		slog.Error("初始化 ProxyManager 失败", "error", err)
		return
	}
	a.manager = manager
	a.startTray()
	backend.RefreshAutoStartCommand()
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

// ToggleProxy 开启或关闭代理（托盘使用；窗口内通过点击订阅控制）。
func (a *App) ToggleProxy() (ProxyStatus, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.manager == nil {
		return ProxyStatus{}, fmt.Errorf("后端尚未初始化")
	}
	if a.manager.Running() {
		status, err := a.disableLocked()
		if err != nil {
			return status, err
		}
		a.emitStatus(status)
		return status, nil
	}
	items, err := backend.ListSubscriptions(a.manager.ConfigDir())
	if err != nil {
		return ProxyStatus{}, err
	}
	hasEnabled := false
	for _, item := range items {
		if item.Enabled {
			hasEnabled = true
			break
		}
	}
	if !hasEnabled {
		return ProxyStatus{Message: "未连接"}, fmt.Errorf("请先点击一个订阅开始使用")
	}
	status, err := a.enableLocked()
	if err != nil {
		return status, err
	}
	a.emitStatus(status)
	a.startWarmup()
	return status, nil
}

// GetStatus 返回当前连接状态。
func (a *App) GetStatus() (ProxyStatus, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.statusLocked()
}

// AutoSelectBestNode 对节点组测速并切换到延迟最低的节点。
func (a *App) AutoSelectBestNode() (ProxyStatus, error) {
	a.mu.Lock()
	running := a.manager != nil && a.manager.Running()
	a.mu.Unlock()
	if !running {
		return ProxyStatus{Message: "未连接"}, fmt.Errorf("请先点击订阅开始使用")
	}

	waitCtx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
	defer cancel()
	if err := a.client.WaitForProxyNodes(waitCtx); err != nil {
		return ProxyStatus{}, fmt.Errorf("节点还在从订阅加载，请稍后再测")
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.manager == nil || !a.manager.Running() {
		return ProxyStatus{Message: "未连接"}, fmt.Errorf("请先点击订阅开始使用")
	}

	sel, err := a.client.AutoSelectBest(a.ctx)
	if err != nil {
		return ProxyStatus{}, err
	}
	status := a.decorateStatus(connectedStatus(sel.Name, sel.Latency))
	a.emitStatus(status)
	return status, nil
}

func (a *App) enableLocked() (ProxyStatus, error) {
	settings := backend.LoadSettings(a.manager.ConfigDir())
	if err := backend.ApplySettingsToConfig(a.manager.ConfigDir(), settings); err != nil {
		slog.Warn("写入运行设置失败", "error", err)
	}

	if err := a.manager.Start(a.ctx); err != nil {
		return ProxyStatus{}, err
	}

	if settings.Tun {
		if err := backend.SetSystemProxy(false); err != nil {
			slog.Warn("TUN 模式下关闭系统代理失败", "error", err)
		}
	} else if err := backend.SetSystemProxy(true); err != nil {
		if stopErr := a.manager.Stop(); stopErr != nil {
			return ProxyStatus{}, fmt.Errorf("开启系统代理失败: %v；回滚停止 mihomo 也失败: %w", err, stopErr)
		}
		return ProxyStatus{}, fmt.Errorf("开启系统代理失败: %w", err)
	}

	if err := a.client.PatchMode(a.ctx, "rule"); err != nil {
		slog.Warn("同步规则模式失败", "error", err)
	}

	status, statusErr := a.statusLocked()
	if statusErr != nil {
		return a.decorateStatus(ProxyStatus{Connected: true, Message: "已连接"}), nil
	}
	return status, nil
}

func (a *App) disableLocked() (ProxyStatus, error) {
	var firstErr error
	if err := backend.SetSystemProxy(false); err != nil {
		slog.Error("关闭系统代理失败", "error", err)
		firstErr = err
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
	if a.manager == nil || !a.manager.Running() {
		return a.decorateStatus(ProxyStatus{Connected: false, Message: "未连接"}), nil
	}

	sel, err := a.client.CurrentNode(a.ctx)
	if err != nil {
		slog.Warn("读取当前节点失败", "error", err)
		return a.decorateStatus(ProxyStatus{Connected: true, Message: "已连接"}), nil
	}
	return a.decorateStatus(connectedStatus(sel.Name, sel.Latency)), nil
}

// Subscription 前端订阅项。
type Subscription = backend.Subscription

// ProxyNode 前端节点项。
type ProxyNode = backend.ProxyNode

func (a *App) reloadLocked() error {
	if a.manager == nil || !a.manager.Running() {
		return nil
	}
	if err := a.manager.Stop(); err != nil {
		return fmt.Errorf("停止内核失败: %w", err)
	}
	if err := a.manager.Start(a.ctx); err != nil {
		return fmt.Errorf("重新启动失败: %w", err)
	}
	return nil
}

func (a *App) startWarmup() {
	a.warmupMu.Lock()
	if a.warmupCancel != nil {
		a.warmupCancel()
	}
	if a.ctx == nil {
		a.warmupMu.Unlock()
		return
	}
	ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
	a.warmupCancel = cancel
	a.warmupMu.Unlock()
	go a.runWarmup(ctx)
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
		if err := a.client.UpdateNamedProviders(ctx, names); err != nil {
			slog.Warn("刷新订阅提供者失败", "error", err)
		}
	} else if err := a.client.UpdateHTTPProviders(ctx); err != nil {
		slog.Warn("刷新订阅提供者失败", "error", err)
	}
	if err := a.client.WaitForProxyNodes(ctx); err != nil {
		slog.Warn("等待节点列表超时", "error", err)
		return
	}

	a.mu.Lock()
	if a.manager != nil && a.manager.Running() {
		if status, err := a.statusLocked(); err == nil {
			a.emitStatus(status)
		}
	}
	running := a.manager != nil && a.manager.Running()
	a.mu.Unlock()
	if !running {
		return
	}

	sel, err := a.client.AutoSelectBest(ctx)

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.manager == nil || !a.manager.Running() {
		return
	}
	if err != nil {
		slog.Warn("后台自动测速失败", "error", err)
		if status, statusErr := a.statusLocked(); statusErr == nil {
			a.emitStatus(status)
		}
		return
	}
	status := a.decorateStatus(connectedStatus(sel.Name, sel.Latency))
	a.emitStatus(status)
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

func cloneSubscriptions(items []backend.Subscription) []Subscription {
	if len(items) == 0 {
		return []Subscription{}
	}
	out := make([]Subscription, len(items))
	copy(out, items)
	return out
}

// GetSubscriptions 返回订阅列表。
func (a *App) GetSubscriptions() ([]Subscription, error) {
	dir, err := a.configDir()
	if err != nil {
		return []Subscription{}, err
	}
	items, err := backend.ListSubscriptions(dir)
	if err != nil {
		return []Subscription{}, err
	}
	return cloneSubscriptions(items), nil
}

// AddSubscription 新增订阅。
func (a *App) AddSubscription(rawURL string, remark string) ([]Subscription, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.manager == nil {
		return nil, fmt.Errorf("后端尚未初始化")
	}
	items, err := backend.AddSubscription(a.manager.ConfigDir(), rawURL, remark)
	if err != nil {
		return []Subscription{}, err
	}
	return cloneSubscriptions(items), nil
}

// SetSubscriptionRemark 更新订阅备注。
func (a *App) SetSubscriptionRemark(id string, remark string) ([]Subscription, error) {
	if a.manager == nil {
		return nil, fmt.Errorf("后端尚未初始化")
	}
	items, err := backend.SetSubscriptionRemark(a.manager.ConfigDir(), id, remark)
	if err != nil {
		return []Subscription{}, err
	}
	return cloneSubscriptions(items), nil
}

// RemoveSubscription 删除订阅。
func (a *App) RemoveSubscription(id string) ([]Subscription, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.manager == nil {
		return nil, fmt.Errorf("后端尚未初始化")
	}
	wasActive := false
	if current, listErr := backend.ListSubscriptions(a.manager.ConfigDir()); listErr == nil {
		for _, item := range current {
			if item.ID == id && item.Enabled {
				wasActive = true
				break
			}
		}
	}
	items, err := backend.RemoveSubscription(a.manager.ConfigDir(), id)
	if err != nil {
		return []Subscription{}, err
	}
	if wasActive && a.manager.Running() {
		status, stopErr := a.disableLocked()
		a.emitStatus(status)
		if stopErr != nil {
			return cloneSubscriptions(items), fmt.Errorf("订阅已删除，但关闭代理失败: %w", stopErr)
		}
	}
	return cloneSubscriptions(items), nil
}

// UseSubscription 点击订阅：未在使用则独占启用并开启代理，再点同一条则关闭。
func (a *App) UseSubscription(id string) (ProxyStatus, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.manager == nil {
		return ProxyStatus{}, fmt.Errorf("后端尚未初始化")
	}

	items, err := backend.ListSubscriptions(a.manager.ConfigDir())
	if err != nil {
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
		return ProxyStatus{}, fmt.Errorf("找不到该订阅")
	}

	if target.Enabled && a.manager.Running() {
		if _, err := backend.SetSubscriptionEnabled(a.manager.ConfigDir(), id, false); err != nil {
			return ProxyStatus{}, err
		}
		status, err := a.disableLocked()
		if err != nil {
			return status, err
		}
		a.emitStatus(status)
		return status, nil
	}

	if _, err := backend.SetSubscriptionEnabled(a.manager.ConfigDir(), id, true); err != nil {
		return ProxyStatus{}, err
	}

	var status ProxyStatus
	if a.manager.Running() {
		if err := a.reloadLocked(); err != nil {
			return ProxyStatus{}, fmt.Errorf("切换订阅失败: %w", err)
		}
		var statusErr error
		status, statusErr = a.statusLocked()
		if statusErr != nil {
			status = a.decorateStatus(ProxyStatus{Connected: true, Message: "已连接"})
		}
	} else {
		var err error
		status, err = a.enableLocked()
		if err != nil {
			return status, err
		}
	}
	a.emitStatus(status)
	a.startWarmup()
	return status, nil
}

// GetNodes 返回可用节点列表。
func (a *App) GetNodes() ([]ProxyNode, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.manager == nil || !a.manager.Running() {
		return []ProxyNode{}, nil
	}
	nodes, err := a.client.ListNodes(a.ctx)
	if err != nil {
		return nil, err
	}
	return nodes, nil
}

// SelectNode 切换当前节点。
func (a *App) SelectNode(name string) (ProxyStatus, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.manager == nil || !a.manager.Running() {
		return ProxyStatus{Message: "未连接"}, fmt.Errorf("请先点击订阅开始使用")
	}
	if strings.TrimSpace(name) == "" {
		return ProxyStatus{}, fmt.Errorf("节点名称不能为空")
	}
	if err := a.client.SwitchProxy(a.ctx, "PROXY", name); err != nil {
		return ProxyStatus{}, err
	}
	status, err := a.statusLocked()
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
			NodeName:  "DIRECT",
			Message:   "已连接 - 直连",
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
		return backend.AppSettings{Mode: "rule"}
	}
	return backend.LoadSettings(a.manager.ConfigDir())
}

// SetTunMode 开关 TUN。开启后走虚拟网卡，并关闭系统代理。
func (a *App) SetTunMode(enabled bool) (ProxyStatus, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.manager == nil {
		return ProxyStatus{}, fmt.Errorf("后端尚未初始化")
	}
	s := backend.LoadSettings(a.manager.ConfigDir())
	s.Tun = enabled
	s.Mode = "rule"
	if err := backend.SaveSettings(a.manager.ConfigDir(), s); err != nil {
		return ProxyStatus{}, err
	}
	if !a.manager.Running() {
		return a.statusLocked()
	}
	if err := a.reloadLocked(); err != nil {
		return ProxyStatus{}, fmt.Errorf("切换 TUN 失败: %w", err)
	}
	if enabled {
		_ = backend.SetSystemProxy(false)
	} else if err := backend.SetSystemProxy(true); err != nil {
		return ProxyStatus{}, fmt.Errorf("恢复系统代理失败: %w", err)
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
		mainH = 400
		minW  = 300
		minH  = 400
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
	a.mu.Lock()
	defer a.mu.Unlock()
	info := TrafficInfo{}
	if a.manager == nil || !a.manager.Running() {
		a.lastUp, a.lastDown = 0, 0
		a.lastAt = time.Time{}
		return info, nil
	}
	status, err := a.statusLocked()
	if err == nil {
		info.Connected = status.Connected
		info.NodeName = status.NodeName
		info.LatencyMs = status.LatencyMs
	}
	snap, err := a.client.ReadTraffic(a.ctx)
	if err != nil {
		return info, nil
	}
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

