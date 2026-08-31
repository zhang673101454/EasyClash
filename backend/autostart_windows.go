//go:build windows

package backend

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows/registry"
)

const autoStartValueName = "EasyClash"
const autoStartLegacyName = "SimpleProxy"
const autoStartFlag = "--autostart"

func autoStartKeyPath() string {
	return `Software\Microsoft\Windows\CurrentVersion\Run`
}

// AutoStartEnabled 是否已登记开机启动。
func AutoStartEnabled() (bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, autoStartKeyPath(), registry.QUERY_VALUE)
	if err != nil {
		if err == registry.ErrNotExist {
			return false, nil
		}
		return false, err
	}
	defer k.Close()
	if hasValue(k, autoStartValueName) || hasValue(k, autoStartLegacyName) {
		return true, nil
	}
	return false, nil
}

func hasValue(k registry.Key, name string) bool {
	_, _, err := k.GetStringValue(name)
	return err == nil
}

// SetAutoStart 写入或删除当前用户的开机启动项。
func SetAutoStart(enabled bool) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, autoStartKeyPath(), registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("打开开机启动注册表失败: %w", err)
	}
	defer k.Close()

	if !enabled {
		_ = k.DeleteValue(autoStartLegacyName)
		err := k.DeleteValue(autoStartValueName)
		if err == registry.ErrNotExist {
			return nil
		}
		return err
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取程序路径失败: %w", err)
	}
	value := `"` + exe + `" ` + autoStartFlag
	if err := k.SetStringValue(autoStartValueName, value); err != nil {
		return fmt.Errorf("写入开机启动项失败: %w", err)
	}
	_ = k.DeleteValue(autoStartLegacyName)
	return nil
}

// LaunchedFromAutoStart 是否由开机启动项拉起。
func LaunchedFromAutoStart() bool {
	for _, arg := range os.Args[1:] {
		if arg == autoStartFlag || arg == "-autostart" {
			return true
		}
	}
	return false
}

// RefreshAutoStartCommand 已开启开机启动时，刷新启动命令以带上 --autostart。
func RefreshAutoStartCommand() {
	enabled, err := AutoStartEnabled()
	if err != nil || !enabled {
		return
	}
	_ = SetAutoStart(true)
}
