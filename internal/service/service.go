// Package service owns the latest usage snapshot. It reads providers on a
// schedule and on demand, never more than one read per provider at a time.
package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"windowseat/internal/contracts"
	"windowseat/internal/providers"
)

var (
	ErrNoProviders     = errors.New("no providers configured")
	ErrUnknownProvider = errors.New("provider not configured")
)

const (
	DefaultMinReadInterval = 60 * time.Second
	defaultReadTimeout     = 2 * time.Minute
)

type Options struct {
	Readers []providers.Reader
	// StatePath is where the latest snapshot survives restarts. Empty disables persistence.
	StatePath string
	// MinReadInterval stops callers from hammering a provider. Zero means DefaultMinReadInterval.
	MinReadInterval time.Duration
	Now             func() time.Time
	Version         string
}

type Service struct {
	slots       []*slot
	store       *store
	minInterval time.Duration
	now         func() time.Time
	version     string
	startedAt   time.Time
	persistMu   sync.Mutex
}

// slot serialises reads for one provider.
type slot struct {
	reader   providers.Reader
	mu       sync.Mutex
	entry    entry
	inflight chan struct{}
}

func (s *slot) current() entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.entry
}

func New(opts Options) *Service {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	minInterval := opts.MinReadInterval
	if minInterval <= 0 {
		minInterval = DefaultMinReadInterval
	}

	s := &Service{
		store:       newStore(opts.StatePath),
		minInterval: minInterval,
		now:         now,
		version:     opts.Version,
		startedAt:   now(),
	}
	saved := s.store.load()
	for _, reader := range opts.Readers {
		sl := &slot{reader: reader}
		if snapshot, ok := saved[reader.Provider()]; ok {
			sl.entry = restore(snapshot)
		}
		s.slots = append(s.slots, sl)
	}
	return s
}

// Run reads every provider now and again whenever its data reaches max age
// or a meter resets. It returns when ctx is cancelled.
func (s *Service) Run(ctx context.Context) error {
	if len(s.slots) == 0 {
		return ErrNoProviders
	}
	var wg sync.WaitGroup
	for _, sl := range s.slots {
		wg.Go(func() { s.schedule(ctx, sl) })
	}
	wg.Wait()
	return nil
}

func (s *Service) schedule(ctx context.Context, sl *slot) {
	for {
		timer := time.NewTimer(nextReadIn(sl.current(), s.now(), s.minInterval))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			_ = s.refresh(ctx, sl)
		}
	}
}

// Refresh reads one provider, or every provider when provider is empty.
// Reads within the minimum interval are skipped and concurrent callers
// share the read already in flight.
func (s *Service) Refresh(ctx context.Context, provider contracts.Provider) error {
	targets, err := s.slotsFor(provider)
	if err != nil {
		return err
	}
	return s.refreshAll(ctx, targets)
}

func (s *Service) slotsFor(provider contracts.Provider) ([]*slot, error) {
	if provider == "" {
		return s.slots, nil
	}
	for _, sl := range s.slots {
		if sl.reader.Provider() == provider {
			return []*slot{sl}, nil
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrUnknownProvider, provider)
}

func (s *Service) refreshAll(ctx context.Context, slots []*slot) error {
	errs := make([]error, len(slots))
	var wg sync.WaitGroup
	for i, sl := range slots {
		wg.Go(func() { errs[i] = s.refresh(ctx, sl) })
	}
	wg.Wait()
	return errors.Join(errs...)
}

func (s *Service) refresh(ctx context.Context, sl *slot) error {
	sl.mu.Lock()
	done := sl.inflight
	if done == nil {
		if !shouldRead(sl.entry, s.now(), s.minInterval) {
			sl.mu.Unlock()
			return nil
		}
		done = make(chan struct{})
		sl.inflight = done
		go s.read(sl, done)
	}
	sl.mu.Unlock()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// read runs detached from any caller so that one impatient client cannot
// cancel a read that others are waiting for.
func (s *Service) read(sl *slot, done chan struct{}) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultReadTimeout)
	result := sl.reader.Read(ctx)
	cancel()

	at := s.now()
	sl.mu.Lock()
	sl.entry = record(sl.entry, accept(sl.reader.Provider(), result, at), at)
	sl.inflight = nil
	sl.mu.Unlock()

	s.persist()
	close(done)
}

// Snapshot returns the latest data for every provider. Providers that have
// never been read are read first.
func (s *Service) Snapshot(ctx context.Context) (contracts.Snapshot, error) {
	if len(s.slots) == 0 {
		return contracts.Snapshot{}, ErrNoProviders
	}
	if err := s.refreshAll(ctx, s.unknown()); err != nil {
		return contracts.Snapshot{}, err
	}
	return s.build(), nil
}

func (s *Service) unknown() []*slot {
	var unknown []*slot
	for _, sl := range s.slots {
		if !sl.current().known() {
			unknown = append(unknown, sl)
		}
	}
	return unknown
}

func (s *Service) build() contracts.Snapshot {
	now := s.now()
	snapshot := contracts.Snapshot{SchemaVersion: contracts.SchemaVersion, GeneratedAt: now}
	for _, sl := range s.slots {
		if e := sl.current(); e.known() {
			snapshot.Providers = append(snapshot.Providers, view(e, now))
		}
	}
	return snapshot
}

func (s *Service) persist() {
	s.persistMu.Lock()
	defer s.persistMu.Unlock()
	s.store.save(s.build())
}

// Select narrows a snapshot to one provider.
func Select(snapshot contracts.Snapshot, provider contracts.Provider) (contracts.Snapshot, error) {
	for _, p := range snapshot.Providers {
		if p.Provider == provider {
			snapshot.Providers = []contracts.ProviderSnapshot{p}
			return snapshot, nil
		}
	}
	return contracts.Snapshot{}, fmt.Errorf("%w: %s", ErrUnknownProvider, provider)
}
