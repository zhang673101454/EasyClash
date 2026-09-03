package backend

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	geoipFileName   = "geoip.metadb"
	geositeFileName = "geosite.dat"
)

var bundledGeoFiles = []string{geoipFileName, geositeFileName}

// SmartRoutingRules 国内直连、其余走代理（需 geoip.metadb + geosite.dat）。
func SmartRoutingRules() []any {
	return BuildRoutingRules(nil)
}

// BuildRoutingRules 生成路由规则。
// 已启用订阅域名 → DIRECT（避免 mihomo 拉取自身订阅走 PROXY 死循环）；
// 未启用订阅域名 → PROXY（经 mixed-port 刷新时走当前代理，避免被 GEOSITE,cn 直连）。
func BuildRoutingRules(items []Subscription) []any {
	rules := []any{
		"IP-CIDR,127.0.0.0/8,DIRECT",
		"IP-CIDR,10.0.0.0/8,DIRECT",
		"IP-CIDR,172.16.0.0/12,DIRECT",
		"IP-CIDR,192.168.0.0/16,DIRECT",
	}

	directHosts := map[string]struct{}{}
	proxyHosts := make([]string, 0, len(items))
	for _, item := range items {
		host := subscriptionHostFromURL(item.URL)
		if host == "" {
			continue
		}
		if item.Enabled {
			if _, exists := directHosts[host]; !exists {
				directHosts[host] = struct{}{}
				rules = append(rules, "DOMAIN-SUFFIX,"+host+",DIRECT")
			}
			continue
		}
		if _, exists := directHosts[host]; exists {
			continue
		}
		proxyHosts = append(proxyHosts, host)
	}
	sort.Strings(proxyHosts)
	for _, host := range uniqueSortedStrings(proxyHosts) {
		rules = append(rules, "DOMAIN-SUFFIX,"+host+",PROXY")
	}

	rules = append(rules,
		"GEOSITE,cn,DIRECT",
		"GEOIP,CN,DIRECT",
		"MATCH,PROXY",
	)
	return rules
}

func uniqueSortedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func subscriptionHostFromURL(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Host == "" {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}

// EnsureGeoDataInConfigDir 把内置 geodata 复制到 mihomo 配置目录（-d）。
func EnsureGeoDataInConfigDir(configDir string) error {
	if configDir == "" {
		return fmt.Errorf("配置目录为空")
	}
	var missing []string
	for _, name := range bundledGeoFiles {
		dest := filepath.Join(configDir, name)
		if info, err := os.Stat(dest); err == nil && !info.IsDir() && info.Size() > 0 {
			continue
		}
		src, err := findBundledGeoFile(name)
		if err != nil {
			missing = append(missing, name)
			continue
		}
		if err := copyFile(src, dest); err != nil {
			return fmt.Errorf("复制 %s 失败: %w", name, err)
		}
		slog.Info("已安装 geodata", "file", name, "dest", dest)
	}
	if len(missing) > 0 {
		return fmt.Errorf("缺少 geodata 文件 %v，请运行 scripts/fetch-geodata.ps1 或重新安装", missing)
	}
	return nil
}

func findBundledGeoFile(name string) (string, error) {
	var dirs []string
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		dirs = append(dirs, exeDir, filepath.Join(exeDir, "resources"))
	}
	if cwd, err := os.Getwd(); err == nil {
		dirs = append(dirs, cwd, filepath.Join(cwd, "resources"))
	}

	seen := map[string]struct{}{}
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		if _, ok := seen[dir]; ok {
			continue
		}
		seen[dir] = struct{}{}
		candidate := filepath.Join(dir, name)
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() || info.Size() == 0 {
			continue
		}
		return candidate, nil
	}
	return "", fmt.Errorf("未找到 %s", name)
}
