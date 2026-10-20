package claude

import (
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

var usageArgs = []string{
	"-p", "/usage",
	"--output-format", "stream-json",
	"--verbose",
	"--no-session-persistence",
	"--settings", `{"disableAllHooks":true}`,
	"--strict-mcp-config",
	"--mcp-config", `{"mcpServers":{}}`,
}

// nestedSessionMarkers make claude believe it runs inside another Claude Code
// session, which changes its behaviour when Windowseat is launched from one.
var nestedSessionMarkers = []string{"CLAUDECODE", "CLAUDE_CODE_ENTRYPOINT"}

const waitAfterCancel = 5 * time.Second

func runUsageCommand(ctx context.Context, commandPath string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, commandPath, usageArgs...)
	cmd.Dir = os.TempDir()
	cmd.Env = childEnvironment(os.Environ())
	cmd.Stderr = io.Discard
	cmd.WaitDelay = waitAfterCancel
	return cmd.Output()
}

// childEnvironment passes the ambient environment through untouched except
// for the nested-session markers. It only compares variable names.
func childEnvironment(environ []string) []string {
	kept := make([]string, 0, len(environ))
	for _, entry := range environ {
		name, _, _ := strings.Cut(entry, "=")
		if !isNestedSessionMarker(name) {
			kept = append(kept, entry)
		}
	}
	return kept
}

func isNestedSessionMarker(name string) bool {
	for _, marker := range nestedSessionMarkers {
		if strings.EqualFold(name, marker) {
			return true
		}
	}
	return false
}
