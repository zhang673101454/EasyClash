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

// RefreshSubscriptionTraffic 拉取订阅 URL 并更新流量缓存。
func RefreshSubscriptionTraffic(configDir, id string) (SubscriptionTraffic, error) {
	items, err := ListSubscriptions(configDir)
	if err != nil {
		return SubscriptionTraffic{}, err
	}
	var rawURL string
	for _, item := range items {
		if item.ID == id {
			rawURL = item.URL
			break
		}
	}
	if rawURL == "" {
		return SubscriptionTraffic{}, fmt.Errorf("找不到该订阅")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	traffic, err := fetchSubscriptionUserinfo(ctx, rawURL)
	if err != nil {
		return SubscriptionTraffic{}, err
	}

	cache, err := loadSubscriptionTrafficCache(configDir)
	if err != nil {
		cache = map[string]SubscriptionTraffic{}
	}
	cache[id] = traffic
	if err := saveSubscriptionTrafficCache(configDir, cache); err != nil {
		slog.Warn("保存订阅流量缓存失败", "id", id, "error", err)
	}
	return traffic, nil
}

// RefreshAllSubscriptionTraffic 刷新全部订阅的流量信息。
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
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		traffic, fetchErr := fetchSubscriptionUserinfo(ctx, item.URL)
		cancel()
		if fetchErr != nil {
			slog.Debug("获取订阅流量失败", "id", item.ID, "error", fetchErr)
			continue
		}
		cache[item.ID] = traffic
	}
	if err := saveSubscriptionTrafficCache(configDir, cache); err != nil {
		return cache, err
	}
	return cache, nil
}

func fetchSubscriptionUserinfo(ctx context.Context, rawURL string) (SubscriptionTraffic, error) {
	clients := subscriptionTrafficClients()
	var lastErr error
	for _, ua := range subscriptionTrafficUserAgents {
		for _, client := range clients {
			traffic, err := fetchSubscriptionUserinfoOnce(ctx, client, rawURL, ua)
			if err == nil {
				return traffic, nil
			}
			lastErr = err
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("该订阅未返回流量信息")
	}
	if !localMixedProxyAvailable() && strings.Contains(lastErr.Error(), "请求订阅失败") {
		return SubscriptionTraffic{}, fmt.Errorf("%w（可先点击订阅开启代理后再刷新流量）", lastErr)
	}
	return SubscriptionTraffic{}, lastErr
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

func fetchSubscriptionUserinfoOnce(ctx context.Context, client *http.Client, rawURL, userAgent string) (SubscriptionTraffic, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return SubscriptionTraffic{}, err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return SubscriptionTraffic{}, fmt.Errorf("请求订阅失败: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 8<<20))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return SubscriptionTraffic{}, fmt.Errorf("订阅返回 HTTP %d", resp.StatusCode)
	}

	info := firstHeaderValue(resp.Header, "subscription-userinfo", "Subscription-Userinfo")
	if info == "" {
		return SubscriptionTraffic{}, fmt.Errorf("该订阅未返回流量信息")
	}
	return parseSubscriptionUserinfo(info), nil
}

func firstHeaderValue(h http.Header, names ...string) string {
	for _, name := range names {
		if v := strings.TrimSpace(h.Get(name)); v != "" {
			return v
		}
	}
	// 兼容部分网关把 header key 改写成非规范大小写
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
			// 兼容少数机场返回浮点字节数
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
