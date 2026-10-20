//go:build windows

package command

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
)

// KillTreeOnCancel makes cancellation kill cmd and every process it started,
// such as the native runtime an npm loader spawns. Call it before Start.
func KillTreeOnCancel(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		if killTree(cmd.Process.Pid) == nil {
			return nil
		}
		return cmd.Process.Kill()
	}
}

func killTree(pid int) error {
	systemRoot := os.Getenv("SystemRoot")
	if systemRoot == "" {
		return errors.New("SystemRoot is not set")
	}
	taskkill := filepath.Join(systemRoot, "System32", "taskkill.exe")
	return exec.Command(taskkill, "/T", "/F", "/PID", strconv.Itoa(pid)).Run()
}
