package install

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
)

// Step is one change to the machine. String says what it changes.
type Step interface {
	fmt.Stringer
	run() error
}

// Run prints each step and carries it out, stopping at the first failure.
// A dry run only prints.
func Run(w io.Writer, plan Plan, dryRun bool) error {
	if dryRun {
		fmt.Fprintln(w, "Dry run. These steps would run:")
	}
	for _, step := range plan.Steps {
		fmt.Fprintln(w, "  "+step.String())
		if dryRun {
			continue
		}
		if err := step.run(); err != nil {
			return fmt.Errorf("%s: %w", step, err)
		}
	}
	for _, note := range plan.Notes {
		fmt.Fprintln(w, note)
	}
	return nil
}

// command runs a program. A command that may fail is one that undoes
// something which might not be there, such as stopping a missing service.
type command struct {
	args    []string
	mayFail bool
}

func (c command) String() string {
	quoted := make([]string, len(c.args))
	for i, arg := range c.args {
		quoted[i] = arg
		if strings.ContainsAny(arg, " \t") {
			quoted[i] = strconv.Quote(arg)
		}
	}
	return "run " + strings.Join(quoted, " ")
}

func (c command) run() error {
	cmd := exec.Command(c.args[0], c.args[1:]...)
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err == nil || c.mayFail {
		return nil
	}
	if text := strings.TrimSpace(string(out)); text != "" {
		return fmt.Errorf("%w: %s", err, text)
	}
	return err
}

type writeFile struct {
	path, data string
}

func (s writeFile) String() string { return "write " + s.path }

func (s writeFile) run() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(s.path, []byte(s.data), 0o644)
}

// copyFile installs an executable. It writes beside the target and renames,
// so a binary that is still mapped by a process is replaced, not rewritten.
type copyFile struct {
	from, to string
}

func (s copyFile) String() string { return "copy " + s.from + " to " + s.to }

func (s copyFile) run() error {
	src, err := os.Open(s.from)
	if err != nil {
		return err
	}
	defer src.Close()
	if err := os.MkdirAll(filepath.Dir(s.to), 0o755); err != nil {
		return err
	}
	tmp := s.to + ".new"
	dst, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	_, err = io.Copy(dst, src)
	if err = errors.Join(err, dst.Close()); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, s.to)
}

type removeFile struct {
	path string
}

func (s removeFile) String() string { return "delete " + s.path }

func (s removeFile) run() error {
	return deleteWithRetry(func() error { return ignoreMissing(os.Remove(s.path)) })
}

type removeDir struct {
	path string
}

func (s removeDir) String() string { return "delete " + s.path + " and everything in it" }

func (s removeDir) run() error {
	return deleteWithRetry(func() error { return os.RemoveAll(s.path) })
}

type removeEmptyDir struct {
	path string
}

func (s removeEmptyDir) String() string { return "delete " + s.path + " if it is empty" }

func (s removeEmptyDir) run() error {
	entries, err := os.ReadDir(s.path)
	if err != nil || len(entries) > 0 {
		return ignoreMissing(err)
	}
	return deleteWithRetry(func() error { return os.Remove(s.path) })
}

// moveAside renames a program out of the way. Windows refuses to delete a
// running executable but lets it be renamed.
type moveAside struct {
	from, to string
}

func (s moveAside) String() string {
	return "move " + s.from + " to " + s.to + ", since Windows cannot delete a running program"
}

func (s moveAside) run() error {
	return ignoreMissing(os.Rename(s.from, s.to))
}

// stopProcesses signals every process running the given executable. It reads
// /proc, so it only finds processes on Linux.
type stopProcesses struct {
	exe string
}

func (s stopProcesses) String() string { return "stop running " + s.exe }

func (s stopProcesses) run() error {
	links, err := filepath.Glob("/proc/[0-9]*/exe")
	if err != nil {
		return err
	}
	for _, link := range links {
		target, err := os.Readlink(link)
		if err != nil || strings.TrimSuffix(target, " (deleted)") != s.exe {
			continue
		}
		pid, _ := strconv.Atoi(filepath.Base(filepath.Dir(link)))
		if process, err := os.FindProcess(pid); err == nil {
			process.Signal(syscall.SIGTERM)
		}
	}
	return nil
}

type startDetached struct {
	exe string
}

func (s startDetached) String() string { return "start " + s.exe }

func (s startDetached) run() error {
	cmd := exec.Command(s.exe)
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// registerTask creates or replaces a scheduled task from its XML definition.
// schtasks reads the file most reliably as UTF-16, so it is written that way.
type registerTask struct {
	name, xml, tempDir string
}

func (s registerTask) String() string {
	return fmt.Sprintf("register logon task %q with schtasks /Create /XML", s.name)
}

func (s registerTask) run() error {
	file, err := os.CreateTemp(s.tempDir, "windowseat-task-*.xml")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, err = file.Write(utf16LE(s.xml))
	if err = errors.Join(err, file.Close()); err != nil {
		return err
	}
	return command{args: []string{"schtasks", "/Create", "/TN", s.name, "/XML", file.Name(), "/F"}}.run()
}

func utf16LE(text string) []byte {
	data := []byte{0xFF, 0xFE}
	for _, unit := range utf16.Encode([]rune(text)) {
		data = binary.LittleEndian.AppendUint16(data, unit)
	}
	return data
}

func ignoreMissing(err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// Windows refuses to delete a file while any program has it open. A service
// that was just stopped, or an antivirus scan, usually lets go within moments.
const (
	deleteAttempts = 10
	deleteWait     = 500 * time.Millisecond
)

func deleteWithRetry(remove func() error) error {
	if runtime.GOOS != "windows" {
		return remove()
	}
	return retryDenied(remove, deleteAttempts, deleteWait)
}

func retryDenied(remove func() error, attempts int, wait time.Duration) error {
	err := remove()
	for i := 1; i < attempts && errors.Is(err, fs.ErrPermission); i++ {
		time.Sleep(wait)
		err = remove()
	}
	if errors.Is(err, fs.ErrPermission) {
		return fmt.Errorf("%w Another program still has it open, often antivirus. Windows finishes the delete once that program lets go, or after a restart", err)
	}
	return err
}
