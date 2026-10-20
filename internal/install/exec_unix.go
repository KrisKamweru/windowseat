//go:build !windows

package install

import (
	"os/exec"
	"syscall"
)

func hideWindow(*exec.Cmd) {}

// detach starts the process in its own session, so it outlives the installer
// and its terminal.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
