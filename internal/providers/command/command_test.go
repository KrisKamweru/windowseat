package command

import (
	"runtime"
	"testing"
)

func TestParseVersionReadsEachProviderFormat(t *testing.T) {
	cases := map[string]string{
		"2.1.304 (Claude Code)\n":      "2.1.304",
		"codex-cli 0.163.0\n":          "0.163.0",
		"GitHub Copilot CLI 1.0.80.\n": "1.0.80",
	}
	for output, want := range cases {
		got, err := ParseVersion(output)
		if err != nil || got != want {
			t.Errorf("ParseVersion(%q) = %q, %v; want %q", output, got, err, want)
		}
	}
}

func TestParseVersionRejectsOutputWithoutVersion(t *testing.T) {
	if _, err := ParseVersion("unknown"); err == nil {
		t.Fatal("expected an error")
	}
}

func TestOnWindowsDriveRejectsOnlyWSLDriveMounts(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("WSL drive mounts exist only on Linux")
	}
	cases := map[string]bool{
		"/mnt/c/nvm4w/nodejs/codex":       true,
		"/mnt/D/tools/claude":             true,
		"/usr/local/bin/codex":            false,
		"/home/kris/.local/bin/claude":    false,
		"/mnt/data/bin/copilot":           false,
		"/home/kris/mnt/c/fake/bin/codex": false,
	}
	for path, want := range cases {
		if got := onWindowsDrive(path); got != want {
			t.Errorf("onWindowsDrive(%q) = %v; want %v", path, got, want)
		}
	}
}
