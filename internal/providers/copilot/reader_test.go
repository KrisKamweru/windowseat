package copilot

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"windowseat/internal/contracts"
)

const (
	fakeScenarioEnv = "WINDOWSEAT_FAKE_COPILOT"
	fakeLogEnv      = "WINDOWSEAT_FAKE_COPILOT_LOG"
)

// TestMain lets the test binary stand in for the copilot CLI, so Read runs
// against a real child process that replays recorded responses.
func TestMain(m *testing.M) {
	if scenario := os.Getenv(fakeScenarioEnv); scenario != "" {
		os.Exit(runFakeCopilot(scenario))
	}
	os.Exit(m.Run())
}

func TestReadSendsOnlyAllowedMethods(t *testing.T) {
	log := installFakeCopilot(t, "signed-in")

	snapshot := NewReader(Options{}).Read(t.Context())

	assertValid(t, snapshot)
	if snapshot.State != contracts.ProviderPartial || len(snapshot.Meters) != 3 {
		t.Fatalf("state = %q, meters = %d; want partial with 3 meters", snapshot.State, len(snapshot.Meters))
	}
	if snapshot.Source.RuntimeVersion != "1.0.80" || snapshot.Source.CommandVersion != "1.0.80" {
		t.Fatalf("Source = %+v", snapshot.Source)
	}
	want := []string{
		"args --headless --no-auto-update --stdio",
		"connect",
		"auth.getStatus",
		"refused server request",
		"account.getQuota",
		"runtime.shutdown",
	}
	if got := readLog(t, log); !slices.Equal(got, want) {
		t.Fatalf("runtime saw %q, want %q", got, want)
	}
}

func TestReadSignedOutSkipsQuota(t *testing.T) {
	log := installFakeCopilot(t, "signed-out")

	snapshot := NewReader(Options{}).Read(t.Context())

	assertValid(t, snapshot)
	if snapshot.State != contracts.ProviderAuthRequired || len(snapshot.Meters) != 0 {
		t.Fatalf("state = %q, meters = %d", snapshot.State, len(snapshot.Meters))
	}
	if slices.Contains(readLog(t, log), "account.getQuota") {
		t.Fatal("quota was requested without an authenticated account")
	}
}

func TestReadFailuresNeverReturnMeters(t *testing.T) {
	cases := []struct {
		scenario string
		state    contracts.ProviderState
		phase    contracts.ErrorPhase
	}{
		{scenario: "missing", state: contracts.ProviderUnavailable, phase: contracts.ErrorDiscover},
		{scenario: "crash", state: contracts.ProviderUnavailable, phase: contracts.ErrorStart},
		{scenario: "hang", state: contracts.ProviderUnavailable, phase: contracts.ErrorRead},
	}
	for _, tc := range cases {
		t.Run(tc.scenario, func(t *testing.T) {
			if tc.scenario == "missing" {
				t.Setenv("PATH", t.TempDir())
			} else {
				installFakeCopilot(t, tc.scenario)
			}

			snapshot := NewReader(Options{Timeout: 2 * time.Second}).Read(t.Context())

			assertValid(t, snapshot)
			if snapshot.State != tc.state || snapshot.Error.Phase != tc.phase || len(snapshot.Meters) != 0 {
				t.Fatalf("state = %q, error = %+v, meters = %d", snapshot.State, snapshot.Error, len(snapshot.Meters))
			}
		})
	}
}

func TestCallRefusesMethodsOutsideAllowlist(t *testing.T) {
	for _, name := range []method{"account.getCurrentAuth", "account.getAllUsers", "session.create"} {
		if err := (&runtime{}).call(name, struct{}{}, nil); err == nil {
			t.Errorf("call(%q) was allowed", name)
		}
	}
}

func installFakeCopilot(t *testing.T, scenario string) string {
	t.Helper()
	if goruntime.GOOS == "windows" {
		t.Skip("the fake copilot command is a POSIX shell script")
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	script := fmt.Sprintf("#!/bin/sh\nexec %q \"$@\"\n", binary)
	if err := os.WriteFile(filepath.Join(directory, "copilot"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(directory, "calls.log")
	t.Setenv("PATH", directory)
	t.Setenv(fakeScenarioEnv, scenario)
	t.Setenv(fakeLogEnv, log)
	return log
}

func readLog(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

func runFakeCopilot(scenario string) int {
	if slices.Contains(os.Args[1:], "--version") {
		fmt.Println("GitHub Copilot CLI 1.0.80.")
		return 0
	}
	if scenario == "crash" {
		return 1
	}

	log, err := os.OpenFile(os.Getenv(fakeLogEnv), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return 2
	}
	defer log.Close()
	fmt.Fprintln(log, "args", strings.Join(os.Args[1:], " "))

	// The client side reads what the server writes, so the roles swap.
	server := &runtime{stdin: os.Stdout, stdout: bufio.NewReader(os.Stdin)}
	for {
		request, err := server.receive()
		if err != nil {
			return 0
		}
		if request.Method == "" {
			fmt.Fprintln(log, "refused server request")
			continue
		}
		fmt.Fprintln(log, request.Method)
		if scenario == "hang" && request.Method == string(methodQuota) {
			time.Sleep(time.Hour)
		}
		if request.Method == string(methodAuthStatus) {
			_ = server.send(map[string]any{"jsonrpc": "2.0", "method": "session.lifecycle", "params": struct{}{}})
			_ = server.send(map[string]any{"jsonrpc": "2.0", "id": "server-1", "method": "sessionFs.readFile", "params": struct{}{}})
		}
		_ = server.send(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": fakeResult(scenario, request.Method)})
	}
}

func fakeResult(scenario, name string) json.RawMessage {
	fixture := map[string]string{
		string(methodConnect):    `{"ok":true,"protocolVersion":3,"version":"1.0.80"}`,
		string(methodAuthStatus): "@auth-status.json",
		string(methodQuota):      "@quota.json",
		string(methodShutdown):   `{}`,
	}[name]
	if scenario == "signed-out" && name == string(methodAuthStatus) {
		fixture = "@auth-status-signed-out.json"
	}
	if file, isFile := strings.CutPrefix(fixture, "@"); isFile {
		data, _ := os.ReadFile(filepath.Join("testdata", "1.0.80", file))
		return data
	}
	return json.RawMessage(fixture)
}
