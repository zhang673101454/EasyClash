package backend

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
)

// CheckTunPrerequisites 检查开启 TUN 所需条件（Windows 需管理员 + wintun.dll）。
func CheckTunPrerequisites() error {
	if runtime.GOOS != "windows" {
		return nil
	}
	if !IsElevated() {
		return fmt.Errorf("TUN 模式需要管理员权限，请右键「以管理员身份运行」EasyClash")
	}
	path, err := FindWintunDLL()
	if err != nil {
		return err
	}
	slog.Info("已找到 wintun.dll", "path", path)
	return nil
}

// FindWintunDLL 在程序目录 / resources 中查找 wintun.dll。
func FindWintunDLL() (string, error) {
	var dirs []string
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		dirs = append(dirs, exeDir, filepath.Join(exeDir, "resources"))
	}
	if cwd, err := os.Getwd(); err == nil {
		dirs = append(dirs, cwd, filepath.Join(cwd, "resources"))
	}
	if bin, err := findMihomoBinary(); err == nil {
		dirs = append(dirs, filepath.Dir(bin))
	}

	seen := map[string]struct{}{}
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		if _, ok := seen[dir]; ok {
			continue
		}
		seen[dir] = struct{}{}
		candidate := filepath.Join(dir, "wintun.dll")
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
			continue
		}
		return candidate, nil
	}
	return "", fmt.Errorf("未找到 wintun.dll。请从 https://www.wintun.net/ 下载，并把 amd64\\wintun.dll 放到程序目录或 resources\\")
}

// EnsureWintunBeside 确保 mihomo 同目录有 wintun.dll（TUN 依赖）。
func EnsureWintunBeside(mihomoPath string) error {
	if runtime.GOOS != "windows" {
		return nil
	}
	dir := filepath.Dir(mihomoPath)
	dest := filepath.Join(dir, "wintun.dll")
	if info, err := os.Stat(dest); err == nil && !info.IsDir() {
		return nil
	}
	src, err := FindWintunDLL()
	if err != nil {
		return err
	}
	if filepath.Clean(src) == filepath.Clean(dest) {
		return nil
	}
	return copyFile(src, dest)
}

func copyFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer func() {
		_ = out.Close()
	}()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
