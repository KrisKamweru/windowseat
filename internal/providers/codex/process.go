package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

const stopGrace = 2 * time.Second

var errStart = errors.New("start codex app-server")

// runAppServer starts a read-only app-server for one rate-limit read. The
// process inherits the ambient environment untouched; credentials stay with codex.
func runAppServer(ctx context.Context, path string) (json.RawMessage, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	cmd := exec.CommandContext(ctx, path, "-s", "read-only", "-a", "never", "app-server")
	cmd.WaitDelay = stopGrace
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errStart, err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errStart, err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("%w: %w", errStart, err)
	}

	result, err := exchange(ctx, stdout, stdin)

	stdin.Close()
	kill := time.AfterFunc(stopGrace, cancel)
	defer kill.Stop()
	_ = cmd.Wait() // The reply decides success; exit status after stdin closes carries no quota data.

	return result, err
}
