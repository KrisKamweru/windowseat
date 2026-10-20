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
	// lifetime ends reads when the service stops. Reads derive from it rather
	// than from a caller, so one caller giving up cannot cancel a shared read.
	lifetime    context.Context
	stop        context.CancelFunc
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

	lifetime, stop := context.WithCancel(context.Background())
	s := &Service{
		lifetime:    lifetime,
		stop:        stop,
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
// or a meter resets. When ctx is cancelled it closes the service and returns
// once in-flight reads have stopped.
func (s *Service) Run(ctx context.Context) error {
	if len(s.slots) == 0 {
		return ErrNoProviders
	}
	var wg sync.WaitGroup
	for _, sl := range s.slots {
		wg.Go(func() { s.schedule(ctx, sl) })
	}
	wg.Wait()
	s.Close()
	return nil
}

// Close cancels in-flight reads, waits for them to finish, and refuses new ones.
func (s *Service) Close() {
	s.stop()
	for _, sl := range s.slots {
		sl.mu.Lock()
		done := sl.inflight
		sl.mu.Unlock()
		if done != nil {
			<-done
		}
	}
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
		if err := s.lifetime.Err(); err != nil {
			sl.mu.Unlock()
			return err
		}
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

// read runs detached from any caller. A read cut short because the service
// is stopping says nothing about the provider, so it is not recorded.
func (s *Service) read(sl *slot, done chan struct{}) {
	ctx, cancel := context.WithTimeout(s.lifetime, defaultReadTimeout)
	result := s.readProvider(ctx, sl.reader)
	cancel()

	if s.lifetime.Err() == nil {
		at := s.now()
		sl.mu.Lock()
		sl.entry = record(sl.entry, result, at)
		sl.mu.Unlock()
		s.persist()
	}

	sl.mu.Lock()
	sl.inflight = nil
	sl.mu.Unlock()
	close(done)
}

// readProvider turns a reader panic into a malformed_response failure so one
// broken adapter cannot take the service down.
func (s *Service) readProvider(ctx context.Context, reader providers.Reader) (result contracts.ProviderSnapshot) {
	defer func() {
		if recover() != nil {
			result = malformed(reader.Provider(), s.now(), "reader_panicked", "The adapter failed unexpectedly while reading.")
		}
	}()
	return accept(reader.Provider(), reader.Read(ctx), s.now())
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
