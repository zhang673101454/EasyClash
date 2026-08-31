//go:build !windows

package backend

import "os/exec"

func prepareCommand(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
}
