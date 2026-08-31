//go:build darwin

package backend

import (
	"fmt"
	"os/exec"
	"strings"
)

func setDarwinProxy(enable bool) error {
	services, err := darwinNetworkServices()
	if err != nil {
		return err
	}

	host := "127.0.0.1"
	port := fmt.Sprintf("%d", mixedPort)

	for _, service := range services {
		if enable {
			if err := runNetworkSetup("-setwebproxy", service, host, port); err != nil {
				return err
			}
			if err := runNetworkSetup("-setsecurewebproxy", service, host, port); err != nil {
				return err
			}
			if err := runNetworkSetup("-setwebproxystate", service, "on"); err != nil {
				return err
			}
			if err := runNetworkSetup("-setsecurewebproxystate", service, "on"); err != nil {
				return err
			}
			continue
		}

		if err := runNetworkSetup("-setwebproxystate", service, "off"); err != nil {
			return err
		}
		if err := runNetworkSetup("-setsecurewebproxystate", service, "off"); err != nil {
			return err
		}
	}
	return nil
}

func darwinNetworkServices() ([]string, error) {
	out, err := exec.Command("networksetup", "-listallnetworkservices").Output()
	if err != nil {
		return nil, fmt.Errorf("列出网络服务失败: %w", err)
	}

	lines := strings.Split(string(out), "\n")
	var services []string
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || i == 0 {
			continue
		}
		if strings.HasPrefix(line, "*") {
			continue
		}
		services = append(services, line)
	}
	if len(services) == 0 {
		return nil, fmt.Errorf("未找到可用网络服务")
	}
	return services, nil
}

func runNetworkSetup(args ...string) error {
	cmd := exec.Command("networksetup", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("networksetup %s 失败: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}
