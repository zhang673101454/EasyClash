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

// SubscriptionFetchOptions 控制订阅 URL 拉取是否经本机 mixed-port。
type SubscriptionFetchOptions struct {
	// PreferProxy 为 true 表示 mihomo 正在运行：强制走 127.0.0.1:7890，且不再回退直连（被墙订阅直连必失败）。
	PreferProxy bool
}

// RefreshSubscriptionTraffic 拉取订阅 URL：更新流量缓存，并保存节点内容到本地（可走当前已开启的代理）。
func RefreshSubscriptionTraffic(ctx context.Context, configDir, id string, opts SubscriptionFetchOptions) (SubscriptionRefreshResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
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

	body, traffic, hasTraffic, err := fetchSubscription(ctx, rawURL, opts)
	if err != nil {
		return SubscriptionRefreshResult{}, err
	}
	if err := SaveProviderSource(configDir, id, body); err != nil {
		return SubscriptionRefreshResult{}, err
	}
	items, err = ListSubscriptions(configDir)
	if err != nil {
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
		itemCtx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		refreshed, refreshErr := RefreshSubscriptionTraffic(itemCtx, configDir, item.ID, SubscriptionFetchOptions{})
		cancel()
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

func fetchSubscription(ctx context.Context, rawURL string, opts SubscriptionFetchOptions) ([]byte, SubscriptionTraffic, bool, error) {
	preferProxy := opts.PreferProxy
	viaProxy := preferProxy || localMixedProxyAvailable()
	host := subscriptionHostFromURL(rawURL)
	slog.Info("拉取订阅", "host", host, "viaProxy", viaProxy, "preferProxy", preferProxy)

	type attempt struct {
		client *http.Client
		label  string
	}
	attempts := make([]attempt, 0, 2)
	if viaProxy {
		if proxyURL, err := url.Parse("http://" + proxyServerValue); err == nil {
			attempts = append(attempts, attempt{
				label: "proxy",
				client: &http.Client{
					Timeout: 25 * time.Second,
					Transport: subscriptionTransport(proxyURL),
				},
			})
		}
	}
	// 代理已开启时不回退直连（被墙域名直连只会超时/被 RST，且会误导用户）
	if !preferProxy {
		attempts = append(attempts, attempt{
			label: "direct",
			client: &http.Client{
				Timeout: 15 * time.Second,
				Transport: subscriptionTransport(nil),
			},
		})
	}

	var lastErr error
	var lastVia string
	// 先用 clash.meta（兼容性最好），失败再换其它 UA
	uas := append([]string{subscriptionTrafficUserAgents[0]}, subscriptionTrafficUserAgents[1:]...)
	for _, item := range attempts {
		for _, ua := range uas {
			if err := ctx.Err(); err != nil {
				return nil, SubscriptionTraffic{}, false, err
			}
			body, traffic, hasTraffic, err := fetchSubscriptionOnce(ctx, item.client, rawURL, ua)
			if err == nil {
				slog.Info("拉取订阅成功", "host", host, "via", item.label, "bytes", len(body))
				return body, traffic, hasTraffic, nil
			}
			lastErr = err
			lastVia = item.label
			// 4xx 换 UA 无意义，直接换通道
			if strings.Contains(err.Error(), "HTTP 4") {
				break
			}
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("请求订阅失败")
	}
	if preferProxy {
		if len(attempts) == 0 {
			return nil, SubscriptionTraffic{}, false, fmt.Errorf("本机代理端口 %s 不可用，请确认 mihomo 已启动", proxyServerValue)
		}
		slog.Warn("经代理拉取订阅失败", "host", host, "via", lastVia, "error", lastErr)
		return nil, SubscriptionTraffic{}, false, fmt.Errorf("经本机代理拉取订阅失败：%w", lastErr)
	}
	if !viaProxy {
		return nil, SubscriptionTraffic{}, false, fmt.Errorf("%w（请先启用可用订阅并开启代理后再刷新）", lastErr)
	}
	slog.Warn("拉取订阅失败", "host", host, "via", lastVia, "error", lastErr)
	return nil, SubscriptionTraffic{}, false, lastErr
}

func localMixedProxyAvailable() bool {
	for attempt := 0; attempt < 3; attempt++ {
		conn, err := net.DialTimeout("tcp", proxyServerValue, time.Second)
		if err == nil {
			_ = conn.Close()
			return true
		}
		if attempt < 2 {
			time.Sleep(120 * time.Millisecond)
		}
	}
	return false
}

// LocalMixedProxyAvailable 本机 mixed-port 是否可用（用于刷新订阅时判断是否可走代理）。
func LocalMixedProxyAvailable() bool {
	return localMixedProxyAvailable()
}

func subscriptionTransport(proxyURL *url.URL) *http.Transport {
	t := &http.Transport{
		DisableKeepAlives: true,
		MaxIdleConns:      0,
	}
	if proxyURL != nil {
		t.Proxy = http.ProxyURL(proxyURL)
	} else {
		t.Proxy = func(*http.Request) (*url.URL, error) { return nil, nil }
	}
	return t
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
