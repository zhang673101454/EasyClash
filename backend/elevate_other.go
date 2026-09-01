//go:build !windows

package backend

// IsElevated 非 Windows 平台视为已具备权限。
func IsElevated() bool {
	return true
}
