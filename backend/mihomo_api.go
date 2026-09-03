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
	ensureMu   sync.Mutex
	delayCacheMu sync.Mutex
	delayCache   map[string]int
	delayCacheAt time.Time
}

const delayCacheTTL = 45 * time.Second

type proxyInfo struct {
	Name    string         `json:"name"`
	Type    string         `json:"type"`
	Now     string         `json:"now"`
	All     []string       `json:"all"`
	Alive   bool           `json:"alive"`
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
			Timeout: 20 * time.Second,
			Transport: &http.Transport{
				DisableKeepAlives: true,
				MaxIdleConns:      0,
			},
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

// GetProxyGroup 只读单个策略组（比 /proxies 全量轻很多）。
func (c *MihomoClient) GetProxyGroup(ctx context.Context, group string) (proxyInfo, error) {
	var info proxyInfo
	path := "/proxies/" + url.PathEscape(group)
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &info); err != nil {
		return proxyInfo{}, fmt.Errorf("获取策略组 %s 失败: %w", group, err)
	}
	return info, nil
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

// SwitchSubscription 切换当前使用的订阅（PROXY → 订阅子组，不重启内核）。
func (c *MihomoClient) SwitchSubscription(ctx context.Context, subID string) error {
	subID = strings.TrimSpace(subID)
	if subID == "" {
		return fmt.Errorf("订阅 ID 不能为空")
	}
	groupName := SubscriptionGroupName(subID)
	if err := c.SwitchProxy(ctx, preferredGroup, groupName); err != nil {
		return fmt.Errorf("切换订阅失败: %w", err)
	}
	if err := c.WaitForSubscriptionNodes(ctx, subID); err != nil {
		slog.Warn("等待订阅节点", "sub", subID, "error", err)
	}
	return c.EnsureSubscriptionNodeSelected(ctx, subID)
}

// EnsureSubscriptionNodeSelected 在订阅子组内选中第一个可用节点。
func (c *MihomoClient) EnsureSubscriptionNodeSelected(ctx context.Context, subID string) error {
	groupName := SubscriptionGroupName(subID)
	group, err := c.GetProxyGroup(ctx, groupName)
	if err != nil {
		return err
	}
	if group.Now != "" && group.Now != "DIRECT" && c.isSubscriptionNodeSelectable(ctx, subID, group.Now) {
		return nil
	}
	for _, node := range group.All {
		if !c.isSubscriptionNodeSelectable(ctx, subID, node) {
			continue
		}
		if err := c.SwitchProxy(ctx, groupName, node); err != nil {
			continue
		}
		slog.Info("已切换到可用节点", "group", groupName, "node", node)
		return nil
	}
	if nodes, ok := c.listProviderNodesFor(ctx, subID, ""); ok {
		for _, node := range nodes {
			if !c.isSubscriptionNodeSelectable(ctx, subID, node.Name) {
				continue
			}
			if err := c.SwitchProxy(ctx, groupName, node.Name); err != nil {
				continue
			}
			slog.Info("已切换到可用节点", "group", groupName, "node", node.Name)
			return nil
		}
	}
	return fmt.Errorf("订阅 %s 没有可用节点", subID)
}

func (c *MihomoClient) isSubscriptionNodeSelectable(ctx context.Context, subID, name string) bool {
	if name == "" || name == "DIRECT" || isPlaceholderNode(name) {
		return false
	}
	if _, skip := skippedNodes[name]; skip {
		return false
	}
	alive, hasAlivePeer, found := c.providerProxyAlive(ctx, subID, name)
	if !found {
		return true
	}
	if alive {
		return true
	}
	// 当前节点不可用，但同订阅里还有 alive 节点时，应切换到其它节点
	return !hasAlivePeer
}

func (c *MihomoClient) providerProxyAlive(ctx context.Context, providerName, nodeName string) (alive, hasAlivePeer, found bool) {
	var result providersResponse
	if err := c.doJSON(ctx, http.MethodGet, "/providers/proxies", nil, &result); err != nil || result.Providers == nil {
		return false, false, false
	}
	provider, ok := result.Providers[providerName]
	if !ok || !isHTTPProvider(provider.VehicleType) {
		return false, false, false
	}
	for _, info := range provider.Proxies {
		if info.Name == "" || isGroupProxy(info.Type) {
			continue
		}
		if _, skip := skippedNodes[info.Name]; skip {
			continue
		}
		if isPlaceholderNode(info.Name) {
			continue
		}
		if info.Alive {
			hasAlivePeer = true
		}
		if info.Name == nodeName {
			found = true
			alive = info.Alive
		}
	}
	return alive, hasAlivePeer, found
}

// activeSubscriptionID 读取 PROXY 当前选中的订阅 id。
func (c *MihomoClient) activeSubscriptionID(ctx context.Context) string {
	group, err := c.GetProxyGroup(ctx, preferredGroup)
	if err != nil || group.Now == "" || group.Now == "DIRECT" {
		return ""
	}
	subID := SubscriptionIDFromGroup(group.Now)
	if subID == "" {
		return ""
	}
	child, err := c.GetProxyGroup(ctx, group.Now)
	if err != nil || !isSelectableGroup(child.Type) {
		return ""
	}
	return subID
}

func (c *MihomoClient) activeNodeGroup(ctx context.Context) string {
	if subID := c.activeSubscriptionID(ctx); subID != "" {
		return SubscriptionGroupName(subID)
	}
	return preferredGroup
}

// EnsureProxyNodeSelected 若当前仍为 DIRECT 但已有节点，先切到第一个可用节点（不测速）。
func (c *MihomoClient) EnsureProxyNodeSelected(ctx context.Context) error {
	c.ensureMu.Lock()
	defer c.ensureMu.Unlock()

	if subID := c.activeSubscriptionID(ctx); subID != "" {
		return c.EnsureSubscriptionNodeSelected(ctx, subID)
	}

	sel, err := c.CurrentNode(ctx)
	if err != nil {
		return err
	}
	if sel.Name != "" && sel.Name != "DIRECT" {
		return nil
	}
	for _, node := range c.collectSelectableNodeNames(ctx) {
		if err := c.SwitchProxy(ctx, preferredGroup, node); err != nil {
			continue
		}
		slog.Info("已切换到可用节点", "node", node)
		return nil
	}
	return fmt.Errorf("没有可用节点")
}

func (c *MihomoClient) collectSelectableNodeNames(ctx context.Context) []string {
	seen := map[string]struct{}{}
	add := func(name string) {
		if name == "" || isPlaceholderNode(name) {
			return
		}
		if _, skip := skippedNodes[name]; skip {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
	}

	nodes, err := c.listNodesCore(ctx)
	if err == nil {
		for _, node := range nodes {
			add(node.Name)
		}
	}
	group, err := c.GetProxyGroup(ctx, preferredGroup)
	if err == nil {
		for _, name := range group.All {
			add(name)
		}
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// WaitAndEnsureProxyNode 等待 provider 就绪并选中可用节点。
func (c *MihomoClient) WaitAndEnsureProxyNode(ctx context.Context) error {
	if subID := c.activeSubscriptionID(ctx); subID != "" {
		if err := c.WaitForSubscriptionNodes(ctx, subID); err != nil {
			slog.Warn("等待节点列表", "sub", subID, "error", err)
		}
		return c.EnsureSubscriptionNodeSelected(ctx, subID)
	}
	if err := c.WaitForProxyNodes(ctx); err != nil {
		slog.Warn("等待节点列表", "error", err)
	}
	return c.EnsureProxyNodeSelected(ctx)
}

// WaitForSubscriptionNodes 等待指定订阅子组出现可用节点。
func (c *MihomoClient) WaitForSubscriptionNodes(ctx context.Context, subID string) error {
	ticker := time.NewTicker(600 * time.Millisecond)
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
			attemptCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
			ready, err := c.subscriptionNodesReady(attemptCtx, subID)
			cancel()
			if ready {
				return nil
			}
			if err != nil {
				lastErr = err
			} else {
				lastErr = fmt.Errorf("订阅 %s 节点尚未就绪", subID)
			}
		}
	}
}

func (c *MihomoClient) subscriptionNodesReady(ctx context.Context, subID string) (bool, error) {
	groupName := SubscriptionGroupName(subID)
	group, err := c.GetProxyGroup(ctx, groupName)
	if err != nil {
		return false, err
	}
	for _, name := range group.All {
		if name == "" || name == "DIRECT" || isPlaceholderNode(name) {
			continue
		}
		if _, skip := skippedNodes[name]; skip {
			continue
		}
		return true, nil
	}
	if nodes, ok := c.listProviderNodesFor(ctx, subID, ""); ok && len(nodes) > 0 {
		return true, nil
	}
	if count, err := c.CountProviderProxies(ctx, subID); err == nil && count > 0 {
		return true, nil
	}
	return false, nil
}

func isPlaceholderNode(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	if lower == "" {
		return true
	}
	for _, hint := range []string{"127.0.0.1", "localhost", "占位", "placeholder"} {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	return false
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
		if err := c.EnsureProxyNodeSelected(ctx); err != nil {
			if count := c.countSelectableNodes(ctx); count > 0 {
				return NodeSelection{}, fmt.Errorf("测速无可用结果（%d 个节点均未在 %d 秒内响应）", count, delayTimeoutMs)
			}
			return NodeSelection{}, fmt.Errorf("当前订阅没有节点，请先刷新订阅")
		}
		sel, err := c.CurrentNode(ctx)
		if err != nil {
			return NodeSelection{}, err
		}
		return sel, nil
	}

	if err := c.SwitchProxy(ctx, c.activeNodeGroup(ctx), bestNode); err != nil {
		return NodeSelection{}, err
	}
	return NodeSelection{Name: bestNode, Latency: bestDelay}, nil
}

func (c *MihomoClient) switchActiveNode(ctx context.Context, node string) error {
	return c.SwitchProxy(ctx, c.activeNodeGroup(ctx), node)
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
		if count := c.countSelectableNodes(ctx); count > 0 {
			return NodeSelection{}, false, fmt.Errorf("测速无可用结果（%d 个节点均未在 %d 秒内响应）", count, delayTimeoutMs)
		}
		return NodeSelection{}, false, fmt.Errorf("当前订阅没有节点，请先刷新订阅")
	}

	if bestNode == current.Name {
		return NodeSelection{Name: current.Name, Latency: bestDelay}, false, nil
	}

	if current.Name == "" || current.Name == "DIRECT" {
		if err := c.switchActiveNode(ctx, bestNode); err != nil {
			return NodeSelection{}, false, err
		}
		return NodeSelection{Name: bestNode, Latency: bestDelay}, true, nil
	}

	// 仅用本轮测速结果判断；当前节点测不通时不能沿用历史延迟而拒绝切换。
	currentDelay := delays[current.Name]
	if currentDelay <= 0 {
		if err := c.switchActiveNode(ctx, bestNode); err != nil {
			return NodeSelection{}, false, err
		}
		return NodeSelection{Name: bestNode, Latency: bestDelay}, true, nil
	}
	if bestDelay > 0 && currentDelay-bestDelay < minImprovementMs {
		return NodeSelection{Name: current.Name, Latency: currentDelay}, false, nil
	}

	if err := c.switchActiveNode(ctx, bestNode); err != nil {
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
		if count := c.countSelectableNodes(ctx); count > 0 {
			return fmt.Errorf("PROXY 组尚未就绪，请稍后再试")
		}
		return fmt.Errorf("当前订阅没有节点，请先刷新订阅")
	}
	return nil
}

func (c *MihomoClient) collectProxyDelays(ctx context.Context) (map[string]int, error) {
	groupName := c.activeNodeGroup(ctx)
	if delays, err := c.TestGroupDelay(ctx, groupName); err == nil {
		if _, bestDelay := pickBestNode(delays); bestDelay > 0 {
			c.storeDelayCache(delays)
			return delays, nil
		}
	}
	delays, err := c.testAllNodeDelays(ctx)
	if err != nil {
		return nil, fmt.Errorf("测速失败: %w", err)
	}
	if _, bestDelay := pickBestNode(delays); bestDelay > 0 {
		c.storeDelayCache(delays)
		return delays, nil
	}
	if count := c.countSelectableNodes(ctx); count > 0 {
		return nil, fmt.Errorf("测速无可用结果（%d 个节点均未在 %d 秒内响应）", count, delayTimeoutMs)
	}
	return nil, fmt.Errorf("当前订阅没有节点，请先刷新订阅")
}

func (c *MihomoClient) countSelectableNodes(ctx context.Context) int {
	nodes, err := c.listNodesCore(ctx)
	if err != nil {
		return 0
	}
	n := 0
	for _, node := range nodes {
		if node.Name == "" || node.Name == "DIRECT" || node.Name == "REJECT" {
			continue
		}
		if _, skip := skippedNodes[node.Name]; skip {
			continue
		}
		if isPlaceholderNode(node.Name) {
			continue
		}
		n++
	}
	return n
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
	activeSub := c.activeSubscriptionID(ctx)
	for providerName, provider := range result.Providers {
		if activeSub != "" && providerName != activeSub {
			continue
		}
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
		if isPlaceholderNode(node) {
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

// CurrentNode 读取当前选中节点及延迟（嵌套模式下解析订阅子组内的真实节点）。
func (c *MihomoClient) CurrentNode(ctx context.Context) (NodeSelection, error) {
	group, err := c.GetProxyGroup(ctx, preferredGroup)
	if err != nil {
		return NodeSelection{}, err
	}
	if group.Now == "" || group.Now == "DIRECT" {
		return NodeSelection{Name: "DIRECT"}, nil
	}

	nodeName := group.Now
	if child, childErr := c.GetProxyGroup(ctx, group.Now); childErr == nil && isSelectableGroup(child.Type) {
		if child.Now == "" || child.Now == "DIRECT" {
			return NodeSelection{Name: "DIRECT"}, nil
		}
		nodeName = child.Now
	}

	sel := NodeSelection{Name: nodeName}
	nodePath := "/proxies/" + url.PathEscape(nodeName)
	var node proxyInfo
	if err := c.doJSON(ctx, http.MethodGet, nodePath, nil, &node); err == nil && len(node.History) > 0 {
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
	nodes, err := c.listNodesCore(ctx)
	if err != nil {
		return nil, err
	}
	return c.enrichNodesWithDelays(ctx, nodes), nil
}

func (c *MihomoClient) listNodesCore(ctx context.Context) ([]ProxyNode, error) {
	nodeGroup := c.activeNodeGroup(ctx)
	providerName := c.activeSubscriptionID(ctx)

	selected := ""
	if group, err := c.GetProxyGroup(ctx, nodeGroup); err == nil {
		selected = group.Now
	}

	if providerName != "" {
		if nodes, ok := c.listProviderNodesFor(ctx, providerName, selected); ok && len(nodes) > 0 {
			return nodes, nil
		}
	}

	group, err := c.GetProxyGroup(ctx, nodeGroup)
	if err != nil {
		return nil, err
	}
	nodes := make([]ProxyNode, 0, len(group.All))
	for _, name := range group.All {
		if name == "" {
			continue
		}
		if _, skip := skippedNodes[name]; skip {
			continue
		}
		nodes = append(nodes, ProxyNode{
			Name:     name,
			Type:     "",
			Delay:    0,
			Selected: name == selected,
			Tested:   false,
		})
	}
	return nodes, nil
}

// InvalidateDelayCache 在切换订阅或重启内核后清空测速缓存。
func (c *MihomoClient) InvalidateDelayCache() {
	c.delayCacheMu.Lock()
	c.delayCache = nil
	c.delayCacheAt = time.Time{}
	c.delayCacheMu.Unlock()
}

func (c *MihomoClient) storeDelayCache(delays map[string]int) {
	if len(delays) == 0 {
		return
	}
	c.delayCacheMu.Lock()
	c.delayCache = delays
	c.delayCacheAt = time.Now()
	c.delayCacheMu.Unlock()
}

func (c *MihomoClient) cachedGroupDelays(ctx context.Context) (map[string]int, error) {
	c.delayCacheMu.Lock()
	if len(c.delayCache) > 0 && time.Since(c.delayCacheAt) < delayCacheTTL {
		out := make(map[string]int, len(c.delayCache))
		for k, v := range c.delayCache {
			out[k] = v
		}
		c.delayCacheMu.Unlock()
		return out, nil
	}
	c.delayCacheMu.Unlock()

	delays, err := c.TestGroupDelay(ctx, c.activeNodeGroup(ctx))
	if err != nil {
		return nil, err
	}
	c.storeDelayCache(delays)
	return delays, nil
}

// enrichNodesWithDelays 把 PROXY 组测速结果合并进节点列表（仅 UI 展示用，等待/切换路径勿依赖）。
func (c *MihomoClient) enrichNodesWithDelays(ctx context.Context, nodes []ProxyNode) []ProxyNode {
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < 10*time.Second {
		return nodes
	}
	need := false
	for _, node := range nodes {
		if !node.Tested {
			need = true
			break
		}
	}
	if !need {
		return nodes
	}
	enrichCtx, enrichCancel := context.WithTimeout(ctx, 8*time.Second)
	defer enrichCancel()
	delays, err := c.cachedGroupDelays(enrichCtx)
	if err != nil || len(delays) == 0 {
		return nodes
	}
	for i := range nodes {
		delay, ok := delays[nodes[i].Name]
		if !ok || delay <= 0 {
			continue
		}
		nodes[i].Delay = delay
		nodes[i].Tested = true
	}
	return nodes
}

func (c *MihomoClient) listProviderNodesFor(ctx context.Context, providerName, selected string) ([]ProxyNode, bool) {
	var result providersResponse
	if err := c.doJSON(ctx, http.MethodGet, "/providers/proxies", nil, &result); err != nil || result.Providers == nil {
		return nil, false
	}
	provider, ok := result.Providers[providerName]
	if !ok || !isHTTPProvider(provider.VehicleType) {
		return nil, false
	}
	nodes := make([]ProxyNode, 0, len(provider.Proxies))
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
	if len(nodes) == 0 {
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

// WaitForProxyNodes 等到 PROXY 组里出现真实节点（订阅下载完成）。不触发测速，避免占满 API。
func (c *MihomoClient) WaitForProxyNodes(ctx context.Context) error {
	ticker := time.NewTicker(600 * time.Millisecond)
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
			attemptCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
			ready, err := c.proxyNodesReady(attemptCtx)
			cancel()
			if ready {
				return nil
			}
			if err != nil {
				lastErr = err
				slog.Debug("等待节点就绪", "error", err)
			} else {
				lastErr = fmt.Errorf("订阅节点尚未就绪")
			}
		}
	}
}

func (c *MihomoClient) proxyNodesReady(ctx context.Context) (bool, error) {
	sel, selErr := c.CurrentNode(ctx)
	if selErr == nil && sel.Name != "" && sel.Name != "DIRECT" && !isPlaceholderNode(sel.Name) {
		return true, nil
	}
	nodes, err := c.listNodesCore(ctx)
	if err != nil {
		return false, err
	}
	for _, node := range nodes {
		if node.Name == "" || node.Name == "DIRECT" || node.Name == "REJECT" {
			continue
		}
		if _, skip := skippedNodes[node.Name]; skip {
			continue
		}
		if isPlaceholderNode(node.Name) {
			continue
		}
		return true, nil
	}
	return false, nil
}

// ReloadConfigFromDisk 让 mihomo 重新加载配置目录中的 config.yaml。
func (c *MihomoClient) ReloadConfigFromDisk(ctx context.Context) error {
	body, err := json.Marshal(map[string]string{"path": "", "payload": ""})
	if err != nil {
		return fmt.Errorf("编码重载请求失败: %w", err)
	}
	if err := c.doJSON(ctx, http.MethodPut, "/configs?force=true", body, nil); err != nil {
		return fmt.Errorf("重载 mihomo 配置失败: %w", err)
	}
	return nil
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

// ProxyUsesNestedGroups 判断运行中的 PROXY 是否已采用嵌套子组（含订阅 ID）。
func (c *MihomoClient) ProxyUsesNestedGroups(ctx context.Context, subIDs []string) bool {
	if len(subIDs) == 0 {
		return false
	}
	group, err := c.GetProxyGroup(ctx, preferredGroup)
	if err != nil {
		return false
	}
	for _, id := range subIDs {
		if contains(group.All, id) {
			return true
		}
	}
	return false
}

func contains(list []string, target string) bool {
	for _, item := range list {
		if item == target {
			return true
		}
	}
	return false
}
