package command

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

var ErrNotFound = errors.New("provider command not found")

var versionPattern = regexp.MustCompile(`\b([0-9]+\.[0-9]+\.[0-9]+)\b`)

// windowsDriveMount matches the paths WSL uses for Windows drives. A command
// found there runs on the Windows host and reports the Windows account.
var windowsDriveMount = regexp.MustCompile(`^/mnt/[a-zA-Z]/`)

// versionWaitDelay bounds how long `--version` output is drained after the
// command exits or its context ends, in case a child process holds stdout.
const versionWaitDelay = time.Second

// Command is a provider CLI selected from the user's PATH.
type Command struct {
	Path    string
	Version string
}

// Discover resolves name on PATH and reads its version from `name --version`.
func Discover(ctx context.Context, name string) (Command, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return Command{}, fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	if onWindowsDrive(path) {
		return Command{}, fmt.Errorf("%w: %s resolves to Windows-hosted %s", ErrNotFound, name, path)
	}
	output, err := readVersionOutput(ctx, path)
	if err != nil {
		return Command{}, fmt.Errorf("%s --version: %w", name, err)
	}
	version, err := ParseVersion(output)
	if err != nil {
		return Command{}, err
	}
	return Command{Path: path, Version: version}, nil
}

func readVersionOutput(ctx context.Context, path string) (string, error) {
	var stdout bytes.Buffer
	cmd := exec.CommandContext(ctx, path, "--version")
	cmd.Stdout = &stdout
	cmd.Stderr = io.Discard
	cmd.WaitDelay = versionWaitDelay
	if err := StartTree(cmd); err != nil {
		return "", err
	}
	err := cmd.Wait()
	return stdout.String(), err
}

// onWindowsDrive reports whether path, after symlinks, lives on a Windows
// drive mounted into WSL. Windows and WSL are separate hosts.
func onWindowsDrive(path string) bool {
	if runtime.GOOS != "linux" {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	return windowsDriveMount.MatchString(path)
}

// ParseVersion extracts the first three-part version from CLI version output.
func ParseVersion(output string) (string, error) {
	match := versionPattern.FindStringSubmatch(output)
	if match == nil {
		return "", fmt.Errorf("no version in %q", strings.TrimSpace(output))
	}
	return match[1], nil
}
