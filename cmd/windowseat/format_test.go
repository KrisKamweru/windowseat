package main

import (
	"strings"
	"testing"
	"time"

	"windowseat/internal/contracts"
)

var now = time.Date(2026, 10, 20, 15, 39, 0, 0, time.UTC)

func ptr[T any](v T) *T { return &v }

func snapshotOf(providers ...contracts.ProviderSnapshot) contracts.Snapshot {
	return contracts.Snapshot{SchemaVersion: contracts.SchemaVersion, GeneratedAt: now, Providers: providers}
}

func claudeReading(state contracts.ProviderState, freshness contracts.FreshnessState) contracts.ProviderSnapshot {
	return contracts.ProviderSnapshot{
		Provider:  contracts.ProviderClaude,
		State:     state,
		Freshness: contracts.Freshness{State: freshness, MaxAgeSeconds: 300},
		Meters: []contracts.Meter{
			{ID: "session", Label: "Current session", UsedPercentage: ptr(31.0),
				Window: &contracts.TimeWindow{ResetsAt: ptr(now.Add(2*time.Hour + 10*time.Minute))}},
			{ID: "week", Label: "Current week", UsedPercentage: ptr(12.5),
				Window: &contracts.TimeWindow{ResetsAt: ptr(now.Add(6*24*time.Hour + 3*time.Hour))}},
		},
	}
}

func TestFormatUsage(t *testing.T) {
	tests := []struct {
		name     string
		provider contracts.ProviderSnapshot
		want     []string
	}{
		{
			name:     "healthy meters",
			provider: claudeReading(contracts.ProviderHealthy, contracts.FreshnessCurrent),
			want: []string{
				"claude  Current session  31% used    resets 17:49 (in 2h10m)",
				"claude  Current week     12.5% used  resets Oct 26 18:39 (in 6d3h)",
			},
		},
		{
			name:     "stale and expired readings are marked",
			provider: claudeReading(contracts.ProviderStale, contracts.FreshnessExpired),
			want: []string{
				"claude  Current session  31% used    resets 17:49 (in 2h10m)  [stale, expired]",
				"claude  Current week     12.5% used  resets Oct 26 18:39 (in 6d3h)  [stale, expired]",
			},
		},
		{
			name: "failure without meters",
			provider: contracts.ProviderSnapshot{
				Provider: contracts.ProviderCopilot, State: contracts.ProviderAuthRequired,
				Error: &contracts.ProviderError{Message: "not signed in", RecoveryHint: "Run copilot and sign in."},
			},
			want: []string{"copilot  [auth_required] not signed in. Run copilot and sign in."},
		},
		{
			name: "quota counts and unknown values",
			provider: contracts.ProviderSnapshot{
				Provider: contracts.ProviderCopilot, State: contracts.ProviderPartial,
				Freshness: contracts.Freshness{State: contracts.FreshnessCurrent},
				Meters: []contracts.Meter{
					{Label: "Premium interactions", Used: ptr(120.0), Entitlement: ptr(300.0)},
					{Label: "Chat", Unlimited: ptr(true)},
					{Label: "Completions"},
				},
			},
			want: []string{
				"copilot  Premium interactions  120/300 used    [partial]",
				"copilot  Chat                  unlimited       [partial]",
				"copilot  Completions           unknown         [partial]",
			},
		},
		{
			name: "long labels stay aligned and a provider denial is shown",
			provider: contracts.ProviderSnapshot{
				Provider: contracts.ProviderCodex, State: contracts.ProviderHealthy,
				Freshness:    contracts.Freshness{State: contracts.FreshnessCurrent},
				UsageAllowed: ptr(false),
				Meters: []contracts.Meter{
					{Label: "Current week (all models)", UsedPercentage: ptr(9.0)},
					{Label: "Five-hour window", UsedPercentage: ptr(100.0)},
				},
			},
			want: []string{
				"codex  Current week (all models)  9% used",
				"codex  Five-hour window           100% used",
				"codex  provider reports usage not allowed",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out strings.Builder
			formatUsage(&out, snapshotOf(tt.provider), now, time.UTC)

			got := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
			if strings.Join(got, "\n") != strings.Join(tt.want, "\n") {
				t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(tt.want, "\n"))
			}
		})
	}
}

func TestFormatUsageShowsLastErrorOfStaleReading(t *testing.T) {
	stale := claudeReading(contracts.ProviderStale, contracts.FreshnessStale)
	stale.Error = &contracts.ProviderError{Message: "usage endpoint rate limited"}
	var out strings.Builder

	formatUsage(&out, snapshotOf(stale), now, time.UTC)

	if !strings.Contains(out.String(), "claude  last read failed: usage endpoint rate limited") {
		t.Fatalf("output lacks the read error:\n%s", out.String())
	}
}

func TestFormatHealth(t *testing.T) {
	report := contracts.HealthReport{
		Ready:   true,
		Service: contracts.ComponentHealth{State: contracts.ComponentDegraded, Version: "0.1.0"},
		Providers: []contracts.ProviderHealth{
			{Provider: contracts.ProviderCodex, State: contracts.ProviderHealthy, OperationalState: contracts.ProviderOperationalHealthy, CheckedAt: now},
			{Provider: contracts.ProviderClaude, State: contracts.ProviderStale, OperationalState: contracts.ProviderOperationalDegraded, CheckedAt: now,
				LastError: &contracts.ProviderError{Message: "rate limited"}},
		},
	}
	var out strings.Builder

	formatHealth(&out, report, time.UTC)

	want := "service degraded  version 0.1.0, ready\n" +
		"codex   healthy  checked 15:39\n" +
		"claude  degraded [stale]  checked 15:39  last error: rate limited\n"
	if out.String() != want {
		t.Fatalf("got\n%s\nwant\n%s", out.String(), want)
	}
}

func TestHumanDuration(t *testing.T) {
	for d, want := range map[time.Duration]string{
		30 * time.Second:                             "<1m",
		45 * time.Minute:                             "45m",
		2*time.Hour + 10*time.Minute:                 "2h10m",
		6*24*time.Hour + 3*time.Hour:                 "6d3h",
		2*time.Hour + 9*time.Minute + 50*time.Second: "2h10m",
	} {
		if got := humanDuration(d); got != want {
			t.Errorf("humanDuration(%s) = %q, want %q", d, got, want)
		}
	}
}
