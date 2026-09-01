package backend

import (
	"fmt"
	"log/slog"
	"runtime"
)

// SetSystemProxy 按当前操作系统开启或关闭系统全局代理。
func SetSystemProxy(enable bool) error {
	slog.Info("设置系统代理", "enable", enable, "os", runtime.GOOS, "addr", proxyServerValue)

	var err error
	switch runtime.GOOS {
	case "windows":
		err = setWindowsProxy(enable)
	case "darwin":
		err = setDarwinProxy(enable)
	case "linux":
		err = setLinuxProxy(enable)
	default:
		err = fmt.Errorf("不支持的操作系统: %s", runtime.GOOS)
	}
	if err != nil {
		return err
	}
	slog.Info("系统代理已更新", "enable", enable)
	return nil
}

// ForceDisableEasyClashProxy 在常规关闭失败或异常退出后，强制清掉本软件留下的系统代理。
// 非 Windows 上为兼容桩，返回 nil。
func ForceDisableEasyClashProxy() error {
	if runtime.GOOS != "windows" {
		return nil
	}
	return forceDisableEasyClashProxyWindows()
}

// DisableSystemProxyIfOurs 仅当系统代理当前指向本软件地址时关闭/还原，避免误伤用户自己的代理。
func DisableSystemProxyIfOurs() error {
	if runtime.GOOS != "windows" {
		return nil
	}
	ours, err := windowsProxyIsOurs()
	if err != nil || !ours {
		return err
	}
	if err := SetSystemProxy(false); err != nil {
		return forceDisableEasyClashProxyWindows()
	}
	return nil
}
