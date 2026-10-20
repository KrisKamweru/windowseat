//go:build windows

package command

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// StartTree starts cmd inside a job object, so cancelling its context ends
// cmd and every process it started, such as the native runtime an npm loader
// spawns. Windows also ends the tree when the service exits. A process the
// child starts in the moment before it joins the job escapes it.
func StartTree(cmd *exec.Cmd) error {
	job, err := newKillOnCloseJob()
	if err != nil {
		return fmt.Errorf("create job object: %w", err)
	}
	tree := &jobTree{job: job}
	cmd.Cancel = tree.terminate
	if err := cmd.Start(); err != nil {
		tree.close()
		return err
	}

	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, uint32(cmd.Process.Pid))
	if err == nil {
		err = windows.AssignProcessToJobObject(job, process)
	}
	if err != nil {
		if process != 0 {
			_ = windows.CloseHandle(process)
		}
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		tree.close()
		return fmt.Errorf("add %s to job object: %w", cmd.Path, err)
	}

	// Closing the job once the child exits ends anything it left behind.
	go func() {
		_, _ = windows.WaitForSingleObject(process, windows.INFINITE)
		_ = windows.CloseHandle(process)
		tree.close()
	}()
	return nil
}

type jobTree struct {
	mu  sync.Mutex
	job windows.Handle
}

func (t *jobTree) terminate() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.job == 0 {
		return os.ErrProcessDone
	}
	return windows.TerminateJobObject(t.job, 1)
}

func (t *jobTree) close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.job != 0 {
		_ = windows.CloseHandle(t.job)
		t.job = 0
	}
}

func newKillOnCloseJob() (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		_ = windows.CloseHandle(job)
		return 0, err
	}
	return job, nil
}
