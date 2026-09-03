package backend

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// SanitizeProviderSource 过滤/修复会导致 mihomo 整包加载失败的节点（如 hy2 缺 obfs-password）。
func SanitizeProviderSource(data []byte) ([]byte, int, error) {
	data = bytesTrimSpace(data)
	if len(data) == 0 {
		return nil, 0, fmt.Errorf("订阅内容为空")
	}

	cfg, proxies, err := parseProviderProxies(data)
	if err != nil {
		if out, removed, ok := sanitizeProviderLinkBundle(data); ok {
			if out == nil {
				return nil, removed, fmt.Errorf("订阅中没有可用节点（已过滤 %d 个无效项）", removed)
			}
			return out, removed, nil
		}
		slog.Debug("订阅非标准格式，跳过节点清洗", "error", err)
		return data, 0, nil
	}
	if len(proxies) == 0 {
		return data, 0, nil
	}

	removed := 0
	kept := make([]any, 0, len(proxies))
	for _, item := range proxies {
		p, ok := item.(map[string]any)
		if !ok {
			removed++
			continue
		}
		repairProxyEntry(p)
		if !proxyUsable(p) {
			removed++
			name, _ := p["name"].(string)
			slog.Warn("跳过无效订阅节点", "name", name, "type", p["type"])
			continue
		}
		kept = append(kept, p)
	}
	if len(kept) == 0 {
		return nil, removed, fmt.Errorf("订阅中没有可用节点（已过滤 %d 个无效项）", removed)
	}

	cfg["proxies"] = kept
	out, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, removed, fmt.Errorf("编码清洗后的订阅失败: %w", err)
	}
	if removed == 0 && string(bytesTrimSpace(out)) == string(data) {
		return data, 0, nil
	}
	if removed > 0 {
		slog.Info("已清洗订阅节点", "removed", removed, "kept", len(kept))
	} else if string(bytesTrimSpace(out)) != string(data) {
		slog.Info("已修复订阅节点配置", "kept", len(kept))
	}
	return out, removed, nil
}

// ResanitizeProviderFile 重新清洗已缓存的 .source 文件（修复旧缓存里的坏节点）。
func ResanitizeProviderFile(configDir, id string) (int, error) {
	path := providerSourcePath(configDir, id)
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	sanitized, removed, err := SanitizeProviderSource(data)
	if err != nil {
		return removed, err
	}
	if string(bytesTrimSpace(sanitized)) == string(bytesTrimSpace(data)) {
		return removed, nil
	}
	if writeErr := os.WriteFile(path, sanitized, 0o644); writeErr != nil {
		return removed, writeErr
	}
	slog.Info("已写回清洗后的订阅缓存", "id", id, "removed", removed)
	return removed, nil
}

func parseProviderProxies(data []byte) (map[string]any, []any, error) {
	var cfg map[string]any
	if err := yaml.Unmarshal(data, &cfg); err == nil {
		if list, ok := cfg["proxies"].([]any); ok && len(list) > 0 {
			return cfg, list, nil
		}
	}

	var list []map[string]any
	if err := yaml.Unmarshal(data, &list); err == nil && len(list) > 0 {
		proxies := make([]any, 0, len(list))
		for _, item := range list {
			proxies = append(proxies, item)
		}
		return map[string]any{"proxies": proxies}, proxies, nil
	}

	return nil, nil, fmt.Errorf("未找到 proxies 列表")
}

func repairProxyEntry(p map[string]any) {
	typ := proxyTypeName(p)
	switch typ {
	case "hysteria2", "hy2":
		repairHysteria2Obfs(p)
	case "ss", "shadowsocks":
		repairShadowsocksPlugin(p)
	}
}

func repairHysteria2Obfs(p map[string]any) {
	obfs := stringField(p["obfs"])
	if obfs == "" || strings.EqualFold(obfs, "none") {
		delete(p, "obfs")
		return
	}
	if stringField(p["obfs-password"]) != "" || stringField(p["obfs_password"]) != "" {
		return
	}
	if pwd := stringField(p["password"]); pwd != "" {
		p["obfs-password"] = pwd
		slog.Debug("Hysteria2 节点补全 obfs-password", "name", p["name"])
		return
	}
	delete(p, "obfs")
	slog.Debug("Hysteria2 节点移除无效 obfs", "name", p["name"])
}

func repairShadowsocksPlugin(p map[string]any) {
	plugin := strings.ToLower(stringField(p["plugin"]))
	if plugin == "" || !strings.Contains(plugin, "obfs") {
		return
	}
	opts, ok := p["plugin-opts"].(map[string]any)
	if !ok {
		opts = map[string]any{}
	}
	if stringField(opts["password"]) == "" && stringField(p["password"]) != "" {
		opts["password"] = p["password"]
		p["plugin-opts"] = opts
	}
}

func proxyUsable(p map[string]any) bool {
	if stringField(p["name"]) == "" {
		return false
	}
	if isLoopbackHost(stringField(p["server"])) {
		return false
	}
	if stringField(p["server"]) == "" {
		return false
	}
	typ := proxyTypeName(p)
	switch typ {
	case "hysteria2", "hy2":
		if stringField(p["obfs"]) != "" {
			if stringField(p["obfs-password"]) == "" && stringField(p["obfs_password"]) == "" {
				return false
			}
		}
		return stringField(p["password"]) != ""
	case "ss", "shadowsocks":
		return stringField(p["password"]) != "" || strings.EqualFold(stringField(p["cipher"]), "none")
	default:
		return true
	}
}

func proxyTypeName(p map[string]any) string {
	return strings.ToLower(strings.TrimSpace(stringField(p["type"])))
}

func stringField(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	default:
		if v == nil {
			return ""
		}
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

// sanitizeProviderLinkBundle 清洗 base64/纯文本分享链接订阅（常见于 v2rayN/clash 订阅）。
func sanitizeProviderLinkBundle(data []byte) ([]byte, int, bool) {
	text, encoded, ok := decodeProviderLinkBundle(data)
	if !ok {
		return nil, 0, false
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	removed := 0
	kept := make([]string, 0, len(lines))
	changed := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		cleaned, drop, repair := sanitizeShareLink(line)
		if drop {
			removed++
			changed = true
			continue
		}
		if repair {
			changed = true
		}
		kept = append(kept, cleaned)
	}
	if len(kept) == 0 {
		return nil, removed, true
	}
	if !changed {
		return data, removed, true
	}
	inner := strings.Join(kept, "\n")
	out := []byte(inner)
	if encoded {
		out = []byte(base64.StdEncoding.EncodeToString(out))
	}
	slog.Info("已清洗订阅链接", "removed", removed, "kept", len(kept))
	return out, removed, true
}

func decodeProviderLinkBundle(data []byte) (text string, wasBase64 bool, ok bool) {
	s := strings.TrimSpace(string(data))
	if strings.Contains(s, "proxies:") {
		return "", false, false
	}
	if strings.Contains(s, "://") {
		return s, false, true
	}
	decoded, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return "", false, false
	}
	inner := string(decoded)
	if !strings.Contains(inner, "://") {
		return "", false, false
	}
	return inner, true, true
}

func sanitizeShareLink(line string) (cleaned string, drop bool, changed bool) {
	if shareLinkUsesLoopback(line) {
		return "", true, true
	}
	lower := strings.ToLower(line)
	if !strings.HasPrefix(lower, "hysteria2://") && !strings.HasPrefix(lower, "hy2://") {
		return line, false, false
	}
	fixed, usable := repairHysteria2ShareLink(line)
	if !usable {
		return "", true, true
	}
	return fixed, false, fixed != line
}

func isLoopbackHost(host string) bool {
	host = strings.TrimSpace(strings.ToLower(host))
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}

// shareLinkUsesLoopback 识别订阅里常见的公告/占位节点（server 指向本机）。
func shareLinkUsesLoopback(line string) bool {
	lower := strings.ToLower(strings.TrimSpace(line))
	if lower == "" {
		return false
	}
	if strings.HasPrefix(lower, "vmess://") {
		return vmessShareLinkLoopback(line)
	}
	if strings.Contains(lower, "127.0.0.1") || strings.Contains(lower, "localhost") {
		return strings.Contains(lower, "://")
	}
	return false
}

func vmessShareLinkLoopback(line string) bool {
	payload := strings.TrimPrefix(line, "vmess://")
	if idx := strings.Index(payload, "#"); idx >= 0 {
		payload = payload[:idx]
	}
	decoded, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		decoded, err = base64.StdEncoding.DecodeString(payload)
	}
	if err != nil {
		return false
	}
	var cfg struct {
		Add string `json:"add"`
	}
	if err := json.Unmarshal(decoded, &cfg); err != nil {
		return false
	}
	return isLoopbackHost(cfg.Add)
}

func repairHysteria2ShareLink(line string) (string, bool) {
	hashIdx := strings.Index(line, "#")
	fragment := ""
	main := line
	if hashIdx >= 0 {
		fragment = line[hashIdx:]
		main = line[:hashIdx]
	}
	u, err := url.Parse(main)
	if err != nil {
		return line, false
	}
	q := u.Query()
	obfs := strings.TrimSpace(q.Get("obfs"))
	if obfs == "" || strings.EqualFold(obfs, "none") {
		if q.Has("obfs") {
			q.Del("obfs")
			u.RawQuery = q.Encode()
			return u.String() + fragment, true
		}
		return line, true
	}
	if q.Get("obfs-password") == "" && q.Get("obfs_password") == "" {
		if pwd, _ := u.User.Password(); pwd != "" {
			q.Set("obfs-password", pwd)
		} else if user := u.User.Username(); user != "" {
			q.Set("obfs-password", user)
		} else {
			q.Del("obfs")
		}
		u.RawQuery = q.Encode()
		return u.String() + fragment, true
	}
	return line, true
}
