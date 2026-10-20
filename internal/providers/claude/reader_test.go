package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"windowseat/internal/contracts"
	"windowseat/internal/providers/command"
)

var fixtureTime = time.Date(2026, 10, 20, 12, 10, 25, 0, time.UTC)

func TestReadsFixtureAsHealthySnapshot(t *testing.T) {
	snapshot := readWith(t, fixtureOutput(t), nil)

	if snapshot.State != contracts.ProviderHealthy || snapshot.Completeness != contracts.CompletenessExact {
		t.Fatalf("state = %q/%q, want healthy/exact", snapshot.State, snapshot.Completeness)
	}
	want := []struct {
		id      string
		label   string
		kind    contracts.MeterKind
		model   string
		percent float64
		resets  string
	}{
		{"session", "Current session", contracts.MeterRollingWindow, "", 31, "2026-10-20T14:49:59.531455Z"},
		{"weekly_all", "Current week (all models)", contracts.MeterRollingWindow, "", 5, "2026-10-27T05:59:59.531476Z"},
		{"weekly_fable", "Current week (Fable)", contracts.MeterModelLimit, "Fable", 4, "2026-10-27T05:59:59.531636Z"},
	}
	if len(snapshot.Meters) != len(want) {
		t.Fatalf("meters = %d, want %d", len(snapshot.Meters), len(want))
	}
	for i, w := range want {
		meter := snapshot.Meters[i]
		if meter.ID != w.id || meter.Label != w.label || meter.Kind != w.kind || meter.Model != w.model {
			t.Errorf("meter %d = %s/%q/%s/%q, want %s/%q/%s/%q", i, meter.ID, meter.Label, meter.Kind, meter.Model, w.id, w.label, w.kind, w.model)
		}
		if meter.UsedPercentage == nil || *meter.UsedPercentage != w.percent {
			t.Errorf("meter %s used_percentage = %v, want %v", w.id, meter.UsedPercentage, w.percent)
		}
		if meter.Window == nil || meter.Window.ResetsAt == nil || meter.Window.ResetsAt.UTC().Format(time.RFC3339Nano) != w.resets {
			t.Errorf("meter %s window = %+v, want resets_at %s", w.id, meter.Window, w.resets)
		}
	}

	source := snapshot.Source
	if source.Name != contracts.SourceClaudeUsageReport || source.CommandPath != "/usr/bin/claude" || source.CommandVersion != "2.1.304" {
		t.Errorf("source = %+v", source)
	}
	if source.ResponseProfile != "claude_usage_report_2_1_304" || source.ProfilePreferredFrom != "2.1.304" || source.VersionBeforeProfile {
		t.Errorf("source profile = %+v", source)
	}
	if !snapshot.ObservedAt.Equal(fixtureTime) || snapshot.Freshness.MaxAgeSeconds != 300 {
		t.Errorf("observed_at = %v, freshness = %+v", snapshot.ObservedAt, snapshot.Freshness)
	}
}

func TestOlderVersionUsesOldestProfileAndRecordsIt(t *testing.T) {
	reader := NewReader(Options{
		Discover: discoverAt("2.1.200"),
		Run:      runReturning(fixtureOutput(t), nil),
		Now:      func() time.Time { return fixtureTime },
	})
	snapshot := reader.Read(context.Background())
	requireValid(t, snapshot)

	if !snapshot.Source.VersionBeforeProfile || snapshot.State != contracts.ProviderHealthy {
		t.Fatalf("source = %+v, state = %q", snapshot.Source, snapshot.State)
	}
}

func TestPastResetKeepsRawValueAndMarksPartial(t *testing.T) {
	afterSessionReset := time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)
	reader := NewReader(Options{
		Discover: discoverAt("2.1.304"),
		Run:      runReturning(fixtureOutput(t), nil),
		Now:      func() time.Time { return afterSessionReset },
	})
	snapshot := reader.Read(context.Background())
	requireValid(t, snapshot)

	if snapshot.State != contracts.ProviderPartial || snapshot.Completeness != contracts.CompletenessPartial {
		t.Fatalf("state = %q/%q, want partial", snapshot.State, snapshot.Completeness)
	}
	session := snapshot.Meters[0]
	if session.Window.ResetsAt != nil || session.Window.RawResetAt != "2026-10-20T14:49:59.531455+00:00" {
		t.Errorf("session window = %+v", session.Window)
	}
	if *session.UsedPercentage != 31 {
		t.Errorf("session used_percentage = %v, want preserved 31", *session.UsedPercentage)
	}
	wantIssue := contracts.ObservationIssue{
		Code:    "reset_time_not_future",
		MeterID: "session",
		Field:   "window.resets_at",
		Message: "The provider reset time was not later than the observation time.",
	}
	if len(snapshot.Issues) != 1 || snapshot.Issues[0] != wantIssue {
		t.Errorf("issues = %+v", snapshot.Issues)
	}
}

func TestUnreadableFieldsBecomeIssues(t *testing.T) {
	output := mutateFixture(t, func(assistant, _ map[string]any) {
		limits := rateLimits(assistant)["limits"].([]any)
		limits[0].(map[string]any)["percent"] = 140.0
		limits[1].(map[string]any)["resets_at"] = "next tuesday"
	})
	snapshot := readWith(t, output, nil)

	if snapshot.State != contracts.ProviderPartial {
		t.Fatalf("state = %q, want partial", snapshot.State)
	}
	if snapshot.Meters[0].UsedPercentage != nil || snapshot.Meters[0].Window.ResetsAt == nil {
		t.Errorf("session meter = %+v", snapshot.Meters[0])
	}
	if snapshot.Meters[1].Window.RawResetAt != "next tuesday" || snapshot.Meters[1].UsedPercentage == nil {
		t.Errorf("weekly_all meter = %+v", snapshot.Meters[1])
	}
	codes := issueCodes(snapshot)
	if codes["session/used_percentage"] != "percent_out_of_range" || codes["weekly_all/window.resets_at"] != "reset_time_unreadable" {
		t.Errorf("issues = %+v", snapshot.Issues)
	}
}

func TestUnknownLimitKindIsPreserved(t *testing.T) {
	output := mutateFixture(t, func(assistant, _ map[string]any) {
		report := rateLimits(assistant)
		report["limits"] = append(report["limits"].([]any), map[string]any{
			"kind": "monthly_opus", "percent": 12.0, "resets_at": "2026-11-01T00:00:00+00:00",
			"scope": map[string]any{"model": map[string]any{"display_name": "Opus 5"}},
		})
	})
	snapshot := readWith(t, output, nil)

	if snapshot.State != contracts.ProviderHealthy || len(snapshot.Meters) != 4 {
		t.Fatalf("state = %q, meters = %d", snapshot.State, len(snapshot.Meters))
	}
	meter := snapshot.Meters[3]
	if meter.ID != "monthly_opus_opus_5" || meter.Kind != contracts.MeterModelLimit || meter.Model != "Opus 5" || meter.Label != "monthly_opus (Opus 5)" {
		t.Errorf("meter = %+v", meter)
	}
}

func TestEnabledExtraUsageBecomesCreditMeter(t *testing.T) {
	output := mutateFixture(t, func(assistant, _ map[string]any) {
		rateLimits(assistant)["extra_usage"] = map[string]any{
			"is_enabled": true, "monthly_limit": 50.0, "used_credits": 12.5, "utilization": 25.0, "currency": "USD",
		}
	})
	snapshot := readWith(t, output, nil)

	meter := snapshot.Meters[len(snapshot.Meters)-1]
	if meter.ID != "extra_usage" || meter.Kind != contracts.MeterCreditBalance || meter.Unit != "USD" {
		t.Fatalf("meter = %+v", meter)
	}
	if *meter.Used != 12.5 || *meter.Entitlement != 50 {
		t.Errorf("used = %v, entitlement = %v", *meter.Used, *meter.Entitlement)
	}
}

func TestSpendingAnyQuotaFailsClosed(t *testing.T) {
	mutations := map[string]func(assistant, result map[string]any){
		"turns":         func(_, result map[string]any) { result["num_turns"] = 1.0 },
		"cost":          func(_, result map[string]any) { result["total_cost_usd"] = 0.0001 },
		"output tokens": func(_, result map[string]any) { result["usage"].(map[string]any)["output_tokens"] = 3.0 },
		"input tokens":  func(_, result map[string]any) { result["usage"].(map[string]any)["input_tokens"] = 9.0 },
		"not a local command": func(_, result map[string]any) {
			delete(result, "local_command")
		},
		"assistant output tokens": func(assistant, _ map[string]any) { messageUsage(assistant)["output_tokens"] = 3.0 },
		"assistant cache tokens":  func(assistant, _ map[string]any) { messageUsage(assistant)["cache_read_input_tokens"] = 900.0 },
		"model reply without report": func(assistant, _ map[string]any) {
			delete(assistant, "usage_report")
			assistant["message"].(map[string]any)["model"] = "claude-opus-4-5"
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			snapshot := readWith(t, mutateFixture(t, mutate), nil)
			requireFailure(t, snapshot, contracts.ProviderMalformedResponse, "usage_safety_check_failed")
		})
	}
}

func TestMissingResultLineFailsClosed(t *testing.T) {
	lines := bytes.Split(bytes.TrimSpace(fixtureOutput(t)), []byte("\n"))
	output := bytes.Join(lines[:2], []byte("\n"))

	snapshot := readWith(t, output, nil)

	requireFailure(t, snapshot, contracts.ProviderMalformedResponse, "usage_result_missing")
}

func TestLoggedOutResultIsAuthRequired(t *testing.T) {
	output := []byte(`{"type":"system","subtype":"init"}
{"type":"result","subtype":"success","is_error":true,"num_turns":0,"total_cost_usd":0,"usage":{"input_tokens":0,"output_tokens":0},"result":"Not logged in · Please run /login","local_command":"usage"}
`)
	snapshot := readWith(t, output, errors.New("exit status 1"))

	requireFailure(t, snapshot, contracts.ProviderAuthRequired, "not_authenticated")
	if snapshot.Error.Phase != contracts.ErrorAuthenticate {
		t.Errorf("phase = %q", snapshot.Error.Phase)
	}
}

func TestMissingUsageReportIsMalformed(t *testing.T) {
	output := mutateFixture(t, func(assistant, _ map[string]any) { delete(assistant, "usage_report") })

	snapshot := readWith(t, output, nil)

	requireFailure(t, snapshot, contracts.ProviderMalformedResponse, "usage_report_missing")
}

func TestEmptyLimitsAreMalformed(t *testing.T) {
	output := mutateFixture(t, func(assistant, _ map[string]any) { rateLimits(assistant)["limits"] = []any{} })

	snapshot := readWith(t, output, nil)

	requireFailure(t, snapshot, contracts.ProviderMalformedResponse, "usage_report_empty")
}

func TestCommandErrorWithoutOutputIsUnavailable(t *testing.T) {
	snapshot := readWith(t, nil, errors.New("exit status 2"))

	requireFailure(t, snapshot, contracts.ProviderUnavailable, "usage_command_failed")
}

func TestTimeoutIsUnavailable(t *testing.T) {
	reader := NewReader(Options{
		Discover: discoverAt("2.1.304"),
		Run: func(ctx context.Context, _ string) ([]byte, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
		Timeout: time.Millisecond,
		Now:     func() time.Time { return fixtureTime },
	})

	snapshot := reader.Read(context.Background())

	requireFailure(t, snapshot, contracts.ProviderUnavailable, "usage_command_timeout")
}

func TestMissingCommandIsUnavailableAtDiscover(t *testing.T) {
	reader := NewReader(Options{
		Discover: func(context.Context, string) (command.Command, error) {
			return command.Command{}, command.ErrNotFound
		},
		Now: func() time.Time { return fixtureTime },
	})

	snapshot := reader.Read(context.Background())

	requireFailure(t, snapshot, contracts.ProviderUnavailable, "command_not_found")
	if snapshot.Error.Phase != contracts.ErrorDiscover || snapshot.Source != nil {
		t.Errorf("phase = %q, source = %+v", snapshot.Error.Phase, snapshot.Source)
	}
}

func TestChildEnvironmentDropsNestedClaudeMarkers(t *testing.T) {
	env := childEnvironment([]string{"PATH=/bin", "CLAUDECODE=1", "CLAUDE_CODE_ENTRYPOINT=cli", "HOME=/home/x"})

	if len(env) != 2 || env[0] != "PATH=/bin" || env[1] != "HOME=/home/x" {
		t.Errorf("env = %v", env)
	}
}

func readWith(t *testing.T, output []byte, runErr error) contracts.ProviderSnapshot {
	t.Helper()
	reader := NewReader(Options{
		Discover: discoverAt("2.1.304"),
		Run:      runReturning(output, runErr),
		Now:      func() time.Time { return fixtureTime },
	})
	snapshot := reader.Read(context.Background())
	requireValid(t, snapshot)
	return snapshot
}

func requireValid(t *testing.T, snapshot contracts.ProviderSnapshot) {
	t.Helper()
	wrapped := contracts.Snapshot{
		SchemaVersion: contracts.SchemaVersion,
		GeneratedAt:   snapshot.ReceivedAt,
		Providers:     []contracts.ProviderSnapshot{snapshot},
	}
	if err := wrapped.Validate(); err != nil {
		t.Fatalf("snapshot fails validation: %v\n%+v", err, snapshot)
	}
}

func requireFailure(t *testing.T, snapshot contracts.ProviderSnapshot, state contracts.ProviderState, code string) {
	t.Helper()
	requireValid(t, snapshot)
	if snapshot.State != state || snapshot.Error == nil || snapshot.Error.Code != code {
		t.Fatalf("state = %q, error = %+v; want %q/%q", snapshot.State, snapshot.Error, state, code)
	}
	if len(snapshot.Meters) != 0 {
		t.Fatalf("failure returned meters: %+v", snapshot.Meters)
	}
}

func discoverAt(version string) func(context.Context, string) (command.Command, error) {
	return func(context.Context, string) (command.Command, error) {
		return command.Command{Path: "/usr/bin/claude", Version: version}, nil
	}
}

func runReturning(output []byte, err error) Runner {
	return func(context.Context, string) ([]byte, error) { return output, err }
}

func fixtureOutput(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "2.1.304", "usage-report.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// mutateFixture decodes the fixture's assistant and result lines, lets the
// test change them, and re-encodes the stream.
func mutateFixture(t *testing.T, mutate func(assistant, result map[string]any)) []byte {
	t.Helper()
	lines := bytes.Split(bytes.TrimSpace(fixtureOutput(t)), []byte("\n"))
	decoded := make([]map[string]any, len(lines))
	for i, line := range lines {
		if err := json.Unmarshal(line, &decoded[i]); err != nil {
			t.Fatal(err)
		}
	}
	mutate(decoded[1], decoded[2])
	var out bytes.Buffer
	for _, line := range decoded {
		encoded, err := json.Marshal(line)
		if err != nil {
			t.Fatal(err)
		}
		out.Write(append(encoded, '\n'))
	}
	return out.Bytes()
}

func messageUsage(assistant map[string]any) map[string]any {
	return assistant["message"].(map[string]any)["usage"].(map[string]any)
}

func rateLimits(assistant map[string]any) map[string]any {
	return assistant["usage_report"].(map[string]any)["rate_limits"].(map[string]any)
}

func issueCodes(snapshot contracts.ProviderSnapshot) map[string]string {
	codes := make(map[string]string, len(snapshot.Issues))
	for _, issue := range snapshot.Issues {
		codes[issue.MeterID+"/"+issue.Field] = issue.Code
	}
	return codes
}
