package ptyusage

import (
	"errors"
	"path/filepath"
	"slices"
	"testing"
)

func TestClaudeArgumentsAreIsolatedAndInformational(t *testing.T) {
	arguments := claudeArguments(artifactSessionID)
	want := []string{
		"--safe-mode",
		"--ax-screen-reader",
		"--no-chrome",
		"--strict-mcp-config",
		"--mcp-config", `{"mcpServers":{}}`,
		"--tools", "",
		"--permission-mode", "dontAsk",
		"--model", "haiku",
		"--session-id", artifactSessionID,
		"--settings", `{"disableAllHooks":true}`,
	}
	if !slices.Equal(arguments, want) {
		t.Fatalf("claudeArguments() = %#v, want %#v", arguments, want)
	}
}

func TestValidateProbeConfigRequiresPolicyCheck(t *testing.T) {
	config := ProbeConfig{
		CommandPath:      "claude",
		Version:          MinimumTestedVersion,
		WorkingDirectory: t.TempDir(),
		ProjectsRoot:     t.TempDir(),
		SessionID:        artifactSessionID,
	}

	err := validateProbeConfig(config)
	if !errors.Is(err, ErrManagedPolicyPresent) {
		t.Fatalf("validateProbeConfig() error = %v, want ErrManagedPolicyPresent", err)
	}
}

func TestValidateProbeConfigAcceptsHigherVersion(t *testing.T) {
	config := ProbeConfig{
		CommandPath:      "claude",
		Version:          "2.1.999",
		WorkingDirectory: t.TempDir(),
		ProjectsRoot:     t.TempDir(),
		SessionID:        artifactSessionID,
		CheckPolicy:      func() error { return nil },
	}

	if err := validateProbeConfig(config); err != nil {
		t.Fatalf("validateProbeConfig() error = %v", err)
	}
}

func TestProjectDirectoryMatchesObservedLinuxEncoding(t *testing.T) {
	root := filepath.FromSlash("/home/person/.claude/projects")
	workingDirectory := filepath.FromSlash("/tmp/windowseat-claude-pty-probe")
	want := filepath.Join(root, "-tmp-windowseat-claude-pty-probe")
	if got := ProjectDirectory(root, workingDirectory); got != want {
		t.Fatalf("ProjectDirectory() = %q, want %q", got, want)
	}
}

func TestNewSessionIDReturnsVersionFourUUID(t *testing.T) {
	sessionID, err := NewSessionID()
	if err != nil {
		t.Fatalf("NewSessionID() error = %v", err)
	}
	if !uuidPattern.MatchString(sessionID) {
		t.Fatalf("NewSessionID() = %q, want version 4 UUID", sessionID)
	}
}
