package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

var skippedNodes = map[string]struct{}{
	"DIRECT":      {},
	"REJECT":      {},
	"REJECT-DROP": {},
	"PASS":        {},
	"COMPATIBLE":  {},
}

// MihomoClient 封装与 mihomo RESTful API 的交互。
type MihomoClient struct {
	baseURL    string
	httpClient *http.Client
}

type proxyInfo struct {
	Name    string         `json:"name"`
	Type    string         `json:"type"`
	Now     string         `json:"now"`
	All     []string       `json:"all"`
	History []delayHistory `json:"history"`
}

type delayHistory struct {
	Delay int `json:"delay"`
}

type proxiesResponse struct {
	Proxies map[string]proxyInfo `json:"proxies"`
}

// NodeSelection 表示当前或测速后选中的节点。
type NodeSelection struct {
	Name    string
	Latency int
}

// NewMihomoClient 创建指向本机 external-controller 的客户端。
func NewMihomoClient() *MihomoClient {
	return &MihomoClient{
		baseURL: fmt.Sprintf("http://%s:%d", apiHost, apiPort),
		httpClient: &http.Client{
			Timeout: 45 * time.Second,
		},
	}
}

// WaitReady 轮询 API，直到 mihomo 可以响应。
func (c *MihomoClient) WaitReady(ctx context.Context) error {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	var lastErr error
	for {
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return fmt.Errorf("mihomo API 未就绪: %w", lastErr)
			}
			return fmt.Errorf("mihomo API 未就绪: %w", ctx.Err())
		case <-ticker.C:
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/version", nil)
			if err != nil {
				lastErr = err
				continue
			}
			resp, err := c.httpClient.Do(req)
			if err != nil {
				lastErr = err
				continue
			}
			if err := resp.Body.Close(); err != nil {
				slog.Warn("关闭 version 响应失败", "error", err)
			}
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return nil
			}
			lastErr = fmt.Errorf("version 接口返回 HTTP %d", resp.StatusCode)
		}
	}
}

// GetProxies 获取全部代理与策略组。
func (c *MihomoClient) GetProxies(ctx context.Context) (map[string]proxyInfo, error) {
	var result proxiesResponse
	if err := c.doJSON(ctx, http.MethodGet, "/proxies", nil, &result); err != nil {
		return nil, fmt.Errorf("获取节点列表失败: %w", err)
	}
	if result.Proxies == nil {
		return nil, fmt.Errorf("获取节点列表失败: 响应为空")
	}
	return result.Proxies, nil
}

// TestGroupDelay 对指定策略组做并发延迟测试。
func (c *MihomoClient) TestGroupDelay(ctx context.Context, group string) (map[string]int, error) {
	path := fmt.Sprintf("/group/%s/delay?url=%s&timeout=%d",
		url.PathEscape(group),
		url.QueryEscape(delayTestURL),
		delayTimeoutMs,
	)
	var delays map[string]int
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &delays); err != nil {
		return nil, fmt.Errorf("测速组 %s 失败: %w", group, err)
	}
	return delays, nil
}

// SwitchProxy 将策略组切换到指定节点。
func (c *MihomoClient) SwitchProxy(ctx context.Context, group, node string) error {
	body, err := json.Marshal(map[string]string{"name": node})
	if err != nil {
		return fmt.Errorf("编码切换请求失败: %w", err)
	}
	path := "/proxies/" + url.PathEscape(group)
	if err := c.doJSON(ctx, http.MethodPut, path, body, nil); err != nil {
		return fmt.Errorf("切换节点失败 (组=%s, 节点=%s): %w", group, node, err)
	}
	slog.Info("已切换节点", "group", group, "node", node)
	return nil
}

// AutoSelectBest 对 PROXY 组测速并切换到延迟最低的节点。
func (c *MihomoClient) AutoSelectBest(ctx context.Context) (NodeSelection, error) {
	if err := c.ensureSelectableGroup(ctx); err != nil {
		return NodeSelection{}, err
	}

	delays, delayErr := c.collectProxyDelays(ctx)
	if delayErr != nil {
		return NodeSelection{}, delayErr
	}

	bestNode, bestDelay := pickBestNode(delays)
	if bestNode == "" {
		return NodeSelection{}, fmt.Errorf("当前订阅没有可测速的节点，请稍后再试")
	}

	if err := c.SwitchProxy(ctx, preferredGroup, bestNode); err != nil {
		return NodeSelection{}, err
	}
	return NodeSelection{Name: bestNode, Latency: bestDelay}, nil
}

// AutoSelectBestIfBetter 仅当最优节点比当前节点快至少 minImprovementMs 时才切换。
func (c *MihomoClient) AutoSelectBestIfBetter(ctx context.Context, minImprovementMs int) (NodeSelection, bool, error) {
	if err := c.ensureSelectableGroup(ctx); err != nil {
		return NodeSelection{}, false, err
	}

	current, err := c.CurrentNode(ctx)
	if err != nil {
		return NodeSelection{}, false, err
	}

	delays, delayErr := c.collectProxyDelays(ctx)
	if delayErr != nil {
		return NodeSelection{}, false, delayErr
	}

	bestNode, bestDelay := pickBestNode(delays)
	if bestNode == "" {
		return NodeSelection{}, false, fmt.Errorf("当前订阅没有可测速的节点，请稍后再试")
	}

	if bestNode == current.Name {
		return NodeSelection{Name: current.Name, Latency: bestDelay}, false, nil
	}

	if current.Name == "" || current.Name == "DIRECT" {
		if err := c.SwitchProxy(ctx, preferredGroup, bestNode); err != nil {
			return NodeSelection{}, false, err
		}
		return NodeSelection{Name: bestNode, Latency: bestDelay}, true, nil
	}

	// 仅用本轮测速结果判断；当前节点测不通时不能沿用历史延迟而拒绝切换。
	currentDelay := delays[current.Name]
	if currentDelay <= 0 {
		if err := c.SwitchProxy(ctx, preferredGroup, bestNode); err != nil {
			return NodeSelection{}, false, err
		}
		return NodeSelection{Name: bestNode, Latency: bestDelay}, true, nil
	}
	if bestDelay > 0 && currentDelay-bestDelay < minImprovementMs {
		return NodeSelection{Name: current.Name, Latency: currentDelay}, false, nil
	}

	if err := c.SwitchProxy(ctx, preferredGroup, bestNode); err != nil {
		return NodeSelection{}, false, err
	}
	return NodeSelection{Name: bestNode, Latency: bestDelay}, true, nil
}

func (c *MihomoClient) ensureSelectableGroup(ctx context.Context) error {
	nodes, err := c.ListNodes(ctx)
	if err == nil {
		for _, node := range nodes {
			if node.Name != "" && node.Name != "DIRECT" && node.Name != "REJECT" {
				return nil
			}
		}
	}
	proxies, err := c.GetProxies(ctx)
	if err != nil {
		return err
	}
	group, ok := proxies[preferredGroup]
	if !ok || !isSelectableGroup(group.Type) {
		return fmt.Errorf("当前订阅没有可测速的节点，请稍后再试")
	}
	return nil
}

func (c *MihomoClient) collectProxyDelays(ctx context.Context) (map[string]int, error) {
	if delays, err := c.TestGroupDelay(ctx, preferredGroup); err == nil {
		if _, bestDelay := pickBestNode(delays); bestDelay > 0 {
			return delays, nil
		}
	}
	delays, err := c.testAllNodeDelays(ctx)
	if err != nil {
		return nil, fmt.Errorf("测速失败: %w", err)
	}
	if _, bestDelay := pickBestNode(delays); bestDelay > 0 {
		return delays, nil
	}
	return nil, fmt.Errorf("当前订阅没有可测速的节点，请稍后再试")
}

func (c *MihomoClient) testAllNodeDelays(ctx context.Context) (map[string]int, error) {
	var result providersResponse
	if err := c.doJSON(ctx, http.MethodGet, "/providers/proxies", nil, &result); err != nil {
		return nil, err
	}

	type target struct {
		name         string
		providerName string
	}
	targets := make([]target, 0)
	for providerName, provider := range result.Providers {
		if !isHTTPProvider(provider.VehicleType) {
			continue
		}
		for _, proxy := range provider.Proxies {
			if proxy.Name == "" {
				continue
			}
			if _, skip := skippedNodes[proxy.Name]; skip {
				continue
			}
			targets = append(targets, target{name: proxy.Name, providerName: providerName})
		}
	}
	if len(targets) == 0 {
		nodes, err := c.ListNodes(ctx)
		if err != nil {
			return nil, err
		}
		for _, node := range nodes {
			if node.Name == "" {
				continue
			}
			if _, skip := skippedNodes[node.Name]; skip {
				continue
			}
			targets = append(targets, target{name: node.Name})
		}
	}
	if len(targets) == 0 {
		return map[string]int{}, nil
	}

	delays := make(map[string]int, len(targets))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)

	for _, item := range targets {
		wg.Add(1)
		go func(item target) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			delay, err := c.testSingleProxyDelay(ctx, item.name)
			if err != nil && item.providerName != "" {
				delay, err = c.testProviderProxyDelay(ctx, item.providerName, item.name)
			}
			if err != nil || delay <= 0 {
				return
			}
			mu.Lock()
			delays[item.name] = delay
			mu.Unlock()
		}(item)
	}
	wg.Wait()
	return delays, nil
}

func (c *MihomoClient) testSingleProxyDelay(ctx context.Context, name string) (int, error) {
	path := fmt.Sprintf("/proxies/%s/delay?url=%s&timeout=%d",
		url.PathEscape(name),
		url.QueryEscape(delayTestURL),
		delayTimeoutMs,
	)
	var resp struct {
		Delay int `json:"delay"`
	}
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return 0, err
	}
	return resp.Delay, nil
}

func (c *MihomoClient) testProviderProxyDelay(ctx context.Context, providerName, proxyName string) (int, error) {
	path := fmt.Sprintf("/providers/proxies/%s/%s/healthcheck?url=%s&timeout=%d",
		url.PathEscape(providerName),
		url.PathEscape(proxyName),
		url.QueryEscape(delayTestURL),
		delayTimeoutMs,
	)
	var resp struct {
		Delay int `json:"delay"`
	}
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return 0, err
	}
	return resp.Delay, nil
}

func pickBestNode(delays map[string]int) (string, int) {
	if best, delay := pickBestNodeMaxDelay(delays, delayMaxUsableMs); best != "" {
		return best, delay
	}
	return pickBestNodeMaxDelay(delays, 0)
}

func pickBestNodeMaxDelay(delays map[string]int, maxDelay int) (string, int) {
	bestNode := ""
	bestDelay := 0
	for node, delay := range delays {
		if _, skip := skippedNodes[node]; skip {
			continue
		}
		if delay <= 0 {
			continue
		}
		if maxDelay > 0 && delay > maxDelay {
			continue
		}
		if bestNode == "" || delay < bestDelay {
			bestNode = node
			bestDelay = delay
		}
	}
	return bestNode, bestDelay
}

// CurrentNode 读取当前选中节点及最近一次延迟。
func (c *MihomoClient) CurrentNode(ctx context.Context) (NodeSelection, error) {
	proxies, err := c.GetProxies(ctx)
	if err != nil {
		return NodeSelection{}, err
	}

	group, ok := proxies[preferredGroup]
	if !ok {
		for _, proxy := range proxies {
			if isSelectableGroup(proxy.Type) && !strings.EqualFold(proxy.Name, "GLOBAL") {
				group = proxy
				ok = true
				break
			}
		}
	}
	if !ok || group.Now == "" || group.Now == "DIRECT" {
		return NodeSelection{Name: "DIRECT"}, nil
	}

	sel := NodeSelection{Name: group.Now}
	if node, exists := proxies[group.Now]; exists && len(node.History) > 0 {
		sel.Latency = node.History[len(node.History)-1].Delay
	}
	return sel, nil
}

// ProxyNode 供前端展示的可用节点。
type ProxyNode struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Delay    int    `json:"delay"`
	Selected bool   `json:"selected"`
	Tested   bool   `json:"tested"`
}

var groupTypes = map[string]struct{}{
	"selector":    {},
	"urltest":     {},
	"fallback":    {},
	"loadbalance": {},
	"relay":       {},
	"compatible":  {},
	"direct":      {},
	"reject":      {},
	"pass":        {},
	"dns":         {},
}

// ListNodes 只返回当前启用订阅里的节点，不含 DIRECT 和其它订阅残留。
func (c *MihomoClient) ListNodes(ctx context.Context) ([]ProxyNode, error) {
	proxies, err := c.GetProxies(ctx)
	if err != nil {
		return nil, err
	}
	selected := ""
	if group, ok := proxies[preferredGroup]; ok {
		selected = group.Now
	}

	if nodes, ok := c.listProviderNodes(ctx, selected); ok {
		return nodes, nil
	}

	group, ok := proxies[preferredGroup]
	if !ok {
		return []ProxyNode{}, nil
	}
	nodes := make([]ProxyNode, 0, len(group.All))
	for _, name := range group.All {
		if name == "" {
			continue
		}
		if _, skip := skippedNodes[name]; skip {
			continue
		}
		info, exists := proxies[name]
		if exists && isGroupProxy(info.Type) {
			continue
		}
		proxyType := ""
		delay := 0
		tested := false
		if exists {
			proxyType = info.Type
			if len(info.History) > 0 {
				tested = true
				delay = info.History[len(info.History)-1].Delay
			}
		}
		nodes = append(nodes, ProxyNode{
			Name:     name,
			Type:     proxyType,
			Delay:    delay,
			Selected: name == selected,
			Tested:   tested,
		})
	}
	return nodes, nil
}

func (c *MihomoClient) listProviderNodes(ctx context.Context, selected string) ([]ProxyNode, bool) {
	var result providersResponse
	if err := c.doJSON(ctx, http.MethodGet, "/providers/proxies", nil, &result); err != nil || result.Providers == nil {
		return nil, false
	}
	nodes := make([]ProxyNode, 0)
	foundHTTP := false
	for _, provider := range result.Providers {
		if !isHTTPProvider(provider.VehicleType) {
			continue
		}
		foundHTTP = true
		for _, info := range provider.Proxies {
			if info.Name == "" {
				continue
			}
			if _, skip := skippedNodes[info.Name]; skip {
				continue
			}
			if isGroupProxy(info.Type) {
				continue
			}
			delay := 0
			tested := false
			if len(info.History) > 0 {
				tested = true
				delay = info.History[len(info.History)-1].Delay
			}
			nodes = append(nodes, ProxyNode{
				Name:     info.Name,
				Type:     info.Type,
				Delay:    delay,
				Selected: info.Name == selected,
				Tested:   tested,
			})
		}
	}
	if !foundHTTP {
		return nil, false
	}
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].Name < nodes[j].Name
	})
	return nodes, true
}

func isGroupProxy(proxyType string) bool {
	_, ok := groupTypes[strings.ToLower(proxyType)]
	return ok
}

func (c *MihomoClient) doJSON(ctx context.Context, method, path string, body []byte, dest any) error {
	var reader io.Reader
	if body != nil {
		reader = strings.NewReader(string(body))
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("请求 mihomo API 失败: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			slog.Warn("关闭 API 响应失败", "error", closeErr)
		}
	}()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取 API 响应失败: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("mihomo API %s %s 返回 HTTP %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if dest == nil || len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, dest); err != nil {
		return fmt.Errorf("解析 API 响应失败: %w", err)
	}
	return nil
}

func isSelectableGroup(proxyType string) bool {
	switch strings.ToLower(proxyType) {
	case "selector", "urltest":
		return true
	default:
		return false
	}
}

type providersResponse struct {
	Providers map[string]providerInfo `json:"providers"`
}

type providerInfo struct {
	Name        string      `json:"name"`
	VehicleType string      `json:"vehicleType"`
	Proxies     []proxyInfo `json:"proxies"`
}

type connectionsSnapshot struct {
	DownloadTotal int64 `json:"downloadTotal"`
	UploadTotal   int64 `json:"uploadTotal"`
}

// TrafficSnapshot 当前累计流量。
type TrafficSnapshot struct {
	UploadTotal   int64
	DownloadTotal int64
}

// UpdateNamedProviders 立即更新指定的订阅提供者。
func (c *MihomoClient) UpdateNamedProviders(ctx context.Context, names []string) error {
	for _, name := range names {
		if strings.TrimSpace(name) == "" {
			continue
		}
		path := "/providers/proxies/" + url.PathEscape(name)
		if err := c.doJSON(ctx, http.MethodPut, path, nil, nil); err != nil {
			slog.Warn("更新订阅提供者失败", "name", name, "error", err)
		}
	}
	return nil
}

// UpdateHTTPProviders 触发 HTTP 订阅 provider 立即更新。
func (c *MihomoClient) UpdateHTTPProviders(ctx context.Context) error {
	var result providersResponse
	if err := c.doJSON(ctx, http.MethodGet, "/providers/proxies", nil, &result); err != nil {
		return fmt.Errorf("读取订阅提供者失败: %w", err)
	}
	for name, provider := range result.Providers {
		if !isHTTPProvider(provider.VehicleType) {
			continue
		}
		path := "/providers/proxies/" + url.PathEscape(name)
		if err := c.doJSON(ctx, http.MethodPut, path, nil, nil); err != nil {
			slog.Warn("更新订阅提供者失败", "name", name, "error", err)
		}
	}
	return nil
}

// WaitForProxyNodes 等到 PROXY 组里出现真实节点（订阅下载完成）。
func (c *MihomoClient) WaitForProxyNodes(ctx context.Context) error {
	ticker := time.NewTicker(400 * time.Millisecond)
	defer ticker.Stop()
	var lastErr error
	for {
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return lastErr
			}
			return fmt.Errorf("等待节点列表超时: %w", ctx.Err())
		case <-ticker.C:
			nodes, err := c.ListNodes(ctx)
			if err != nil {
				lastErr = err
				continue
			}
			for _, node := range nodes {
				if node.Name != "" && node.Name != "DIRECT" && node.Name != "REJECT" {
					return nil
				}
			}
			lastErr = fmt.Errorf("订阅节点尚未就绪")
		}
	}
}

// PatchMode 热更新 mihomo 的 rule/global 模式。
func (c *MihomoClient) PatchMode(ctx context.Context, mode string) error {
	body, err := json.Marshal(map[string]string{"mode": mode})
	if err != nil {
		return err
	}
	if err := c.doJSON(ctx, http.MethodPatch, "/configs", body, nil); err != nil {
		return fmt.Errorf("切换模式失败: %w", err)
	}
	return nil
}

// ReadTraffic 读取累计上下行字节。
func (c *MihomoClient) ReadTraffic(ctx context.Context) (TrafficSnapshot, error) {
	var snap connectionsSnapshot
	if err := c.doJSON(ctx, http.MethodGet, "/connections", nil, &snap); err != nil {
		return TrafficSnapshot{}, err
	}
	return TrafficSnapshot{UploadTotal: snap.UploadTotal, DownloadTotal: snap.DownloadTotal}, nil
}

// TunEnabled 查询内核配置中 TUN 是否已启用。
func (c *MihomoClient) TunEnabled(ctx context.Context) (bool, error) {
	var cfg map[string]any
	if err := c.doJSON(ctx, http.MethodGet, "/configs", nil, &cfg); err != nil {
		return false, err
	}
	tun, _ := cfg["tun"].(map[string]any)
	if tun == nil {
		return false, nil
	}
	switch v := tun["enable"].(type) {
	case bool:
		return v, nil
	case string:
		return strings.EqualFold(v, "true"), nil
	default:
		return false, nil
	}
}

// CountProviderProxies 返回订阅 provider 中的可用节点数。
func (c *MihomoClient) CountProviderProxies(ctx context.Context, name string) (int, error) {
	var result providersResponse
	if err := c.doJSON(ctx, http.MethodGet, "/providers/proxies", nil, &result); err != nil {
		return 0, err
	}
	provider, ok := result.Providers[name]
	if !ok {
		return 0, fmt.Errorf("找不到订阅提供者 %s", name)
	}
	count := 0
	for _, info := range provider.Proxies {
		if info.Name == "" || isGroupProxy(info.Type) {
			continue
		}
		if _, skip := skippedNodes[info.Name]; skip {
			continue
		}
		count++
	}
	return count, nil
}

func isHTTPProvider(vehicleType string) bool {
	switch strings.ToLower(vehicleType) {
	case "http", "file":
		return true
	default:
		return false
	}
}

func contains(list []string, target string) bool {
	for _, item := range list {
		if item == target {
			return true
		}
	}
	return false
}
