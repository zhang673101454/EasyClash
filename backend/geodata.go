package backend

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

const (
	geoipFileName   = "geoip.metadb"
	geositeFileName = "geosite.dat"
)

var bundledGeoFiles = []string{geoipFileName, geositeFileName}

// SmartRoutingRules 国内直连、其余走代理（需 geoip.metadb + geosite.dat）。
func SmartRoutingRules() []any {
	return []any{
		"IP-CIDR,127.0.0.0/8,DIRECT",
		"IP-CIDR,10.0.0.0/8,DIRECT",
		"IP-CIDR,172.16.0.0/12,DIRECT",
		"IP-CIDR,192.168.0.0/16,DIRECT",
		"GEOSITE,cn,DIRECT",
		"GEOIP,CN,DIRECT",
		"MATCH,PROXY",
	}
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
