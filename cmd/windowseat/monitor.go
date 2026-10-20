package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"

	"windowseat/internal/contracts"
	"windowseat/internal/ipc"
	"windowseat/internal/providers"
	"windowseat/internal/service"
)

// monitor asks the running service and falls back to reading providers in
// this process when no service is listening.
type monitor struct {
	remote *ipc.Client
	local  func() *service.Service
}

// newMonitor builds a monitor. only limits direct reads to one provider.
// When keepFresh is set, the fallback service also runs its schedule until
// ctx ends, which suits long-lived callers such as the MCP bridge.
func newMonitor(ctx context.Context, only contracts.Provider, keepFresh bool) (*monitor, error) {
	addr, err := ipc.DefaultAddress()
	if err != nil {
		return nil, err
	}
	return &monitor{
		remote: ipc.NewClient(addr),
		local: sync.OnceValue(func() *service.Service {
			fmt.Fprintln(os.Stderr, "windowseat: service not running; reading providers directly")
			svc := service.New(service.Options{Readers: readersFor(only), Version: version})
			if keepFresh {
				go func() { _ = svc.Run(ctx) }()
			}
			return svc
		}),
	}, nil
}

func readersFor(only contracts.Provider) []providers.Reader {
	var selected []providers.Reader
	for _, reader := range newReaders() {
		if only == "" || reader.Provider() == only {
			selected = append(selected, reader)
		}
	}
	return selected
}

func (m *monitor) Snapshot(ctx context.Context) (contracts.Snapshot, error) {
	return fallback(m,
		func() (contracts.Snapshot, error) { return m.remote.Snapshot(ctx) },
		func(svc *service.Service) (contracts.Snapshot, error) { return svc.Snapshot(ctx) })
}

func (m *monitor) Health(ctx context.Context) (contracts.HealthReport, error) {
	return fallback(m,
		func() (contracts.HealthReport, error) { return m.remote.Health(ctx) },
		func(svc *service.Service) (contracts.HealthReport, error) {
			_, _ = svc.Snapshot(ctx)
			return svc.Health(ctx)
		})
}

func (m *monitor) Refresh(ctx context.Context, provider contracts.Provider) error {
	_, err := fallback(m,
		func() (struct{}, error) { return struct{}{}, m.remote.Refresh(ctx, provider) },
		func(svc *service.Service) (struct{}, error) { return struct{}{}, svc.Refresh(ctx, provider) })
	return err
}

func fallback[T any](m *monitor, remote func() (T, error), local func(*service.Service) (T, error)) (T, error) {
	result, err := remote()
	if errors.Is(err, ipc.ErrNotRunning) {
		return local(m.local())
	}
	return result, err
}
