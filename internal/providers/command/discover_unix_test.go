//go:build !windows

package command

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestDiscoverReturnsPromptlyWhenAChildHoldsStdout(t *testing.T) {
	scripts := map[string]string{
		"wrapper outlives the deadline": "%s 30 & echo 1.2.3; wait",
		"wrapper exits first":           "%s 3 & echo 1.2.3",
	}
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("sleep not found")
	}
	for name, script := range scripts {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "provider")
			if err := os.WriteFile(path, []byte("#!/bin/sh\n"+fmt.Sprintf(script, sleep)+"\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir)
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()

			started := time.Now()
			_, _ = Discover(ctx, "provider")

			if elapsed := time.Since(started); elapsed > versionWaitDelay+time.Second {
				t.Fatalf("Discover took %v", elapsed)
			}
		})
	}
}
