//go:build !windows

package command

import (
	"bufio"
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestCancelKillsTheWholeProcessTree(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", "sleep 30 & echo $!; wait")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := StartTree(cmd); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	grandchild, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Kill(grandchild, syscall.SIGKILL)

	cancel()
	_ = cmd.Wait()

	deadline := time.Now().Add(5 * time.Second)
	for !errors.Is(syscall.Kill(grandchild, 0), syscall.ESRCH) {
		if time.Now().After(deadline) {
			t.Fatal("grandchild survived cancellation")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
