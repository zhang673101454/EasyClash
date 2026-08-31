//go:build !windows

package backend

import "fmt"

// AutoStartEnabled 非 Windows 暂不支持开机启动。
func AutoStartEnabled() (bool, error) {
	return false, nil
}

// SetAutoStart 非 Windows 暂不支持开机启动。
func SetAutoStart(enabled bool) error {
	if !enabled {
		return nil
	}
	return fmt.Errorf("当前系统暂不支持开机自动启动")
}

func LaunchedFromAutoStart() bool {
	return false
}

func RefreshAutoStartCommand() {}
