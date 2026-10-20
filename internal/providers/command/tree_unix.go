//go:build !windows

package command

import (
	"os/exec"
	"syscall"
)

// StartTree starts cmd so that cancelling its context kills cmd and every
// process it started, such as the native runtime an npm loader spawns.
func StartTree(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	return cmd.Start()
}
