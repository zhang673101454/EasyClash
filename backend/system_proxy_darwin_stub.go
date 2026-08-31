//go:build !darwin

package backend

import "fmt"

func setDarwinProxy(enable bool) error {
	return fmt.Errorf("内部错误: 非 macOS 系统不应调用 setDarwinProxy (enable=%v)", enable)
}
