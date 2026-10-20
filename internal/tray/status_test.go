package tray

import (
	"testing"
	"time"

	"windowseat/internal/contracts"
)

var now = time.Date(2026, 10, 20, 15, 0, 0, 0, time.UTC)

func ptr[T any](v T) *T { return &v }

func reading(provider contracts.Provider, state contracts.ProviderState, freshness contracts.FreshnessState, meters ...contracts.Meter) contracts.ProviderSnapshot {
	return contracts.ProviderSnapshot{
		Provider: provider, State: state, ObservedAt: now.Add(-2 * time.Minute),
		Freshness: contracts.Freshness{State: freshness}, Meters: meters,
	}
}

func refusing(p contracts.ProviderSnapshot) contracts.ProviderSnapshot {
	p.UsageAllowed = ptr(false)
	return p
}

func usedPercent(label string, v float64) contracts.Meter {
	return contracts.Meter{Label: label, UsedPercentage: ptr(v)}
}

func TestSummarize(t *testing.T) {
	tests := []struct {
		name      string
		providers []contracts.ProviderSnapshot
		want      Status
	}{
		{
			name: "healthy readings below the threshold",
			providers: []contracts.ProviderSnapshot{
				reading(contracts.ProviderClaude, contracts.ProviderHealthy, contracts.FreshnessCurrent, usedPercent("Current session", 40)),
				reading(contracts.ProviderCodex, contracts.ProviderHealthy, contracts.FreshnessCurrent, usedPercent("Five-hour window", 55)),
			},
			want: Status{Level: LevelOK, Tooltip: "Windowseat\ncodex Five-hour window: 55% used\nread 2m ago"},
		},
		{
			name: "a current meter at the threshold warns",
			providers: []contracts.ProviderSnapshot{
				reading(contracts.ProviderClaude, contracts.ProviderHealthy, contracts.FreshnessCurrent, usedPercent("Current session", 98)),
			},
			want: Status{Level: LevelWarning, Tooltip: "Windowseat\nclaude Current session: 98% used\nread 2m ago"},
		},
		{
			name: "quota counts warn by their used share",
			providers: []contracts.ProviderSnapshot{
				reading(contracts.ProviderCopilot, contracts.ProviderPartial, contracts.FreshnessCurrent,
					contracts.Meter{Label: "Premium interactions", Used: ptr(1838.0), Entitlement: ptr(2000.0), RemainingPercentage: ptr(8.1)},
					contracts.Meter{Label: "Chat", Unlimited: ptr(true)}),
			},
			want: Status{Level: LevelWarning, Tooltip: "Windowseat\ncopilot Premium interactions: 1838/2000 used, 8.1% left\nread 2m ago"},
		},
		{
			name: "a high reading that has expired is stale, not a warning",
			providers: []contracts.ProviderSnapshot{
				reading(contracts.ProviderClaude, contracts.ProviderHealthy, contracts.FreshnessExpired, usedPercent("Current session", 98)),
			},
			want: Status{Level: LevelStale, Tooltip: "Windowseat\nclaude Current session: 98% used\nread 2m ago\nsome readings are out of date"},
		},
		{
			name: "a failed provider is an error even when others are fine",
			providers: []contracts.ProviderSnapshot{
				reading(contracts.ProviderClaude, contracts.ProviderHealthy, contracts.FreshnessCurrent, usedPercent("Current session", 20)),
				{Provider: contracts.ProviderCodex, State: contracts.ProviderAuthRequired, Error: &contracts.ProviderError{Message: "not signed in"}},
			},
			want: Status{Level: LevelError, Tooltip: "Windowseat\nclaude Current session: 20% used\nread 2m ago\ncodex: not signed in"},
		},
		{
			name: "a provider that refuses usage warns below the threshold",
			providers: []contracts.ProviderSnapshot{
				refusing(reading(contracts.ProviderCodex, contracts.ProviderHealthy, contracts.FreshnessCurrent, usedPercent("Five-hour window", 10))),
			},
			want: Status{Level: LevelWarning, Tooltip: "Windowseat\ncodex Five-hour window: 10% used\nread 2m ago\ncodex: provider reports usage not allowed"},
		},
		{
			name: "an expired refusal is stale, not a warning",
			providers: []contracts.ProviderSnapshot{
				refusing(reading(contracts.ProviderCodex, contracts.ProviderHealthy, contracts.FreshnessExpired, usedPercent("Five-hour window", 10))),
			},
			want: Status{Level: LevelStale, Tooltip: "Windowseat\ncodex Five-hour window: 10% used\nread 2m ago\ncodex: provider reports usage not allowed\nsome readings are out of date"},
		},
		{
			name:      "no readings yet",
			providers: nil,
			want:      Status{Level: LevelOK, Tooltip: "Windowseat\nno readings yet"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := contracts.Snapshot{GeneratedAt: now, Providers: tt.providers}

			got := Summarize(snapshot, 80, now)

			if got != tt.want {
				t.Fatalf("got  %#v\nwant %#v", got, tt.want)
			}
		})
	}
}

func TestUnreachable(t *testing.T) {
	got := Unreachable()

	if got.Level != LevelError || got.Tooltip != "Windowseat\nreconnecting to the service" {
		t.Fatalf("got %#v", got)
	}
}
