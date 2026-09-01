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
	proxyBackupServerValue        = "EasyClashProxyServer"
	proxyBackupOverrideValue      = "EasyClashProxyOverride"
	proxyBackupEnableValue        = "EasyClashProxyEnable"
	proxyBackupValidValue         = "EasyClashProxyBackup"
)

func setWindowsProxy(enable bool) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, internetSettingsKey, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return fmt.Errorf("打开代理注册表失败: %w", err)
	}
	defer func() {
		if closeErr := key.Close(); closeErr != nil {
			slog.Warn("关闭注册表键失败", "error", closeErr)
		}
	}()

	if enable {
		if err := backupWindowsProxy(key); err != nil {
			slog.Warn("备份原系统代理失败", "error", err)
		}
		if err := key.SetStringValue("ProxyServer", proxyServerValue); err != nil {
			return fmt.Errorf("写入 ProxyServer 失败: %w", err)
		}
		if err := key.SetStringValue("ProxyOverride", "localhost;127.*;10.*;172.16.*;192.168.*;<local>"); err != nil {
			return fmt.Errorf("写入 ProxyOverride 失败: %w", err)
		}
		if err := key.SetDWordValue("ProxyEnable", 1); err != nil {
			return fmt.Errorf("写入 ProxyEnable 失败: %w", err)
		}
	} else {
		if restored, err := restoreWindowsProxy(key); err != nil {
			slog.Warn("还原原系统代理失败，将直接关闭", "error", err)
			if setErr := key.SetDWordValue("ProxyEnable", 0); setErr != nil {
				return fmt.Errorf("写入 ProxyEnable 失败: %w", setErr)
			}
		} else if !restored {
			if err := key.SetDWordValue("ProxyEnable", 0); err != nil {
				return fmt.Errorf("写入 ProxyEnable 失败: %w", err)
			}
		}
	}

	if err := notifyProxyChange(); err != nil {
		return err
	}
	return nil
}

func backupWindowsProxy(key registry.Key) error {
	if alreadyOurs(key) {
		return nil
	}
	server, _, _ := key.GetStringValue("ProxyServer")
	override, _, _ := key.GetStringValue("ProxyOverride")
	enable, _, err := key.GetIntegerValue("ProxyEnable")
	if err != nil {
		enable = 0
	}
	if err := key.SetStringValue(proxyBackupServerValue, server); err != nil {
		return err
	}
	if err := key.SetStringValue(proxyBackupOverrideValue, override); err != nil {
		return err
	}
	if err := key.SetDWordValue(proxyBackupEnableValue, uint32(enable)); err != nil {
		return err
	}
	return key.SetDWordValue(proxyBackupValidValue, 1)
}

func alreadyOurs(key registry.Key) bool {
	server, _, err := key.GetStringValue("ProxyServer")
	if err != nil {
		return false
	}
	enable, _, err := key.GetIntegerValue("ProxyEnable")
	if err != nil || enable == 0 {
		return false
	}
	return server == proxyServerValue
}

func windowsProxyIsOurs() (bool, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, internetSettingsKey, registry.QUERY_VALUE)
	if err != nil {
		return false, err
	}
	defer key.Close()
	return alreadyOurs(key), nil
}

func restoreWindowsProxy(key registry.Key) (bool, error) {
	valid, _, err := key.GetIntegerValue(proxyBackupValidValue)
	if err != nil || valid == 0 {
		return false, nil
	}
	server, _, _ := key.GetStringValue(proxyBackupServerValue)
	override, _, _ := key.GetStringValue(proxyBackupOverrideValue)
	enable, _, err := key.GetIntegerValue(proxyBackupEnableValue)
	if err != nil {
		enable = 0
	}

	if server != "" {
		if err := key.SetStringValue("ProxyServer", server); err != nil {
			return false, err
		}
	}
	if override != "" {
		if err := key.SetStringValue("ProxyOverride", override); err != nil {
			return false, err
		}
	}
	if err := key.SetDWordValue("ProxyEnable", uint32(enable)); err != nil {
		return false, err
	}
	_ = key.DeleteValue(proxyBackupValidValue)
	_ = key.DeleteValue(proxyBackupServerValue)
	_ = key.DeleteValue(proxyBackupOverrideValue)
	_ = key.DeleteValue(proxyBackupEnableValue)
	return true, nil
}

// forceDisableEasyClashProxyWindows 紧急清理：仅当当前代理指向本机 7890 时关闭。
func forceDisableEasyClashProxyWindows() error {
	key, err := registry.OpenKey(registry.CURRENT_USER, internetSettingsKey, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()

	server, _, _ := key.GetStringValue("ProxyServer")
	if server == proxyServerValue {
		_ = key.SetDWordValue("ProxyEnable", 0)
	}
	_ = key.DeleteValue(proxyBackupValidValue)
	_ = key.DeleteValue(proxyBackupServerValue)
	_ = key.DeleteValue(proxyBackupOverrideValue)
	_ = key.DeleteValue(proxyBackupEnableValue)
	return notifyProxyChange()
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
