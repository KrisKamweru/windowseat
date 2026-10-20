package service

import (
	"time"

	"windowseat/internal/contracts"
)

// entry is everything the service knows about one provider.
type entry struct {
	lastValid   *contracts.ProviderSnapshot
	lastFailure *contracts.ProviderSnapshot
	restored    bool
	attemptedAt time.Time
}

func (e entry) known() bool {
	return e.lastValid != nil || e.lastFailure != nil
}

func (e entry) newest() contracts.ProviderSnapshot {
	if e.lastFailure != nil {
		return *e.lastFailure
	}
	return *e.lastValid
}

// record folds a completed read into the entry. A failed read never
// discards the last valid reading; only a disabled provider does.
func record(e entry, read contracts.ProviderSnapshot, at time.Time) entry {
	switch {
	case isReading(read.State):
		return entry{lastValid: &read, attemptedAt: at}
	case read.State == contracts.ProviderDisabled:
		return entry{lastFailure: &read, attemptedAt: at}
	default:
		return entry{lastValid: e.lastValid, restored: e.restored, lastFailure: &read, attemptedAt: at}
	}
}

// restore turns a persisted provider snapshot into an entry that is served
// stale until a read confirms it.
func restore(saved contracts.ProviderSnapshot) entry {
	if len(saved.Meters) > 0 && saved.Source != nil {
		return entry{lastValid: &saved, restored: true}
	}
	return entry{lastFailure: &saved}
}

// view is the provider snapshot clients see at time now.
func view(e entry, now time.Time) contracts.ProviderSnapshot {
	if e.lastValid == nil {
		return *e.lastFailure
	}

	served := *e.lastValid
	served.Freshness.State = freshness(served, now)
	if served.Freshness.State == contracts.FreshnessCurrent && (e.restored || e.lastFailure != nil) {
		served.Freshness.State = contracts.FreshnessStale
	}
	if served.Freshness.State != contracts.FreshnessCurrent {
		served.State = contracts.ProviderStale
	}
	served.Error = nil
	if e.lastFailure != nil {
		served.Error = e.lastFailure.Error
	}
	return served
}

func freshness(reading contracts.ProviderSnapshot, now time.Time) contracts.FreshnessState {
	if reset, ok := earliestReset(reading); ok && !reset.After(now) {
		return contracts.FreshnessExpired
	}
	if now.Sub(reading.ObservedAt) > maxAge(reading) {
		return contracts.FreshnessStale
	}
	return contracts.FreshnessCurrent
}

// nextReadIn is how long to wait before the scheduled read: when the newest
// result reaches its max age or a meter of the last valid reading resets, but
// never sooner than minInterval after the previous attempt.
func nextReadIn(e entry, now time.Time, minInterval time.Duration) time.Duration {
	if !e.known() || e.attemptedAt.IsZero() {
		return 0
	}
	due := e.attemptedAt.Add(maxAge(e.newest()))
	if reset, ok := e.nextReset(); ok && reset.Before(due) {
		due = reset
	}
	if earliest := e.attemptedAt.Add(minInterval); due.Before(earliest) {
		due = earliest
	}
	return max(due.Sub(now), 0)
}

// nextReset is the earliest reset of the last valid reading that the last
// attempt has not already seen pass.
func (e entry) nextReset() (time.Time, bool) {
	if e.lastValid == nil {
		return time.Time{}, false
	}
	return earliestResetAfter(*e.lastValid, e.attemptedAt)
}

func shouldRead(e entry, now time.Time, minInterval time.Duration) bool {
	return e.attemptedAt.IsZero() || now.Sub(e.attemptedAt) >= minInterval
}

// accept stamps a reader's result with service metadata and replaces
// anything that breaks the contract with a malformed_response failure.
func accept(provider contracts.Provider, read contracts.ProviderSnapshot, at time.Time) contracts.ProviderSnapshot {
	read.Provider = provider
	read.ReceivedAt = at
	if read.ObservedAt.After(at) {
		read.ReceivedAt = read.ObservedAt
	}
	err := contracts.Snapshot{
		SchemaVersion: contracts.SchemaVersion,
		GeneratedAt:   at,
		Providers:     []contracts.ProviderSnapshot{read},
	}.Validate()
	if err == nil {
		return read
	}
	return malformed(provider, at, "invalid_snapshot", "The adapter returned a snapshot that broke the contract: "+err.Error())
}

func malformed(provider contracts.Provider, at time.Time, code, message string) contracts.ProviderSnapshot {
	return contracts.ProviderSnapshot{
		Provider:     provider,
		State:        contracts.ProviderMalformedResponse,
		ObservedAt:   at,
		ReceivedAt:   at,
		Freshness:    contracts.Freshness{State: contracts.FreshnessExpired, MaxAgeSeconds: defaultMaxAgeSeconds},
		Completeness: contracts.CompletenessPartial,
		Error:        &contracts.ProviderError{Code: code, Phase: contracts.ErrorParse, Message: message, ObservedAt: at},
	}
}

const defaultMaxAgeSeconds = 300

func maxAge(snapshot contracts.ProviderSnapshot) time.Duration {
	seconds := snapshot.Freshness.MaxAgeSeconds
	if seconds <= 0 {
		seconds = defaultMaxAgeSeconds
	}
	return time.Duration(seconds) * time.Second
}

func earliestReset(snapshot contracts.ProviderSnapshot) (time.Time, bool) {
	return earliestResetAfter(snapshot, time.Time{})
}

func earliestResetAfter(snapshot contracts.ProviderSnapshot, after time.Time) (time.Time, bool) {
	var earliest time.Time
	for _, meter := range snapshot.Meters {
		if meter.Window == nil || meter.Window.ResetsAt == nil || !meter.Window.ResetsAt.After(after) {
			continue
		}
		if earliest.IsZero() || meter.Window.ResetsAt.Before(earliest) {
			earliest = *meter.Window.ResetsAt
		}
	}
	return earliest, !earliest.IsZero()
}

func isReading(state contracts.ProviderState) bool {
	return state == contracts.ProviderHealthy || state == contracts.ProviderPartial || state == contracts.ProviderLowerBound
}
