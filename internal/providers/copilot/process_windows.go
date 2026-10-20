//go:build windows

package copilot

import "os/exec"

// killProcessTreeOnCancel keeps the default kill on Windows. WaitDelay still
// releases the pipes if the loader leaves a native runtime behind.
func killProcessTreeOnCancel(*exec.Cmd) {}
