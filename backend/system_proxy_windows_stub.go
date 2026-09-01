//go:build !windows

package backend

import "fmt"

func setWindowsProxy(enable bool) error {
	return fmt.Errorf("内部错误: 非 Windows 系统不应调用 setWindowsProxy (enable=%v)", enable)
}

func forceDisableEasyClashProxyWindows() error {
	return nil
}

func windowsProxyIsOurs() (bool, error) {
	return false, nil
}
