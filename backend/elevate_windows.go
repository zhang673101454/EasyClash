//go:build windows

package backend

import (
	"golang.org/x/sys/windows"
)

// IsElevated 当前进程是否以管理员身份运行。
func IsElevated() bool {
	var token windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &token); err != nil {
		return false
	}
	defer token.Close()
	return token.IsElevated()
}
