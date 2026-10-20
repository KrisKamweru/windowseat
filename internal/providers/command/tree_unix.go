//go:build !windows

package command

import (
	"os/exec"
	"syscall"
)

// KillTreeOnCancel makes cancellation kill cmd and every process it started,
// such as the native runtime an npm loader spawns. Call it before Start.
func KillTreeOnCancel(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
