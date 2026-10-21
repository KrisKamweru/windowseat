package install

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type fakeStep struct {
	name string
	err  error
	ran  *[]string
}

func (s fakeStep) String() string { return s.name }

func (s fakeStep) run() error {
	*s.ran = append(*s.ran, s.name)
	return s.err
}

func TestRunDryRunPrintsAndRunsNothing(t *testing.T) {
	var ran []string
	plan := Plan{Steps: []Step{fakeStep{"one", nil, &ran}, fakeStep{"two", nil, &ran}}, Notes: []string{"a note"}}
	var out bytes.Buffer

	if err := Run(&out, plan, true); err != nil {
		t.Fatal(err)
	}
	if len(ran) != 0 {
		t.Errorf("dry run ran %q", ran)
	}
	for _, line := range []string{"one", "two", "a note"} {
		if !strings.Contains(out.String(), line) {
			t.Errorf("output lacks %q:\n%s", line, out.String())
		}
	}
}

func TestRunStopsAtFirstError(t *testing.T) {
	var ran []string
	failure := errors.New("boom")
	plan := Plan{Steps: []Step{fakeStep{"one", nil, &ran}, fakeStep{"two", failure, &ran}, fakeStep{"three", nil, &ran}}}

	err := Run(&bytes.Buffer{}, plan, false)
	if !errors.Is(err, failure) || !strings.Contains(err.Error(), "two") {
		t.Fatalf("err = %v, want the failure and the step that failed", err)
	}
	if strings.Join(ran, ",") != "one,two" {
		t.Errorf("ran %q, want one,two", ran)
	}
}

func TestCopyFileReplacesTarget(t *testing.T) {
	dir := t.TempDir()
	from := filepath.Join(dir, "src")
	to := filepath.Join(dir, "bin", "dst")
	writeTestFile(t, from, "new")
	writeTestFile(t, to, "old")

	if err := (copyFile{from: from, to: to}).run(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(to)
	if err != nil || string(data) != "new" {
		t.Fatalf("target = %q, %v", data, err)
	}
	if info, _ := os.Stat(to); runtime.GOOS != "windows" && info.Mode().Perm()&0o100 == 0 {
		t.Errorf("target mode %v is not executable", info.Mode())
	}
	if entries, _ := os.ReadDir(filepath.Dir(to)); len(entries) != 1 {
		t.Errorf("copy left %d files behind, want 1", len(entries))
	}
}

func TestWriteFileCreatesParents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b", "unit")
	if err := (writeFile{path: path, data: "x"}).run(); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); string(data) != "x" {
		t.Errorf("file = %q", data)
	}
}

func TestRemoveStepsTolerateMissingPaths(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	for _, step := range []Step{removeFile{path: missing}, removeDir{path: missing}, removeEmptyDir{path: missing}} {
		if err := step.run(); err != nil {
			t.Errorf("%s: %v", step, err)
		}
	}
}

func TestRemoveEmptyDirKeepsDirWithFiles(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "keep"), "x")
	if err := (removeEmptyDir{path: dir}).run(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("dir with files was removed: %v", err)
	}

	empty := filepath.Join(dir, "empty")
	os.Mkdir(empty, 0o755)
	if err := (removeEmptyDir{path: empty}).run(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(empty); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("empty dir still exists: %v", err)
	}
}

func TestMoveAsideTolerateMissingSource(t *testing.T) {
	dir := t.TempDir()
	if err := (moveAside{from: filepath.Join(dir, "missing"), to: filepath.Join(dir, "aside")}).run(); err != nil {
		t.Fatal(err)
	}

	from := filepath.Join(dir, "running")
	writeTestFile(t, from, "x")
	if err := (moveAside{from: from, to: filepath.Join(dir, "aside")}).run(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "aside")); err != nil {
		t.Error(err)
	}
}

func TestCommandMayFail(t *testing.T) {
	missing := []string{filepath.Join(t.TempDir(), "no-such-command")}
	if err := (command{args: missing, mayFail: true}).run(); err != nil {
		t.Errorf("tolerated command failed: %v", err)
	}
	if err := (command{args: missing}).run(); err == nil {
		t.Error("missing command succeeded")
	}
}

func TestUTF16WithBOM(t *testing.T) {
	got := utf16LE("<é>")
	want := []byte{0xFF, 0xFE, '<', 0, 0xE9, 0, '>', 0}
	if !bytes.Equal(got, want) {
		t.Errorf("utf16LE = % x, want % x", got, want)
	}
}

func writeTestFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestRetryDeniedSucceedsOnceTheHolderLetsGo(t *testing.T) {
	calls := 0
	remove := func() error {
		calls++
		if calls < 3 {
			return fs.ErrPermission
		}
		return nil
	}
	if err := retryDenied(remove, 5, 0); err != nil || calls != 3 {
		t.Fatalf("err = %v after %d calls, want nil after 3", err, calls)
	}
}

func TestRetryDeniedExplainsALastingDenial(t *testing.T) {
	calls := 0
	remove := func() error { calls++; return fs.ErrPermission }
	err := retryDenied(remove, 4, 0)
	if !errors.Is(err, fs.ErrPermission) || !strings.Contains(err.Error(), "restart") || calls != 4 {
		t.Fatalf("err = %v after %d calls, want an explained permission error after 4", err, calls)
	}
}

func TestRetryDeniedLeavesOtherErrorsAlone(t *testing.T) {
	calls := 0
	other := errors.New("disk on fire")
	remove := func() error { calls++; return other }
	if err := retryDenied(remove, 4, 0); err != other || calls != 1 {
		t.Fatalf("err = %v after %d calls, want the original error after 1", err, calls)
	}
}
