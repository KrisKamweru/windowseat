package service

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"windowseat/internal/contracts"
)

var t0 = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

func reading(provider contracts.Provider, observedAt time.Time, maxAge time.Duration, resetsAt time.Time) contracts.ProviderSnapshot {
	used := 31.0
	return contracts.ProviderSnapshot{
		Provider:     provider,
		State:        contracts.ProviderHealthy,
		ObservedAt:   observedAt,
		ReceivedAt:   observedAt,
		Source:       &contracts.Source{Name: contracts.SourceCodexAppServer, CommandPath: "/bin/codex", CommandVersion: "1.0.0", ResponseProfile: "p1", ProfilePreferredFrom: "1.0.0"},
		Freshness:    contracts.Freshness{State: contracts.FreshnessCurrent, MaxAgeSeconds: int64(maxAge / time.Second)},
		Completeness: contracts.CompletenessExact,
		Meters: []contracts.Meter{{
			ID: "five_hour", Label: "Current session", Kind: contracts.MeterRollingWindow,
			UsedPercentage: &used,
			Window:         &contracts.TimeWindow{ResetsAt: &resetsAt},
		}},
	}
}

func failure(provider contracts.Provider, observedAt time.Time) contracts.ProviderSnapshot {
	return contracts.ProviderSnapshot{
		Provider:     provider,
		State:        contracts.ProviderAuthRequired,
		ObservedAt:   observedAt,
		ReceivedAt:   observedAt,
		Freshness:    contracts.Freshness{State: contracts.FreshnessExpired, MaxAgeSeconds: 300},
		Completeness: contracts.CompletenessPartial,
		Error: &contracts.ProviderError{
			Code: "not_authenticated", Phase: contracts.ErrorAuthenticate,
			Message: "not signed in", ObservedAt: observedAt,
		},
	}
}

type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// fakeReader returns whatever next produces and counts the reads.
// When gate is set, each read blocks until the gate is closed.
type fakeReader struct {
	provider contracts.Provider
	next     func() contracts.ProviderSnapshot
	gate     chan struct{}
	reads    atomic.Int32
}

func (f *fakeReader) Provider() contracts.Provider { return f.provider }

func (f *fakeReader) Read(context.Context) contracts.ProviderSnapshot {
	f.reads.Add(1)
	if f.gate != nil {
		<-f.gate
	}
	return f.next()
}

// stubbornReader blocks until its context ends, like a provider CLI that hangs.
type stubbornReader struct {
	started   chan struct{}
	cancelled atomic.Bool
}

func (r *stubbornReader) Provider() contracts.Provider { return contracts.ProviderCodex }

func (r *stubbornReader) Read(ctx context.Context) contracts.ProviderSnapshot {
	close(r.started)
	<-ctx.Done()
	r.cancelled.Store(true)
	return failure(contracts.ProviderCodex, t0)
}
