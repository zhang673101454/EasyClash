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
