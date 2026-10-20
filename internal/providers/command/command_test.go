package command

import "testing"

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
