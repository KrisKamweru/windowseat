package mcpbridge

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"windowseat/internal/contracts"
	"windowseat/internal/service"
)

var at = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

type fakeMonitor struct {
	refreshed []contracts.Provider
}

func (f *fakeMonitor) Snapshot(context.Context) (contracts.Snapshot, error) {
	disabled := func(p contracts.Provider) contracts.ProviderSnapshot {
		return contracts.ProviderSnapshot{
			Provider: p, State: contracts.ProviderDisabled,
			ObservedAt: at, ReceivedAt: at, Completeness: contracts.CompletenessExact,
			Freshness: contracts.Freshness{State: contracts.FreshnessCurrent, MaxAgeSeconds: 300},
		}
	}
	return contracts.Snapshot{
		SchemaVersion: contracts.SchemaVersion,
		GeneratedAt:   at,
		Providers:     []contracts.ProviderSnapshot{disabled(contracts.ProviderClaude), disabled(contracts.ProviderCodex)},
	}, nil
}

func (f *fakeMonitor) Health(context.Context) (contracts.HealthReport, error) {
	return contracts.HealthReport{SchemaVersion: contracts.SchemaVersion, ObservedAt: at, Live: true,
		Service: contracts.ComponentHealth{Component: contracts.ComponentService, State: contracts.ComponentHealthy}}, nil
}

func (f *fakeMonitor) Refresh(_ context.Context, provider contracts.Provider) error {
	f.refreshed = append(f.refreshed, provider)
	return nil
}

func connect(t *testing.T, monitor Monitor) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	server := NewServer(monitor, "test")
	if _, err := server.Connect(ctx, serverTransport, nil); err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}

func TestListsExactlyTheReadOnlyTools(t *testing.T) {
	session := connect(t, &fakeMonitor{})

	result, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}

	var names []string
	for _, tool := range result.Tools {
		names = append(names, tool.Name)
		for _, phrase := range []string{"advisory", "read-only", "freshness", "not zero"} {
			if !strings.Contains(strings.ToLower(tool.Description), phrase) {
				t.Errorf("%s description lacks %q", tool.Name, phrase)
			}
		}
	}
	slices.Sort(names)
	if want := []string{"get_health", "get_provider_usage", "get_usage"}; !slices.Equal(names, want) {
		t.Fatalf("tools = %v, want %v", names, want)
	}
}

func TestGetUsageReturnsTheSnapshot(t *testing.T) {
	session := connect(t, &fakeMonitor{})

	snapshot := callTool[contracts.Snapshot](t, session, "get_usage", map[string]any{})

	if err := snapshot.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Providers) != 2 {
		t.Fatalf("providers = %d, want 2", len(snapshot.Providers))
	}
}

func TestGetProviderUsageRefreshesAndFilters(t *testing.T) {
	monitor := &fakeMonitor{}
	session := connect(t, monitor)

	snapshot := callTool[contracts.Snapshot](t, session, "get_provider_usage", map[string]any{"provider": "codex", "refresh": true})

	if len(snapshot.Providers) != 1 || snapshot.Providers[0].Provider != contracts.ProviderCodex {
		t.Fatalf("providers = %+v", snapshot.Providers)
	}
	if !slices.Equal(monitor.refreshed, []contracts.Provider{contracts.ProviderCodex}) {
		t.Fatalf("refreshed = %v", monitor.refreshed)
	}
}

func TestGetProviderUsageRejectsUnknownProvider(t *testing.T) {
	session := connect(t, &fakeMonitor{})

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "get_provider_usage", Arguments: map[string]any{"provider": "gemini"},
	})

	if err == nil && !result.IsError {
		t.Fatal("expected an unknown provider to be rejected")
	}
}

func TestGetProviderUsageForProviderWithoutData(t *testing.T) {
	session := connect(t, &fakeMonitor{})

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "get_provider_usage", Arguments: map[string]any{"provider": "copilot"},
	})

	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError || !strings.Contains(textOf(result), service.ErrUnknownProvider.Error()) {
		t.Fatalf("result = %+v", result)
	}
}

func TestGetHealth(t *testing.T) {
	session := connect(t, &fakeMonitor{})

	report := callTool[contracts.HealthReport](t, session, "get_health", map[string]any{})

	if err := report.Validate(); err != nil {
		t.Fatal(err)
	}
}

func callTool[T any](t *testing.T, session *mcp.ClientSession, name string, args map[string]any) T {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("%s failed: %s", name, textOf(result))
	}
	var out T
	if err := json.Unmarshal([]byte(textOf(result)), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func textOf(result *mcp.CallToolResult) string {
	var b strings.Builder
	for _, content := range result.Content {
		if text, ok := content.(*mcp.TextContent); ok {
			b.WriteString(text.Text)
		}
	}
	return b.String()
}
