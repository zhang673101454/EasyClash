package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const subscriptionTrafficFileName = "subscription_traffic.json"

var subscriptionTrafficUserAgents = []string{
	"clash.meta",
	"ClashforWindows/0.20.39",
	"clash",
	"mihomo",
}

// SubscriptionTraffic 来自 subscription-userinfo 响应头的流量配额。
type SubscriptionTraffic struct {
	Upload    int64 `json:"upload"`
	Download  int64 `json:"download"`
	Total     int64 `json:"total"`
	Expire    int64 `json:"expire"`
	UpdatedAt int64 `json:"updatedAt"`
}

// SubscriptionRefreshResult 刷新订阅 URL 的结果。
type SubscriptionRefreshResult struct {
	Traffic    SubscriptionTraffic
	NodesSaved bool
}

func subscriptionTrafficPath(configDir string) string {
	return filepath.Join(configDir, subscriptionTrafficFileName)
}

// LoadSubscriptionTrafficCache 读取全部订阅流量缓存。
func LoadSubscriptionTrafficCache(configDir string) (map[string]SubscriptionTraffic, error) {
	return loadSubscriptionTrafficCache(configDir)
}

func loadSubscriptionTrafficCache(configDir string) (map[string]SubscriptionTraffic, error) {
	data, err := os.ReadFile(subscriptionTrafficPath(configDir))
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]SubscriptionTraffic{}, nil
		}
		return nil, fmt.Errorf("读取订阅流量缓存失败: %w", err)
	}
	cache := map[string]SubscriptionTraffic{}
	if err := json.Unmarshal(data, &cache); err != nil {
		return map[string]SubscriptionTraffic{}, nil
	}
	if cache == nil {
		cache = map[string]SubscriptionTraffic{}
	}
	return cache, nil
}

func saveSubscriptionTrafficCache(configDir string, cache map[string]SubscriptionTraffic) error {
	if cache == nil {
		cache = map[string]SubscriptionTraffic{}
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return fmt.Errorf("编码订阅流量缓存失败: %w", err)
	}
	if err := os.WriteFile(subscriptionTrafficPath(configDir), data, 0o644); err != nil {
		return fmt.Errorf("写入订阅流量缓存失败: %w", err)
	}
	return nil
}

// GetSubscriptionTrafficCache 返回指定订阅的缓存流量信息。
func GetSubscriptionTrafficCache(configDir, id string) (SubscriptionTraffic, bool) {
	cache, err := loadSubscriptionTrafficCache(configDir)
	if err != nil {
		return SubscriptionTraffic{}, false
	}
	traffic, ok := cache[id]
	return traffic, ok
}

// RefreshSubscriptionTraffic 拉取订阅 URL：更新流量缓存，并保存节点内容到本地（可走当前已开启的代理）。
func RefreshSubscriptionTraffic(configDir, id string) (SubscriptionRefreshResult, error) {
	items, err := ListSubscriptions(configDir)
	if err != nil {
		return SubscriptionRefreshResult{}, err
	}
	var rawURL string
	for _, item := range items {
		if item.ID == id {
			rawURL = item.URL
			break
		}
	}
	if rawURL == "" {
		return SubscriptionRefreshResult{}, fmt.Errorf("找不到该订阅")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	body, traffic, hasTraffic, err := fetchSubscription(ctx, rawURL)
	if err != nil {
		return SubscriptionRefreshResult{}, err
	}
	if err := SaveProviderSource(configDir, id, body); err != nil {
		return SubscriptionRefreshResult{}, err
	}
	if err := syncProvidersToConfig(configDir, items); err != nil {
		return SubscriptionRefreshResult{}, fmt.Errorf("更新订阅配置失败: %w", err)
	}

	result := SubscriptionRefreshResult{NodesSaved: true}
	if hasTraffic {
		cache, cacheErr := loadSubscriptionTrafficCache(configDir)
		if cacheErr != nil {
			cache = map[string]SubscriptionTraffic{}
		}
		cache[id] = traffic
		if err := saveSubscriptionTrafficCache(configDir, cache); err != nil {
			slog.Warn("保存订阅流量缓存失败", "id", id, "error", err)
		}
		result.Traffic = traffic
	} else {
		slog.Debug("订阅未返回流量头，已保存节点", "id", id)
		if cached, ok := GetSubscriptionTrafficCache(configDir, id); ok {
			result.Traffic = cached
		}
	}
	slog.Info("已刷新订阅", "id", id, "bytes", len(body), "traffic", hasTraffic)
	return result, nil
}

// RefreshAllSubscriptionTraffic 刷新全部订阅的流量与节点缓存。
func RefreshAllSubscriptionTraffic(configDir string) (map[string]SubscriptionTraffic, error) {
	items, err := ListSubscriptions(configDir)
	if err != nil {
		return nil, err
	}
	cache, err := loadSubscriptionTrafficCache(configDir)
	if err != nil {
		cache = map[string]SubscriptionTraffic{}
	}
	for _, item := range items {
		refreshed, refreshErr := RefreshSubscriptionTraffic(configDir, item.ID)
		if refreshErr != nil {
			slog.Debug("刷新订阅失败", "id", item.ID, "error", refreshErr)
			continue
		}
		if refreshed.Traffic.UpdatedAt > 0 {
			cache[item.ID] = refreshed.Traffic
		}
	}
	if err := saveSubscriptionTrafficCache(configDir, cache); err != nil {
		return cache, err
	}
	return cache, nil
}

func fetchSubscription(ctx context.Context, rawURL string) ([]byte, SubscriptionTraffic, bool, error) {
	clients := subscriptionTrafficClients()
	attempts := 1
	if localMixedProxyAvailable() {
		attempts = 3
	}
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, SubscriptionTraffic{}, false, ctx.Err()
			case <-time.After(2 * time.Second):
			}
		}
		for _, ua := range subscriptionTrafficUserAgents {
			for _, client := range clients {
				body, traffic, hasTraffic, err := fetchSubscriptionOnce(ctx, client, rawURL, ua)
				if err == nil {
					return body, traffic, hasTraffic, nil
				}
				lastErr = err
			}
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("请求订阅失败")
	}
	if !localMixedProxyAvailable() && strings.Contains(lastErr.Error(), "请求订阅失败") {
		return nil, SubscriptionTraffic{}, false, fmt.Errorf("%w（请先启用可用订阅并开启代理后再刷新）", lastErr)
	}
	return nil, SubscriptionTraffic{}, false, lastErr
}

func subscriptionTrafficClients() []*http.Client {
	clients := make([]*http.Client, 0, 2)
	if localMixedProxyAvailable() {
		proxyURL, err := url.Parse("http://" + proxyServerValue)
		if err == nil {
			clients = append(clients, &http.Client{
				Timeout: 20 * time.Second,
				Transport: &http.Transport{
					Proxy: http.ProxyURL(proxyURL),
				},
			})
		}
	}
	clients = append(clients, &http.Client{
		Timeout: 20 * time.Second,
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
		},
	})
	return clients
}

func localMixedProxyAvailable() bool {
	conn, err := net.DialTimeout("tcp", proxyServerValue, 300*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func fetchSubscriptionOnce(ctx context.Context, client *http.Client, rawURL, userAgent string) ([]byte, SubscriptionTraffic, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, SubscriptionTraffic{}, false, err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, SubscriptionTraffic{}, false, fmt.Errorf("请求订阅失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, SubscriptionTraffic{}, false, fmt.Errorf("读取订阅内容失败: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, SubscriptionTraffic{}, false, fmt.Errorf("订阅返回 HTTP %d", resp.StatusCode)
	}
	if len(bytesTrimSpace(body)) == 0 {
		return nil, SubscriptionTraffic{}, false, fmt.Errorf("订阅内容为空")
	}

	info := firstHeaderValue(resp.Header, "subscription-userinfo", "Subscription-Userinfo")
	if info == "" {
		return body, SubscriptionTraffic{}, false, nil
	}
	return body, parseSubscriptionUserinfo(info), true, nil
}

func firstHeaderValue(h http.Header, names ...string) string {
	for _, name := range names {
		if v := strings.TrimSpace(h.Get(name)); v != "" {
			return v
		}
	}
	for k, values := range h {
		if strings.EqualFold(k, "subscription-userinfo") && len(values) > 0 {
			if v := strings.TrimSpace(values[0]); v != "" {
				return v
			}
		}
	}
	return ""
}

func parseSubscriptionUserinfo(raw string) SubscriptionTraffic {
	var traffic SubscriptionTraffic
	for _, part := range strings.Split(raw, ";") {
		part = strings.TrimSpace(part)
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(kv[0]))
		val, err := strconv.ParseInt(strings.TrimSpace(kv[1]), 10, 64)
		if err != nil {
			if f, fErr := strconv.ParseFloat(strings.TrimSpace(kv[1]), 64); fErr == nil {
				val = int64(f)
			} else {
				continue
			}
		}
		switch key {
		case "upload":
			traffic.Upload = val
		case "download":
			traffic.Download = val
		case "total":
			traffic.Total = val
		case "expire":
			traffic.Expire = val
		}
	}
	if traffic.Expire > 32_000_000_000 {
		traffic.Expire /= 1000
	}
	traffic.UpdatedAt = time.Now().Unix()
	return traffic
}
