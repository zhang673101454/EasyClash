//go:build !windows

package backend

import (
	"os"
	"os/exec"
	"strings"
)

func terminateMihomoPID(pid int) error {
	if pid <= 0 {
		return nil
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return nil
	}
	_ = proc.Kill()
	return nil
}

func killMihomoByConfigDir(configDir string) {
	if strings.TrimSpace(configDir) == "" {
		return
	}
	cmd := exec.Command("pkill", "-f", configDir)
	_ = cmd.Run()
}
