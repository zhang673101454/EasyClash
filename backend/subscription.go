package backend

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const subscriptionsFileName = "subscriptions.json"

// Subscription 表示一条可开关的订阅。
type Subscription struct {
	ID      string `json:"id"`
	URL     string `json:"url"`
	Remark  string `json:"remark"`
	Enabled bool   `json:"enabled"`
}

func subscriptionsPath(configDir string) string {
	return filepath.Join(configDir, subscriptionsFileName)
}

// ListSubscriptions 读取已保存的订阅列表。
func ListSubscriptions(configDir string) ([]Subscription, error) {
	if err := migrateLegacySubscription(configDir); err != nil {
		slog.Warn("迁移旧订阅失败", "error", err)
	}
	data, err := os.ReadFile(subscriptionsPath(configDir))
	if err != nil {
		if os.IsNotExist(err) {
			return []Subscription{}, nil
		}
		return nil, fmt.Errorf("读取订阅列表失败: %w", err)
	}
	var items []Subscription
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("解析订阅列表失败: %w", err)
	}
	if items == nil {
		items = []Subscription{}
	}
	enabled := 0
	for _, item := range items {
		if item.Enabled {
			enabled++
		}
	}
	if enabled > 1 {
		if err := persistSubscriptions(configDir, items); err != nil {
			slog.Warn("规范化订阅启用状态失败", "error", err)
		}
	}
	return items, nil
}

// AddSubscription 新增一条订阅，默认不启用（点击后才开始使用）。
func AddSubscription(configDir, rawURL, remark string) ([]Subscription, error) {
	rawURL = strings.TrimSpace(rawURL)
	remark = strings.TrimSpace(remark)
	if err := validateSubscribeURL(rawURL); err != nil {
		return nil, err
	}
	items, err := ListSubscriptions(configDir)
	if err != nil {
		return nil, err
	}
	for i, item := range items {
		if sameSubscribeURL(item.URL, rawURL) {
			if remark != "" && strings.TrimSpace(items[i].Remark) == "" {
				items[i].Remark = remark
				if err := persistSubscriptions(configDir, items); err != nil {
					return nil, err
				}
			}
			return items, nil
		}
	}
	items = append(items, Subscription{
		ID:      nextSubscriptionID(items),
		URL:     rawURL,
		Remark:  remark,
		Enabled: false,
	})
	if err := persistSubscriptions(configDir, items); err != nil {
		return nil, err
	}
	return items, nil
}

// SetSubscriptionRemark 更新订阅备注。
func SetSubscriptionRemark(configDir, id, remark string) ([]Subscription, error) {
	items, err := ListSubscriptions(configDir)
	if err != nil {
		return nil, err
	}
	found := false
	for i := range items {
		if items[i].ID != id {
			continue
		}
		items[i].Remark = strings.TrimSpace(remark)
		found = true
		break
	}
	if !found {
		return nil, fmt.Errorf("找不到该订阅")
	}
	if err := persistSubscriptions(configDir, items); err != nil {
		return nil, err
	}
	return items, nil
}

// RemoveSubscription 删除指定订阅。
func RemoveSubscription(configDir, id string) ([]Subscription, error) {
	items, err := ListSubscriptions(configDir)
	if err != nil {
		return nil, err
	}
	next := make([]Subscription, 0, len(items))
	found := false
	for _, item := range items {
		if item.ID == id {
			found = true
			continue
		}
		next = append(next, item)
	}
	if !found {
		return nil, fmt.Errorf("找不到该订阅")
	}
	if err := persistSubscriptions(configDir, next); err != nil {
		return nil, err
	}
	return next, nil
}

// SetSubscriptionEnabled 启用或停用一条订阅。同一时刻最多只能启用一条。
func SetSubscriptionEnabled(configDir, id string, enabled bool) ([]Subscription, error) {
	items, err := ListSubscriptions(configDir)
	if err != nil {
		return nil, err
	}
	found := false
	for i := range items {
		if items[i].ID == id {
			items[i].Enabled = enabled
			found = true
			continue
		}
		if enabled {
			items[i].Enabled = false
		}
	}
	if !found {
		return nil, fmt.Errorf("找不到该订阅")
	}
	if err := persistSubscriptions(configDir, items); err != nil {
		return nil, err
	}
	return items, nil
}

func persistSubscriptions(configDir string, items []Subscription) error {
	seenEnabled := false
	for i := range items {
		if !items[i].Enabled {
			continue
		}
		if seenEnabled {
			items[i].Enabled = false
			continue
		}
		seenEnabled = true
	}
	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return fmt.Errorf("编码订阅列表失败: %w", err)
	}
	if err := os.WriteFile(subscriptionsPath(configDir), data, 0o644); err != nil {
		return fmt.Errorf("写入订阅列表失败: %w", err)
	}
	if err := syncProvidersToConfig(configDir, items); err != nil {
		return err
	}
	slog.Info("已更新订阅列表", "count", len(items))
	return nil
}

func syncProvidersToConfig(configDir string, items []Subscription) error {
	cfg, err := loadConfigMap(configDir)
	if err != nil {
		return err
	}
	normalizeRuntimeConfig(cfg)

	providers := map[string]any{}
	enabledIDs := make([]any, 0, 1)
	keep := map[string]struct{}{}
	for _, item := range items {
		if !item.Enabled {
			continue
		}
		providers[item.ID] = newProviderEntry(item.ID, item.URL)
		enabledIDs = append(enabledIDs, item.ID)
		keep[item.ID] = struct{}{}
	}
	cfg["proxy-providers"] = providers
	if err := ensureProxyGroupUses(cfg, enabledIDs); err != nil {
		return err
	}
	cleanupProviderFiles(configDir, keep)

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("编码配置失败: %w", err)
	}
	path := filepath.Join(configDir, configFileName)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("写入订阅配置失败: %w", err)
	}
	return nil
}

func newProviderEntry(id, rawURL string) map[string]any {
	return map[string]any{
		"type":     "http",
		"url":      rawURL,
		"path":     "./providers/" + id + ".yaml",
		"interval": 3600,
		"header": map[string]any{
			"User-Agent": []any{"clash.meta"},
		},
		"health-check": map[string]any{
			"enable":   false,
			"url":      delayTestURL,
			"interval": 300,
		},
	}
}

func sameSubscribeURL(left, right string) bool {
	a, errA := url.Parse(strings.TrimSpace(left))
	b, errB := url.Parse(strings.TrimSpace(right))
	if errA != nil || errB != nil || a.Host == "" || b.Host == "" {
		return strings.TrimSpace(left) == strings.TrimSpace(right)
	}
	a.Host = strings.ToLower(a.Host)
	b.Host = strings.ToLower(b.Host)
	return a.String() == b.String()
}

func validateSubscribeURL(rawURL string) error {
	if rawURL == "" {
		return fmt.Errorf("请填写订阅链接")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return fmt.Errorf("订阅链接无效，请使用 http 或 https 地址")
	}
	return nil
}

func nextSubscriptionID(items []Subscription) string {
	maxN := 0
	for _, item := range items {
		if !strings.HasPrefix(item.ID, "sub") {
			continue
		}
		n, err := strconv.Atoi(strings.TrimPrefix(item.ID, "sub"))
		if err == nil && n > maxN {
			maxN = n
		}
	}
	return fmt.Sprintf("sub%d", maxN+1)
}

func migrateLegacySubscription(configDir string) error {
	if _, err := os.Stat(subscriptionsPath(configDir)); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}

	cfg, err := loadConfigMap(configDir)
	if err != nil {
		return err
	}
	providers, _ := cfg["proxy-providers"].(map[string]any)
	if len(providers) == 0 {
		return nil
	}

	keys := make([]string, 0, len(providers))
	for key := range providers {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	items := make([]Subscription, 0, len(keys))
	for _, key := range keys {
		provider, ok := providers[key].(map[string]any)
		if !ok {
			continue
		}
		raw, _ := provider["url"].(string)
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		items = append(items, Subscription{ID: key, URL: raw, Enabled: true})
	}
	if len(items) == 0 {
		return nil
	}
	return persistSubscriptions(configDir, items)
}

func loadConfigMap(configDir string) (map[string]any, error) {
	path := filepath.Join(configDir, configFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置失败: %w", err)
	}
	cfg := map[string]any{}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("解析配置失败: %w", err)
	}
	return cfg, nil
}

func cleanupProviderFiles(configDir string, keep map[string]struct{}) {
	dir := filepath.Join(configDir, "providers")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		if _, ok := keep[id]; ok {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if err := os.Remove(path); err != nil {
			slog.Warn("清理旧订阅缓存失败", "path", path, "error", err)
			continue
		}
		slog.Info("已删除旧订阅节点缓存", "file", entry.Name())
	}
}

func ensureProxyGroupUses(cfg map[string]any, enabledIDs []any) error {
	groups, ok := cfg["proxy-groups"].([]any)
	if !ok {
		groups = []any{}
	}

	found := false
	for i, item := range groups {
		group, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := group["name"].(string)
		if name != preferredGroup {
			continue
		}
		group["type"] = "select"
		group["include-all-proxies"] = true
		group["exclude-type"] = "selector,urltest,fallback,loadbalance,relay,direct,reject"
		group["proxies"] = []any{"DIRECT"}
		if len(enabledIDs) > 0 {
			group["use"] = enabledIDs
		} else {
			delete(group, "use")
			delete(group, "include-all-proxies")
			delete(group, "exclude-type")
		}
		groups[i] = group
		found = true
		break
	}
	if !found {
		group := map[string]any{
			"name":    preferredGroup,
			"type":    "select",
			"proxies": []any{"DIRECT"},
		}
		if len(enabledIDs) > 0 {
			group["use"] = enabledIDs
			group["include-all-proxies"] = true
			group["exclude-type"] = "selector,urltest,fallback,loadbalance,relay,direct,reject"
		}
		groups = append([]any{group}, groups...)
	}
	cfg["proxy-groups"] = groups
	return nil
}

func normalizeConfigFile(configDir string) error {
	cfg, err := loadConfigMap(configDir)
	if err != nil {
		return err
	}
	changed := normalizeRuntimeConfig(cfg)
	if !changed {
		return nil
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("编码配置失败: %w", err)
	}
	path := filepath.Join(configDir, configFileName)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("写入规范化配置失败: %w", err)
	}
	slog.Info("已规范化 mihomo 配置，避免首次启动下载 GeoIP 卡住")
	return nil
}

func normalizeRuntimeConfig(cfg map[string]any) bool {
	changed := false
	delete(cfg, "geox-url")

	safeRules := []any{
		"IP-CIDR,127.0.0.0/8,DIRECT",
		"IP-CIDR,10.0.0.0/8,DIRECT",
		"IP-CIDR,172.16.0.0/12,DIRECT",
		"IP-CIDR,192.168.0.0/16,DIRECT",
		"MATCH,PROXY",
	}
	if !rulesEqual(cfg["rules"], safeRules) {
		cfg["rules"] = safeRules
		changed = true
	}

	if dns, ok := cfg["dns"].(map[string]any); ok {
		if _, exists := dns["listen"]; exists {
			delete(dns, "listen")
			cfg["dns"] = dns
			changed = true
		}
	}

	providers, _ := cfg["proxy-providers"].(map[string]any)
	if providers != nil {
		for _, value := range providers {
			provider, ok := value.(map[string]any)
			if !ok {
				continue
			}
			header, _ := provider["header"].(map[string]any)
			if header == nil {
				header = map[string]any{}
			}
			if _, isSlice := header["User-Agent"].([]any); !isSlice {
				header["User-Agent"] = []any{"clash.meta"}
				provider["header"] = header
				changed = true
			}
		}
	}
	return changed
}

func rulesEqual(current any, want []any) bool {
	list, ok := current.([]any)
	if !ok || len(list) != len(want) {
		return false
	}
	for i := range want {
		left, _ := list[i].(string)
		right, _ := want[i].(string)
		if left != right {
			return false
		}
	}
	return true
}
