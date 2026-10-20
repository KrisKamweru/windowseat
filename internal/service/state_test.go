package service

import (
	"testing"
	"time"

	"windowseat/internal/contracts"
)

func TestViewKeepsCurrentReadingWithinMaxAge(t *testing.T) {
	read := reading(contracts.ProviderCodex, t0, 5*time.Minute, t0.Add(2*time.Hour))
	e := record(entry{}, read, t0)

	served := view(e, t0.Add(4*time.Minute))

	if served.State != contracts.ProviderHealthy || served.Freshness.State != contracts.FreshnessCurrent {
		t.Fatalf("got %s/%s, want healthy/current", served.State, served.Freshness.State)
	}
}

func TestViewMarksReadingStaleAfterMaxAge(t *testing.T) {
	e := record(entry{}, reading(contracts.ProviderCodex, t0, 5*time.Minute, t0.Add(2*time.Hour)), t0)

	served := view(e, t0.Add(6*time.Minute))

	if served.State != contracts.ProviderStale || served.Freshness.State != contracts.FreshnessStale {
		t.Fatalf("got %s/%s, want stale/stale", served.State, served.Freshness.State)
	}
	if len(served.Meters) != 1 {
		t.Fatal("stale reading must keep its meters")
	}
}

func TestViewMarksReadingExpiredOnceAResetPasses(t *testing.T) {
	e := record(entry{}, reading(contracts.ProviderCodex, t0, time.Hour, t0.Add(10*time.Minute)), t0)

	served := view(e, t0.Add(10*time.Minute))

	if served.State != contracts.ProviderStale || served.Freshness.State != contracts.FreshnessExpired {
		t.Fatalf("got %s/%s, want stale/expired", served.State, served.Freshness.State)
	}
}

func TestFailureAfterSuccessServesPreviousMetersAsStale(t *testing.T) {
	good := reading(contracts.ProviderCodex, t0, 5*time.Minute, t0.Add(2*time.Hour))
	e := record(entry{}, good, t0)
	e = record(e, failure(contracts.ProviderCodex, t0.Add(time.Minute)), t0.Add(time.Minute))

	served := view(e, t0.Add(time.Minute))

	if served.State != contracts.ProviderStale || served.Freshness.State != contracts.FreshnessStale {
		t.Fatalf("got %s/%s, want stale/stale", served.State, served.Freshness.State)
	}
	if len(served.Meters) != 1 || *served.Meters[0].UsedPercentage != 31 {
		t.Fatal("previous meters must survive a failed read")
	}
	if served.Error == nil || served.Error.Code != "not_authenticated" {
		t.Fatalf("served error = %+v, want the new failure", served.Error)
	}
	if err := (contracts.Snapshot{SchemaVersion: contracts.SchemaVersion, GeneratedAt: t0, Providers: []contracts.ProviderSnapshot{served}}).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestSuccessAfterFailureClearsTheError(t *testing.T) {
	e := record(entry{}, reading(contracts.ProviderCodex, t0, 5*time.Minute, t0.Add(2*time.Hour)), t0)
	e = record(e, failure(contracts.ProviderCodex, t0.Add(time.Minute)), t0.Add(time.Minute))
	e = record(e, reading(contracts.ProviderCodex, t0.Add(2*time.Minute), 5*time.Minute, t0.Add(2*time.Hour)), t0.Add(2*time.Minute))

	served := view(e, t0.Add(2*time.Minute))

	if served.State != contracts.ProviderHealthy || served.Error != nil {
		t.Fatalf("got %s with error %+v, want healthy without error", served.State, served.Error)
	}
}

func TestFailureWithoutPreviousReadingIsServedAsIs(t *testing.T) {
	e := record(entry{}, failure(contracts.ProviderCodex, t0), t0)

	served := view(e, t0)

	if served.State != contracts.ProviderAuthRequired || len(served.Meters) != 0 {
		t.Fatalf("got %s with %d meters", served.State, len(served.Meters))
	}
}

func TestDisabledDropsPreviousReading(t *testing.T) {
	e := record(entry{}, reading(contracts.ProviderCodex, t0, 5*time.Minute, t0.Add(2*time.Hour)), t0)
	disabled := contracts.ProviderSnapshot{
		Provider: contracts.ProviderCodex, State: contracts.ProviderDisabled,
		ObservedAt: t0, ReceivedAt: t0, Completeness: contracts.CompletenessExact,
		Freshness: contracts.Freshness{State: contracts.FreshnessCurrent, MaxAgeSeconds: 300},
	}
	e = record(e, disabled, t0)

	if served := view(e, t0); served.State != contracts.ProviderDisabled {
		t.Fatalf("got %s, want disabled", served.State)
	}
}

func TestRestoredReadingIsStaleUntilConfirmed(t *testing.T) {
	e := restore(reading(contracts.ProviderCodex, t0, 5*time.Minute, t0.Add(2*time.Hour)))

	served := view(e, t0)

	if served.State != contracts.ProviderStale || served.Freshness.State != contracts.FreshnessStale {
		t.Fatalf("got %s/%s, want stale/stale", served.State, served.Freshness.State)
	}
}

func TestInvalidReadBecomesMalformedResponse(t *testing.T) {
	bad := reading(contracts.ProviderCodex, t0, 5*time.Minute, t0.Add(2*time.Hour))
	bad.Source.ResponseProfile = ""
	bad.Source.ProfilePreferredFrom = ""

	got := accept(contracts.ProviderCodex, bad, t0)

	if got.State != contracts.ProviderMalformedResponse || got.Error == nil || len(got.Meters) != 0 {
		t.Fatalf("got %s error=%+v meters=%d", got.State, got.Error, len(got.Meters))
	}
	if err := (contracts.Snapshot{SchemaVersion: contracts.SchemaVersion, GeneratedAt: t0, Providers: []contracts.ProviderSnapshot{got}}).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestAcceptStampsProviderAndReceivedAt(t *testing.T) {
	read := reading(contracts.ProviderClaude, t0, 5*time.Minute, t0.Add(2*time.Hour))

	got := accept(contracts.ProviderCodex, read, t0.Add(time.Second))

	if got.Provider != contracts.ProviderCodex || !got.ReceivedAt.Equal(t0.Add(time.Second)) {
		t.Fatalf("got provider %s received %s", got.Provider, got.ReceivedAt)
	}
}

func TestNextReadIn(t *testing.T) {
	minInterval := time.Minute
	fresh := record(entry{}, reading(contracts.ProviderCodex, t0, 5*time.Minute, t0.Add(2*time.Hour)), t0)
	resetSoon := record(entry{}, reading(contracts.ProviderCodex, t0, 5*time.Minute, t0.Add(2*time.Minute)), t0)
	resetPassed := record(entry{}, reading(contracts.ProviderCodex, t0, 5*time.Minute, t0.Add(time.Second)), t0)
	failedBeforeReset := record(
		record(entry{}, reading(contracts.ProviderCodex, t0, 5*time.Minute, t0.Add(3*time.Minute)), t0),
		failure(contracts.ProviderCodex, t0.Add(30*time.Second)), t0.Add(30*time.Second))
	failedAfterReset := record(resetPassed, failure(contracts.ProviderCodex, t0.Add(time.Minute)), t0.Add(time.Minute))

	tests := []struct {
		name string
		e    entry
		now  time.Time
		want time.Duration
	}{
		{"never read", entry{}, t0, 0},
		{"restored from disk", restore(reading(contracts.ProviderCodex, t0, 5*time.Minute, t0.Add(2*time.Hour))), t0, 0},
		{"at max age", fresh, t0.Add(time.Minute), 4 * time.Minute},
		{"overdue", fresh, t0.Add(time.Hour), 0},
		{"reset before max age", resetSoon, t0, 2 * time.Minute},
		{"never sooner than min interval", resetPassed, t0, time.Minute},
		{"failed read still waits for the last reading's reset", failedBeforeReset, t0.Add(30 * time.Second), 150 * time.Second},
		{"failed read after the reset waits for the failure's max age", failedAfterReset, t0.Add(time.Minute), 5 * time.Minute},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := nextReadIn(tt.e, tt.now, minInterval); got != tt.want {
				t.Fatalf("got %s, want %s", got, tt.want)
			}
		})
	}
}
