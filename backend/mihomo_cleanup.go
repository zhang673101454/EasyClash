package backend

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const mihomoPIDFile = "mihomo.pid"

func killLeftoverMihomo(configDir string) {
	killPIDFile(configDir)
	killMihomoByConfigDir(configDir)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !mihomoAPIReachable() {
			return
		}
		time.Sleep(80 * time.Millisecond)
	}
}

func writeMihomoPID(configDir string, pid int) {
	if configDir == "" || pid <= 0 {
		return
	}
	path := filepath.Join(configDir, mihomoPIDFile)
	if err := os.WriteFile(path, []byte(strconv.Itoa(pid)), 0o644); err != nil {
		slog.Warn("写入 mihomo pid 失败", "error", err)
	}
}

func clearMihomoPID(configDir string) {
	if configDir == "" {
		return
	}
	_ = os.Remove(filepath.Join(configDir, mihomoPIDFile))
}

func killPIDFile(configDir string) {
	data, err := os.ReadFile(filepath.Join(configDir, mihomoPIDFile))
	if err != nil {
		return
	}
	pid, convErr := strconv.Atoi(strings.TrimSpace(string(data)))
	_ = os.Remove(filepath.Join(configDir, mihomoPIDFile))
	if convErr != nil || pid <= 0 {
		return
	}
	_ = terminateMihomoPID(pid)
}

func mihomoAPIReachable() bool {
	client := NewMihomoClient()
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	return client.WaitReady(ctx) == nil
}
