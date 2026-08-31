//go:build windows

package backend

import (
	"fmt"
	"log/slog"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	internetSettingsKey           = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`
	internetOptionSettingsChanged = 39
	internetOptionRefresh         = 37
)

func setWindowsProxy(enable bool) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, internetSettingsKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("打开代理注册表失败: %w", err)
	}
	defer func() {
		if closeErr := key.Close(); closeErr != nil {
			slog.Warn("关闭注册表键失败", "error", closeErr)
		}
	}()

	var enableValue uint32
	if enable {
		enableValue = 1
		if err := key.SetStringValue("ProxyServer", proxyServerValue); err != nil {
			return fmt.Errorf("写入 ProxyServer 失败: %w", err)
		}
		if err := key.SetStringValue("ProxyOverride", "localhost;127.*;10.*;172.16.*;192.168.*;<local>"); err != nil {
			return fmt.Errorf("写入 ProxyOverride 失败: %w", err)
		}
	}

	if err := key.SetDWordValue("ProxyEnable", enableValue); err != nil {
		return fmt.Errorf("写入 ProxyEnable 失败: %w", err)
	}

	if err := notifyProxyChange(); err != nil {
		return err
	}
	return nil
}

func notifyProxyChange() error {
	wininet := windows.NewLazySystemDLL("wininet.dll")
	proc := wininet.NewProc("InternetSetOptionW")
	if err := wininet.Load(); err != nil {
		return fmt.Errorf("加载 wininet.dll 失败: %w", err)
	}
	if err := proc.Find(); err != nil {
		return fmt.Errorf("查找 InternetSetOptionW 失败: %w", err)
	}

	r1, _, callErr := proc.Call(0, uintptr(internetOptionSettingsChanged), 0, 0)
	if r1 == 0 {
		return fmt.Errorf("InternetSetOption SETTINGS_CHANGED 失败: %w", callErr)
	}
	r1, _, callErr = proc.Call(0, uintptr(internetOptionRefresh), 0, 0)
	if r1 == 0 {
		return fmt.Errorf("InternetSetOption REFRESH 失败: %w", callErr)
	}
	return nil
}
