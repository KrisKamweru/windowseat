package service

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"windowseat/internal/contracts"
	"windowseat/internal/providers"
)

func newTestService(t *testing.T, c *clock, statePath string, readers ...providers.Reader) *Service {
	t.Helper()
	return New(Options{
		Readers:         readers,
		StatePath:       statePath,
		MinReadInterval: time.Minute,
		Now:             c.Now,
		Version:         "test",
	})
}

func codexReader(c *clock) *fakeReader {
	return &fakeReader{
		provider: contracts.ProviderCodex,
		next: func() contracts.ProviderSnapshot {
			return reading(contracts.ProviderCodex, c.Now(), 5*time.Minute, c.Now().Add(2*time.Hour))
		},
	}
}

func TestSnapshotReadsProvidersThatHaveNoData(t *testing.T) {
	c := &clock{now: t0}
	codex := codexReader(c)
	svc := newTestService(t, c, "", codex)

	snapshot, err := svc.Snapshot(context.Background())

	if err != nil {
		t.Fatal(err)
	}
	if err := snapshot.Validate(); err != nil {
		t.Fatal(err)
	}
	if codex.reads.Load() != 1 || snapshot.Providers[0].State != contracts.ProviderHealthy {
		t.Fatalf("reads=%d state=%s", codex.reads.Load(), snapshot.Providers[0].State)
	}
}

func TestSnapshotWithoutReadersFails(t *testing.T) {
	svc := newTestService(t, &clock{now: t0}, "")

	if _, err := svc.Snapshot(context.Background()); !errors.Is(err, ErrNoProviders) {
		t.Fatalf("got %v, want ErrNoProviders", err)
	}
}

func TestConcurrentRefreshesShareOneRead(t *testing.T) {
	c := &clock{now: t0}
	codex := codexReader(c)
	codex.gate = make(chan struct{})
	svc := newTestService(t, c, "", codex)

	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			if err := svc.Refresh(context.Background(), contracts.ProviderCodex); err != nil {
				t.Error(err)
			}
		})
	}
	waitFor(t, func() bool { return codex.reads.Load() == 1 })
	close(codex.gate)
	wg.Wait()

	if got := codex.reads.Load(); got != 1 {
		t.Fatalf("reads = %d, want 1", got)
	}
}

func TestRefreshRespectsMinimumInterval(t *testing.T) {
	c := &clock{now: t0}
	codex := codexReader(c)
	svc := newTestService(t, c, "", codex)
	ctx := context.Background()

	_ = svc.Refresh(ctx, contracts.ProviderCodex)
	c.Advance(30 * time.Second)
	_ = svc.Refresh(ctx, contracts.ProviderCodex)
	if got := codex.reads.Load(); got != 1 {
		t.Fatalf("reads within interval = %d, want 1", got)
	}

	c.Advance(31 * time.Second)
	_ = svc.Refresh(ctx, contracts.ProviderCodex)
	if got := codex.reads.Load(); got != 2 {
		t.Fatalf("reads after interval = %d, want 2", got)
	}
}

func TestRefreshUnknownProvider(t *testing.T) {
	c := &clock{now: t0}
	svc := newTestService(t, c, "", codexReader(c))

	if err := svc.Refresh(context.Background(), contracts.ProviderClaude); !errors.Is(err, ErrUnknownProvider) {
		t.Fatalf("got %v, want ErrUnknownProvider", err)
	}
}

func TestFailedReadKeepsServingPreviousMetersAsStale(t *testing.T) {
	c := &clock{now: t0}
	failing := false
	codex := codexReader(c)
	healthy := codex.next
	codex.next = func() contracts.ProviderSnapshot {
		if failing {
			return failure(contracts.ProviderCodex, c.Now())
		}
		return healthy()
	}
	svc := newTestService(t, c, "", codex)
	ctx := context.Background()
	_ = svc.Refresh(ctx, "")

	failing = true
	c.Advance(2 * time.Minute)
	_ = svc.Refresh(ctx, "")
	snapshot, _ := svc.Snapshot(ctx)
	health, _ := svc.Health(ctx)

	served := snapshot.Providers[0]
	if served.State != contracts.ProviderStale || len(served.Meters) == 0 {
		t.Fatalf("got %s with %d meters, want stale with meters", served.State, len(served.Meters))
	}
	if err := snapshot.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := health.Validate(); err != nil {
		t.Fatal(err)
	}
	ph := health.Providers[0]
	if ph.OperationalState != contracts.ProviderOperationalDegraded || ph.LastError == nil || ph.LastSuccessfulReadAt == nil {
		t.Fatalf("provider health = %+v", ph)
	}
}

func TestSnapshotExpiresWhenResetPasses(t *testing.T) {
	c := &clock{now: t0}
	codex := &fakeReader{
		provider: contracts.ProviderCodex,
		next: func() contracts.ProviderSnapshot {
			return reading(contracts.ProviderCodex, t0, time.Hour, t0.Add(10*time.Minute))
		},
	}
	svc := newTestService(t, c, "", codex)
	ctx := context.Background()
	_ = svc.Refresh(ctx, "")

	c.Advance(11 * time.Minute)
	snapshot, _ := svc.Snapshot(ctx)

	if got := snapshot.Providers[0].Freshness.State; got != contracts.FreshnessExpired {
		t.Fatalf("freshness = %s, want expired", got)
	}
}

func TestHealthBeforeAnyReadIsValid(t *testing.T) {
	c := &clock{now: t0}
	svc := newTestService(t, c, "", codexReader(c))

	health, err := svc.Health(context.Background())

	if err != nil {
		t.Fatal(err)
	}
	if err := health.Validate(); err != nil {
		t.Fatal(err)
	}
	if health.Ready {
		t.Fatal("service must not be ready before it has any provider data")
	}
}

func TestHealthyReadingReportsHealthyService(t *testing.T) {
	c := &clock{now: t0}
	svc := newTestService(t, c, "", codexReader(c))
	ctx := context.Background()
	_ = svc.Refresh(ctx, "")

	health, _ := svc.Health(ctx)

	if err := health.Validate(); err != nil {
		t.Fatal(err)
	}
	if !health.Ready || health.Service.State != contracts.ComponentHealthy {
		t.Fatalf("ready=%v service=%s", health.Ready, health.Service.State)
	}
	if health.Providers[0].OperationalState != contracts.ProviderOperationalHealthy {
		t.Fatalf("provider = %s", health.Providers[0].OperationalState)
	}
}

func TestPersistedSnapshotIsServedStaleOnRestart(t *testing.T) {
	c := &clock{now: t0}
	path := filepath.Join(t.TempDir(), "state", "snapshot.json")
	first := newTestService(t, c, path, codexReader(c))
	_ = first.Refresh(context.Background(), "")

	blocked := codexReader(c)
	blocked.gate = make(chan struct{})
	defer close(blocked.gate)
	second := newTestService(t, c, path, blocked)
	snapshot, err := second.Snapshot(context.Background())

	if err != nil {
		t.Fatal(err)
	}
	if blocked.reads.Load() != 0 {
		t.Fatal("restored data should be served without waiting for a read")
	}
	if got := snapshot.Providers[0].State; got != contracts.ProviderStale {
		t.Fatalf("state = %s, want stale", got)
	}
}

func TestRunReadsOnStartAndAgainAtMaxAge(t *testing.T) {
	c := &clock{now: t0}
	codex := &fakeReader{
		provider: contracts.ProviderCodex,
		next: func() contracts.ProviderSnapshot {
			r := reading(contracts.ProviderCodex, c.Now(), time.Second, c.Now().Add(time.Hour))
			return r
		},
	}
	svc := New(Options{Readers: []providers.Reader{codex}, MinReadInterval: time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = svc.Run(ctx) }()

	waitFor(t, func() bool { return codex.reads.Load() >= 2 })
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met in time")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestReaderPanicBecomesMalformedResponse(t *testing.T) {
	c := &clock{now: t0}
	panicking := &fakeReader{
		provider: contracts.ProviderCodex,
		next:     func() contracts.ProviderSnapshot { panic("reader bug") },
	}
	svc := newTestService(t, c, "", panicking)

	snapshot, err := svc.Snapshot(context.Background())

	if err != nil {
		t.Fatal(err)
	}
	if got := snapshot.Providers[0].State; got != contracts.ProviderMalformedResponse {
		t.Fatalf("state = %s, want malformed_response", got)
	}
	c.Advance(time.Minute)
	if err := svc.Refresh(context.Background(), contracts.ProviderCodex); err != nil {
		t.Fatal(err)
	}
	if panicking.reads.Load() != 2 {
		t.Fatalf("reads = %d, want a second read after the panic", panicking.reads.Load())
	}
}
