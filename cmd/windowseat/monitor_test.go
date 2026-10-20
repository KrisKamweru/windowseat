//go:build !windows

package main

import (
	"context"
	"path/filepath"
	"testing"

	"windowseat/internal/contracts"
	"windowseat/internal/ipc"
	"windowseat/internal/providers"
	"windowseat/internal/service"
)

type blankReader struct{}

func (blankReader) Provider() contracts.Provider { return contracts.ProviderCodex }

func (blankReader) Read(context.Context) contracts.ProviderSnapshot {
	return contracts.ProviderSnapshot{}
}

func newBlankService() *service.Service {
	return service.New(service.Options{Readers: []providers.Reader{blankReader{}}})
}

func TestMonitorStopsLocalReadsWhenTheServiceReturns(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	addr := filepath.Join(t.TempDir(), "ws.sock")
	local := newBlankService()
	m := &monitor{remote: ipc.NewClient(addr), newLocal: func() *service.Service {
		go func() { _ = local.Run(ctx) }()
		return local
	}}
	defer m.Close()
	if _, err := m.Snapshot(ctx); err != nil {
		t.Fatal(err)
	}

	ln, err := ipc.Listen(addr)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = ipc.Serve(ctx, ln, newBlankService()) }()
	if _, err := m.Snapshot(ctx); err != nil {
		t.Fatal(err)
	}

	if err := local.Refresh(ctx, ""); err == nil {
		t.Fatal("the fallback service is still reading providers after the service came back")
	}
}
