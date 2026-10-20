//go:build !windows

package copilot

import (
	"os/exec"
	"syscall"
)

// killProcessTreeOnCancel puts the CLI in its own process group so that
// cancellation also kills the native runtime the npm loader spawns.
func killProcessTreeOnCancel(process *exec.Cmd) {
	process.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	process.Cancel = func() error {
		return syscall.Kill(-process.Process.Pid, syscall.SIGKILL)
	}
}
