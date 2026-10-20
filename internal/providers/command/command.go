package command

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

var ErrNotFound = errors.New("provider command not found")

var versionPattern = regexp.MustCompile(`\b([0-9]+\.[0-9]+\.[0-9]+)\b`)

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
	output, err := exec.CommandContext(ctx, path, "--version").Output()
	if err != nil {
		return Command{}, fmt.Errorf("%s --version: %w", name, err)
	}
	version, err := ParseVersion(string(output))
	if err != nil {
		return Command{}, err
	}
	return Command{Path: path, Version: version}, nil
}

// ParseVersion extracts the first three-part version from CLI version output.
func ParseVersion(output string) (string, error) {
	match := versionPattern.FindStringSubmatch(output)
	if match == nil {
		return "", fmt.Errorf("no version in %q", strings.TrimSpace(output))
	}
	return match[1], nil
}
