//go:build windows

package backend

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

func terminateMihomoPID(pid int) error {
	if pid <= 0 || !isMihomoPID(pid) {
		return nil
	}
	cmd := exec.Command("taskkill", "/F", "/PID", strconv.Itoa(pid))
	prepareCommand(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func isMihomoPID(pid int) bool {
	cmd := exec.Command("tasklist", "/FI", "PID eq "+strconv.Itoa(pid), "/FI", "IMAGENAME eq mihomo.exe", "/NH", "/FO", "CSV")
	prepareCommand(cmd)
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(out)), "mihomo.exe")
}

func killMihomoByConfigDir(configDir string) {
	if strings.TrimSpace(configDir) == "" {
		return
	}
	script := `
$needle = $env:SIMPLEPROXY_CONFIG_DIR
Get-CimInstance Win32_Process -Filter "Name = 'mihomo.exe'" -ErrorAction SilentlyContinue |
  Where-Object { $_.CommandLine -and $_.CommandLine.IndexOf($needle, [StringComparison]::OrdinalIgnoreCase) -ge 0 } |
  ForEach-Object {
    Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue
  }
`
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.Env = append(cmd.Environ(), "SIMPLEPROXY_CONFIG_DIR="+configDir)
	prepareCommand(cmd)
	_ = cmd.Run()
}
