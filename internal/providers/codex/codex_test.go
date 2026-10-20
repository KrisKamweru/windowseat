package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"windowseat/internal/contracts"
	"windowseat/internal/providers/command"
)

var installedCodex = command.Command{Path: "/usr/local/bin/codex", Version: "0.163.0"}

func fakeReader(discovered command.Command, discoverErr error, result json.RawMessage, readErr error) *reader {
	return &reader{
		discover: func(context.Context, string) (command.Command, error) { return discovered, discoverErr },
		readRateLimits: func(context.Context, string) (json.RawMessage, error) {
			return result, readErr
		},
		now:     func() time.Time { return probeTime },
		timeout: time.Second,
	}
}

func validate(t *testing.T, snapshot contracts.ProviderSnapshot) {
	t.Helper()
	envelope := contracts.Snapshot{
		SchemaVersion: contracts.SchemaVersion,
		GeneratedAt:   snapshot.ReceivedAt,
		Providers:     []contracts.ProviderSnapshot{snapshot},
	}
	if err := envelope.Validate(); err != nil {
		t.Fatalf("snapshot failed validation: %v\n%+v", err, snapshot)
	}
}

func TestReadReturnsHealthySnapshotFromCurrentShape(t *testing.T) {
	snapshot := fakeReader(installedCodex, nil, fixtureResult(t, "0.163.0"), nil).Read(context.Background())

	validate(t, snapshot)
	if snapshot.Provider != contracts.ProviderCodex || snapshot.State != contracts.ProviderHealthy {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	want := contracts.Source{
		Name:                 contracts.SourceCodexAppServer,
		CommandPath:          installedCodex.Path,
		CommandVersion:       installedCodex.Version,
		ResponseProfile:      "codex_rate_limits_0_149_1",
		ProfilePreferredFrom: "0.149.1",
	}
	if *snapshot.Source != want {
		t.Errorf("source = %+v", *snapshot.Source)
	}
	if snapshot.Freshness != (contracts.Freshness{State: contracts.FreshnessCurrent, MaxAgeSeconds: 300}) {
		t.Errorf("freshness = %+v", snapshot.Freshness)
	}
	if snapshot.Plan != "plus" || len(snapshot.Meters) != 2 {
		t.Errorf("plan %q meters %+v", snapshot.Plan, snapshot.Meters)
	}
}

func TestReadNeverExposesAccountOrResetCreditDetails(t *testing.T) {
	result := json.RawMessage(`{"rateLimits":{"primary":{"usedPercent":1,"windowDurationMins":300}},
		"accountId":"acct-secret","rateLimitResetCredits":{"availableCount":2,"credits":[{"id":"credit-secret"}]}}`)

	snapshot := fakeReader(installedCodex, nil, result, nil).Read(context.Background())

	encoded, _ := json.Marshal(snapshot)
	for _, secret := range []string{"acct-secret", "credit-secret", "availableCount"} {
		if strings.Contains(string(encoded), secret) {
			t.Errorf("snapshot exposes %q: %s", secret, encoded)
		}
	}
}

func TestReadMarksOlderVersionsAsBeforeProfile(t *testing.T) {
	old := command.Command{Path: "/usr/bin/codex", Version: "0.140.0"}

	snapshot := fakeReader(old, nil, fixtureResult(t, "0.149.1"), nil).Read(context.Background())

	validate(t, snapshot)
	if !snapshot.Source.VersionBeforeProfile {
		t.Errorf("source = %+v", snapshot.Source)
	}
}

func TestReadReturnsPartialSnapshotWhenFieldsDegrade(t *testing.T) {
	result := json.RawMessage(`{"rateLimits":{"primary":{"usedPercent":5,"windowDurationMins":300,"resetsAt":1000}}}`)

	snapshot := fakeReader(installedCodex, nil, result, nil).Read(context.Background())

	validate(t, snapshot)
	if snapshot.State != contracts.ProviderPartial || snapshot.Completeness != contracts.CompletenessPartial {
		t.Errorf("state %q completeness %q", snapshot.State, snapshot.Completeness)
	}
}

func TestReadReportsFailuresWithoutMeters(t *testing.T) {
	cases := map[string]struct {
		discoverErr error
		result      json.RawMessage
		readErr     error
		state       contracts.ProviderState
		phase       contracts.ErrorPhase
	}{
		"codex missing": {
			discoverErr: fmt.Errorf("%w: codex", command.ErrNotFound),
			state:       contracts.ProviderUnavailable, phase: contracts.ErrorDiscover,
		},
		"logged out": {
			readErr: &rpcError{Method: methodReadRateLimits, Code: -32600, Message: "Please log in with `codex login`"},
			state:   contracts.ProviderAuthRequired, phase: contracts.ErrorAuthenticate,
		},
		"chatgpt auth required": {
			readErr: &rpcError{Method: methodReadRateLimits, Code: -32600, Message: "chatgpt authentication required to read rate limits"},
			state:   contracts.ProviderAuthRequired, phase: contracts.ErrorAuthenticate,
		},
		"other rpc error": {
			readErr: &rpcError{Method: methodReadRateLimits, Code: -32603, Message: "backend unavailable"},
			state:   contracts.ProviderUnavailable, phase: contracts.ErrorRead,
		},
		"initialize rejected": {
			readErr: &rpcError{Method: methodInitialize, Code: -32603, Message: "boom"},
			state:   contracts.ProviderUnavailable, phase: contracts.ErrorStart,
		},
		"process failed to start": {
			readErr: fmt.Errorf("%w: exec format error", errStart),
			state:   contracts.ProviderUnavailable, phase: contracts.ErrorStart,
		},
		"timeout": {
			readErr: context.DeadlineExceeded,
			state:   contracts.ProviderUnavailable, phase: contracts.ErrorRead,
		},
		"malformed line": {
			readErr: fmt.Errorf("%w: not json", errMalformedMessage),
			state:   contracts.ProviderMalformedResponse, phase: contracts.ErrorParse,
		},
		"result without windows": {
			result: json.RawMessage(`{"rateLimits":null}`),
			state:  contracts.ProviderMalformedResponse, phase: contracts.ErrorParse,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			snapshot := fakeReader(installedCodex, tc.discoverErr, tc.result, tc.readErr).Read(context.Background())

			validate(t, snapshot)
			if snapshot.State != tc.state || snapshot.Error == nil || snapshot.Error.Phase != tc.phase {
				t.Errorf("state %q error %+v; want %q/%q", snapshot.State, snapshot.Error, tc.state, tc.phase)
			}
		})
	}
}

func TestReadDoesNotCopyProviderErrorText(t *testing.T) {
	readErr := &rpcError{Method: methodReadRateLimits, Code: -1, Message: "user@example.com is not logged in"}

	snapshot := fakeReader(installedCodex, nil, nil, readErr).Read(context.Background())

	encoded, _ := json.Marshal(snapshot)
	if strings.Contains(string(encoded), "user@example.com") {
		t.Errorf("snapshot copies provider error text: %s", encoded)
	}
}

func TestLiveRead(t *testing.T) {
	if os.Getenv("WINDOWSEAT_LIVE") != "1" {
		t.Skip("set WINDOWSEAT_LIVE=1 to read the installed Codex account")
	}

	snapshot := NewReader(Options{}).Read(context.Background())

	validate(t, snapshot)
	if snapshot.Error != nil {
		t.Fatalf("live read failed: %+v", snapshot.Error)
	}
	t.Logf("codex %s via %s: state %s plan %s", snapshot.Source.CommandVersion, snapshot.Source.ResponseProfile, snapshot.State, snapshot.Plan)
	for _, meter := range snapshot.Meters {
		encoded, _ := json.Marshal(meter)
		t.Logf("  %s", encoded)
	}
}
