//go:build !windows

package ipc

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"windowseat/internal/contracts"
	"windowseat/internal/service"
)

type fakeMonitor struct {
	snapshot  contracts.Snapshot
	refreshed []contracts.Provider
}

func (f *fakeMonitor) Snapshot(context.Context) (contracts.Snapshot, error) { return f.snapshot, nil }

func (f *fakeMonitor) Health(context.Context) (contracts.HealthReport, error) {
	return contracts.HealthReport{SchemaVersion: contracts.SchemaVersion, ObservedAt: f.snapshot.GeneratedAt, Live: true,
		Service: contracts.ComponentHealth{Component: contracts.ComponentService, State: contracts.ComponentHealthy}}, nil
}

func (f *fakeMonitor) Refresh(_ context.Context, provider contracts.Provider) error {
	if provider != "" && provider != contracts.ProviderCodex {
		return service.ErrUnknownProvider
	}
	f.refreshed = append(f.refreshed, provider)
	return nil
}

func startServer(t *testing.T, monitor Monitor) *Client {
	t.Helper()
	addr := filepath.Join(t.TempDir(), "ws.sock")
	ln, err := Listen(addr)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, ln, monitor) }()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return NewClient(addr)
}

func testSnapshot() contracts.Snapshot {
	at := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	return contracts.Snapshot{
		SchemaVersion: contracts.SchemaVersion,
		GeneratedAt:   at,
		Providers: []contracts.ProviderSnapshot{{
			Provider: contracts.ProviderCodex, State: contracts.ProviderDisabled,
			ObservedAt: at, ReceivedAt: at, Completeness: contracts.CompletenessExact,
			Freshness: contracts.Freshness{State: contracts.FreshnessCurrent, MaxAgeSeconds: 300},
		}},
	}
}

func TestRoundTrip(t *testing.T) {
	monitor := &fakeMonitor{snapshot: testSnapshot()}
	client := startServer(t, monitor)
	ctx := context.Background()

	snapshot, err := client.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := snapshot.Validate(); err != nil {
		t.Fatal(err)
	}

	health, err := client.Health(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !health.Live {
		t.Fatal("health did not round trip")
	}

	if err := client.Refresh(ctx, contracts.ProviderCodex); err != nil {
		t.Fatal(err)
	}
	if len(monitor.refreshed) != 1 || monitor.refreshed[0] != contracts.ProviderCodex {
		t.Fatalf("refreshed = %v", monitor.refreshed)
	}
}

func TestProviderSnapshotEndpoint(t *testing.T) {
	client := startServer(t, &fakeMonitor{snapshot: testSnapshot()})

	var snapshot contracts.Snapshot
	if err := client.get(context.Background(), "/v1/snapshot/codex", &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Providers) != 1 {
		t.Fatalf("providers = %d", len(snapshot.Providers))
	}

	err := client.get(context.Background(), "/v1/snapshot/claude", &snapshot)
	if err == nil {
		t.Fatal("expected an error for a provider without data")
	}
}

func TestRefreshUnknownProviderIsAnError(t *testing.T) {
	client := startServer(t, &fakeMonitor{snapshot: testSnapshot()})

	if err := client.Refresh(context.Background(), contracts.ProviderClaude); err == nil {
		t.Fatal("expected an error")
	}
}

func TestClientReportsNotRunning(t *testing.T) {
	client := NewClient(filepath.Join(t.TempDir(), "missing.sock"))

	if _, err := client.Snapshot(context.Background()); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("got %v, want ErrNotRunning", err)
	}
}

func TestSocketIsPrivate(t *testing.T) {
	addr := filepath.Join(t.TempDir(), "private", "ws.sock")
	ln, err := Listen(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	for path, want := range map[string]os.FileMode{filepath.Dir(addr): 0o700, addr: 0o600} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("%s mode = %o, want %o", path, got, want)
		}
	}
}

func TestListenReplacesStaleSocket(t *testing.T) {
	addr := filepath.Join(t.TempDir(), "ws.sock")
	first, err := Listen(addr)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Listen(addr); err == nil {
		t.Fatal("second listener must fail while the first is serving")
	}
	first.(*net.UnixListener).SetUnlinkOnClose(false)
	first.Close()

	second, err := Listen(addr)
	if err != nil {
		t.Fatal(err)
	}
	second.Close()
}
