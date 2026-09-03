package backend

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const settingsFileName = "settings.json"

const (
	DefaultAutoSelectIntervalMin = 15
	MinAutoSelectIntervalMin     = 5
	MaxAutoSelectIntervalMin     = 120
	AutoSelectImprovementMs      = 80
)

// AppSettings 持久化的应用设置。捕获方式：智能模式（默认，系统代理）或 TUN，始终为 rule。
type AppSettings struct {
	Tun                   bool `json:"tun"`
	Mode                  string `json:"mode"`
	AutoSelectBest        bool `json:"autoSelectBest"`
	AutoSelectIntervalMin int  `json:"autoSelectIntervalMin"`
}

func settingsPath(configDir string) string {
	return filepath.Join(configDir, settingsFileName)
}

// DefaultSettings 返回默认应用设置。
func DefaultSettings() AppSettings {
	return defaultSettings()
}

func defaultSettings() AppSettings {
	return AppSettings{
		Tun:                   false,
		Mode:                  "rule",
		AutoSelectBest:        true,
		AutoSelectIntervalMin: DefaultAutoSelectIntervalMin,
	}
}

func normalizeSettings(s AppSettings) AppSettings {
	s.Mode = "rule"
	if s.AutoSelectIntervalMin <= 0 {
		s.AutoSelectIntervalMin = DefaultAutoSelectIntervalMin
		s.AutoSelectBest = true
	}
	if s.AutoSelectIntervalMin < MinAutoSelectIntervalMin {
		s.AutoSelectIntervalMin = MinAutoSelectIntervalMin
	}
	if s.AutoSelectIntervalMin > MaxAutoSelectIntervalMin {
		s.AutoSelectIntervalMin = MaxAutoSelectIntervalMin
	}
	return s
}

// LoadSettings 读取设置，缺省为智能模式。
func LoadSettings(configDir string) AppSettings {
	data, err := os.ReadFile(settingsPath(configDir))
	if err != nil {
		return defaultSettings()
	}
	var s AppSettings
	if err := json.Unmarshal(data, &s); err != nil {
		slog.Warn("解析设置失败，将使用默认值", "error", err)
		return defaultSettings()
	}
	return normalizeSettings(s)
}

// SaveSettings 保存设置并同步到 config.yaml。
func SaveSettings(configDir string, s AppSettings) error {
	s = normalizeSettings(s)
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("编码设置失败: %w", err)
	}
	if err := os.WriteFile(settingsPath(configDir), data, 0o644); err != nil {
		return fmt.Errorf("写入设置失败: %w", err)
	}
	if err := ApplySettingsToConfig(configDir, s); err != nil {
		return err
	}
	return nil
}

// ApplySettingsToConfig 把 TUN / 智能模式写入 mihomo 配置（始终 rule，不提供 global）。
func ApplySettingsToConfig(configDir string, s AppSettings) error {
	cfg, err := loadConfigMap(configDir)
	if err != nil {
		return err
	}
	cfg["mode"] = "rule"
	if s.Tun {
		cfg["tun"] = map[string]any{
			"enable":                true,
			"stack":                 "mixed",
			"auto-route":            true,
			"auto-detect-interface": true,
			"strict-route":          false,
			"dns-hijack":            []any{"any:53"},
		}
	} else {
		cfg["tun"] = map[string]any{
			"enable": false,
		}
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("编码配置失败: %w", err)
	}
	path := filepath.Join(configDir, configFileName)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("写入 TUN/模式配置失败: %w", err)
	}
	return nil
}
