package tray

import (
	"slices"
	"strings"
	"testing"
	"time"

	"windowseat/internal/contracts"
)

func TestMenuLines(t *testing.T) {
	claude := reading(contracts.ProviderClaude, contracts.ProviderHealthy, contracts.FreshnessCurrent,
		contracts.Meter{Label: "Current session", UsedPercentage: ptr(98.0),
			Window: &contracts.TimeWindow{ResetsAt: ptr(now.Add(70 * time.Minute))}})
	claude.Source = &contracts.Source{Name: contracts.SourceClaudeUsageReport}
	codex := reading(contracts.ProviderCodex, contracts.ProviderStale, contracts.FreshnessStale,
		contracts.Meter{Label: "Five-hour window", UsedPercentage: ptr(11.0)})
	codex.Error = &contracts.ProviderError{Message: "timed out"}
	copilot := contracts.ProviderSnapshot{Provider: contracts.ProviderCopilot, State: contracts.ProviderAuthRequired,
		Error: &contracts.ProviderError{Message: "not signed in", RecoveryHint: "Run copilot and sign in."}}

	got := MenuLines(contracts.Snapshot{Providers: []contracts.ProviderSnapshot{claude, codex, copilot}}, now, time.UTC)

	want := []string{
		"claude · read 2m ago · claude_usage_report",
		"    Current session: 98% used · resets 16:10 (in 1h10m)",
		"codex · read 2m ago · stale · last read failed: timed out",
		"    Five-hour window: 11% used",
		"copilot · auth_required: not signed in. Run copilot and sign in.",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
