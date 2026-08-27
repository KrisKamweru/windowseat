package contracts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSnapshotFixture(t *testing.T) {
	var snapshot Snapshot
	readFixture(t, "snapshot.json", &snapshot)

	if err := snapshot.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if got := len(snapshot.Providers); got != 3 {
		t.Fatalf("len(Providers) = %d, want 3", got)
	}

	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if !strings.Contains(string(encoded), `"has_quota":false`) {
		t.Fatal("exhausted Copilot quota was omitted during JSON encoding")
	}
}

func TestHealthFixture(t *testing.T) {
	var health HealthReport
	readFixture(t, "health.json", &health)

	if err := health.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if !health.Live || !health.Ready {
		t.Fatalf("fixture service live=%t ready=%t, want both true", health.Live, health.Ready)
	}
}

func TestAuthRequiredFixtureDoesNotInventZeroUsage(t *testing.T) {
	var snapshot Snapshot
	readFixture(t, "snapshot-auth-required.json", &snapshot)

	if err := snapshot.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	provider := snapshot.Providers[0]
	if provider.State != ProviderAuthRequired {
		t.Fatalf("State = %q, want %q", provider.State, ProviderAuthRequired)
	}
	if len(provider.Meters) != 0 {
		t.Fatalf("len(Meters) = %d, want 0", len(provider.Meters))
	}
	if provider.Error == nil || provider.Error.Code != "not_authenticated" {
		t.Fatalf("Error = %#v, want not_authenticated", provider.Error)
	}
}

func TestSnapshotRejectsNormalizedPastReset(t *testing.T) {
	snapshot := validSnapshot(t)
	past := snapshot.Providers[2].ObservedAt.Add(-time.Second)
	snapshot.Providers[2].Meters[0].Window = &TimeWindow{ResetsAt: &past}

	err := snapshot.Validate()
	if err == nil || !strings.Contains(err.Error(), "must be later than observed_at") {
		t.Fatalf("Validate() error = %v, want invalid reset error", err)
	}
}

func TestSnapshotAcceptsExhaustedQuota(t *testing.T) {
	snapshot := validSnapshot(t)
	copilot := snapshot.Providers[2]
	premium := copilot.Meters[2]

	if premium.HasQuota == nil || *premium.HasQuota {
		t.Fatal("fixture must contain exhausted Copilot quota")
	}
	if err := snapshot.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestSnapshotRejectsDuplicateProvider(t *testing.T) {
	snapshot := validSnapshot(t)
	snapshot.Providers = append(snapshot.Providers, snapshot.Providers[0])

	err := snapshot.Validate()
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("Validate() error = %v, want duplicate provider error", err)
	}
}

func TestProviderFailureRequiresSafeError(t *testing.T) {
	snapshot := validSnapshot(t)
	snapshot.Providers[0].State = ProviderAuthRequired
	snapshot.Providers[0].Error = nil

	err := snapshot.Validate()
	if err == nil || !strings.Contains(err.Error(), "required for provider failure state") {
		t.Fatalf("Validate() error = %v, want missing provider error", err)
	}
}

func TestProviderFailureRejectsMeters(t *testing.T) {
	states := []ProviderState{
		ProviderAuthRequired,
		ProviderUnsupportedVersion,
		ProviderUnavailable,
		ProviderMalformedResponse,
	}
	for _, state := range states {
		t.Run(string(state), func(t *testing.T) {
			snapshot := validSnapshot(t)
			provider := &snapshot.Providers[0]
			provider.State = state
			provider.Error = &ProviderError{
				Code:       "provider_read_failed",
				Phase:      ErrorRead,
				Message:    "The selected provider did not return a reading.",
				ObservedAt: provider.ObservedAt,
			}

			err := snapshot.Validate()
			if err == nil || !strings.Contains(err.Error(), "must not return meters") {
				t.Fatalf("Validate() error = %v, want failure meter error", err)
			}
		})
	}
}

func TestPartialReadingRequiresFieldIssue(t *testing.T) {
	snapshot := validSnapshot(t)
	snapshot.Providers[2].Issues = nil

	err := snapshot.Validate()
	if err == nil || !strings.Contains(err.Error(), "partial reading requires at least one issue") {
		t.Fatalf("Validate() error = %v, want missing issue error", err)
	}
}

func TestHealthyReadingRejectsFieldIssue(t *testing.T) {
	snapshot := validSnapshot(t)
	snapshot.Providers[1].Issues = []ObservationIssue{{
		Code:    "reset_time_unparseable",
		MeterID: "primary",
		Field:   "window.resets_at",
		Message: "The reset time could not be normalized.",
	}}

	err := snapshot.Validate()
	if err == nil || !strings.Contains(err.Error(), "healthy reading must not contain issues") {
		t.Fatalf("Validate() error = %v, want healthy issue error", err)
	}
}

func TestLowerBoundReadingCanExposeFieldIssue(t *testing.T) {
	snapshot := validSnapshot(t)
	provider := &snapshot.Providers[0]
	provider.Issues = []ObservationIssue{{
		Code:    "reset_time_unparseable",
		MeterID: "five_hour",
		Field:   "window.resets_at",
		Message: "The reset time could not be normalized.",
	}}

	if err := snapshot.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestPartialReadingRejectsDuplicateFieldIssue(t *testing.T) {
	snapshot := validSnapshot(t)
	snapshot.Providers[2].Issues = append(snapshot.Providers[2].Issues, snapshot.Providers[2].Issues[0])

	err := snapshot.Validate()
	if err == nil || !strings.Contains(err.Error(), "duplicate issue") {
		t.Fatalf("Validate() error = %v, want duplicate issue error", err)
	}
}

func TestVersionFallbackRequiresResponseProfile(t *testing.T) {
	snapshot := validSnapshot(t)
	snapshot.Providers[0].Source.ResponseProfile = ""
	snapshot.Providers[0].Source.ProfilePreferredFrom = ""
	snapshot.Providers[0].Source.VersionBeforeProfile = true

	err := snapshot.Validate()
	if err == nil || !strings.Contains(err.Error(), "response profile is required") {
		t.Fatalf("Validate() error = %v, want missing profile error", err)
	}
}

func TestCurrentReadingRequiresResponseProfile(t *testing.T) {
	snapshot := validSnapshot(t)
	snapshot.Providers[1].Source.ResponseProfile = ""
	snapshot.Providers[1].Source.ProfilePreferredFrom = ""

	err := snapshot.Validate()
	if err == nil || !strings.Contains(err.Error(), "requires a response profile") {
		t.Fatalf("Validate() error = %v, want missing profile error", err)
	}
}

func TestCurrentReadingRejectsWhitespaceProfile(t *testing.T) {
	snapshot := validSnapshot(t)
	snapshot.Providers[1].Source.ResponseProfile = " "
	snapshot.Providers[1].Source.ProfilePreferredFrom = "\t"

	err := snapshot.Validate()
	if err == nil || !strings.Contains(err.Error(), "must not be whitespace") {
		t.Fatalf("Validate() error = %v, want whitespace profile error", err)
	}
}

func TestHealthRejectsDuplicateCheck(t *testing.T) {
	var health HealthReport
	readFixture(t, "health.json", &health)
	health.Service.Checks = append(health.Service.Checks, health.Service.Checks[0])

	err := health.Validate()
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("Validate() error = %v, want duplicate check error", err)
	}
}

func TestProviderHealthCanReportCleanupDegradationForExactSnapshot(t *testing.T) {
	var health HealthReport
	readFixture(t, "health.json", &health)
	provider := &health.Providers[1]
	provider.OperationalState = ProviderOperationalDegraded
	provider.LastError = &ProviderError{
		Code:       "artifact_retained",
		Phase:      ErrorCleanup,
		Message:    "The probe artifact did not meet the cleanup rules.",
		ObservedAt: provider.CheckedAt,
	}
	provider.Checks = append(provider.Checks, HealthCheck{
		Name:      "artifact_cleanup",
		State:     CheckWarning,
		CheckedAt: provider.CheckedAt,
		Code:      "artifact_retained",
		Message:   "The probe artifact did not meet the cleanup rules.",
	})

	if provider.State != ProviderHealthy {
		t.Fatalf("State = %q, want %q", provider.State, ProviderHealthy)
	}
	if err := health.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestProviderHealthRejectsHealthyOperationForPartialSnapshot(t *testing.T) {
	var health HealthReport
	readFixture(t, "health.json", &health)
	provider := &health.Providers[2]
	provider.OperationalState = ProviderOperationalHealthy

	err := health.Validate()
	if err == nil || !strings.Contains(err.Error(), "requires a healthy provider state") {
		t.Fatalf("Validate() error = %v, want operational state error", err)
	}
}

func validSnapshot(t *testing.T) Snapshot {
	t.Helper()
	var snapshot Snapshot
	readFixture(t, "snapshot.json", &snapshot)
	return snapshot
}

func readFixture(t *testing.T, name string, target any) {
	t.Helper()
	path := filepath.Join("testdata", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatalf("Unmarshal(%q) error = %v", path, err)
	}
}
