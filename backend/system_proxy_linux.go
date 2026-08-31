//go:build linux

package backend

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
)

func setLinuxProxy(enable bool) error {
	proxyURL := fmt.Sprintf("http://%s", proxyServerValue)
	if enable {
		if err := os.Setenv("http_proxy", proxyURL); err != nil {
			return fmt.Errorf("设置 http_proxy 失败: %w", err)
		}
		if err := os.Setenv("https_proxy", proxyURL); err != nil {
			return fmt.Errorf("设置 https_proxy 失败: %w", err)
		}
		if err := os.Setenv("HTTP_PROXY", proxyURL); err != nil {
			return fmt.Errorf("设置 HTTP_PROXY 失败: %w", err)
		}
		if err := os.Setenv("HTTPS_PROXY", proxyURL); err != nil {
			return fmt.Errorf("设置 HTTPS_PROXY 失败: %w", err)
		}
	} else {
		for _, key := range []string{"http_proxy", "https_proxy", "HTTP_PROXY", "HTTPS_PROXY"} {
			if err := os.Unsetenv(key); err != nil {
				return fmt.Errorf("清除 %s 失败: %w", key, err)
			}
		}
	}

	if err := setGnomeProxy(enable); err != nil {
		slog.Warn("GNOME 系统代理设置失败，已降级为仅设置环境变量", "error", err)
	}
	return nil
}

func setGnomeProxy(enable bool) error {
	if _, err := exec.LookPath("gsettings"); err != nil {
		return fmt.Errorf("未找到 gsettings: %w", err)
	}

	if !enable {
		return runGSettings("set", "org.gnome.system.proxy", "mode", "none")
	}

	if err := runGSettings("set", "org.gnome.system.proxy", "mode", "manual"); err != nil {
		return err
	}
	if err := runGSettings("set", "org.gnome.system.proxy.http", "host", "127.0.0.1"); err != nil {
		return err
	}
	if err := runGSettings("set", "org.gnome.system.proxy.http", "port", strconv.Itoa(mixedPort)); err != nil {
		return err
	}
	if err := runGSettings("set", "org.gnome.system.proxy.https", "host", "127.0.0.1"); err != nil {
		return err
	}
	if err := runGSettings("set", "org.gnome.system.proxy.https", "port", strconv.Itoa(mixedPort)); err != nil {
		return err
	}
	return nil
}

func runGSettings(args ...string) error {
	cmd := exec.Command("gsettings", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("gsettings %v 失败: %w (%s)", args, err, string(out))
	}
	return nil
}
