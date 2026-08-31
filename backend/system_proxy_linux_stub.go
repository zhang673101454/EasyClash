//go:build !linux

package backend

import "fmt"

func setLinuxProxy(enable bool) error {
	return fmt.Errorf("内部错误: 非 Linux 系统不应调用 setLinuxProxy (enable=%v)", enable)
}
