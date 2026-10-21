// Package mcpbridge exposes the usage snapshot to agents as read-only MCP tools.
package mcpbridge

import (
	"context"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"windowseat/internal/contracts"
	"windowseat/internal/service"
)

type Monitor interface {
	Snapshot(ctx context.Context) (contracts.Snapshot, error)
	Health(ctx context.Context) (contracts.HealthReport, error)
	Refresh(ctx context.Context, provider contracts.Provider) error
}

const advisory = " The data is advisory and read-only: it reports provider limits and never decides whether to work. " +
	"Check each provider's state and freshness before relying on it, and treat a missing meter as unknown, not zero."

var (
	getUsage = &mcp.Tool{
		Name:        "get_usage",
		Description: "Return the latest usage snapshot for every configured AI coding provider." + advisory,
	}
	getProviderUsage = &mcp.Tool{
		Name:        "get_provider_usage",
		Description: "Return the latest usage snapshot for one provider: claude, codex, or copilot." + advisory,
		InputSchema: providerInputSchema(),
	}
	getHealth = &mcp.Tool{
		Name:        "get_health",
		Description: "Return WindowSeat service and provider adapter health." + advisory,
	}
)

type usageInput struct {
	Refresh bool `json:"refresh,omitempty" jsonschema:"read the providers again now instead of returning the cached snapshot; rate limited"`
}

type providerUsageInput struct {
	Provider contracts.Provider `json:"provider"`
	Refresh  bool               `json:"refresh,omitempty" jsonschema:"read the provider again now instead of returning the cached snapshot; rate limited"`
}

func providerInputSchema() *jsonschema.Schema {
	schema, err := jsonschema.For[providerUsageInput](nil)
	if err != nil {
		panic(err)
	}
	provider := schema.Properties["provider"]
	provider.Description = "provider to report"
	provider.Enum = []any{contracts.ProviderClaude, contracts.ProviderCodex, contracts.ProviderCopilot}
	return schema
}

// NewServer builds the MCP server. Run it with mcp.StdioTransport.
func NewServer(monitor Monitor, version string) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "windowseat", Version: version}, nil)

	mcp.AddTool(server, getUsage, func(ctx context.Context, _ *mcp.CallToolRequest, in usageInput) (*mcp.CallToolResult, any, error) {
		snapshot, err := usage(ctx, monitor, "", in.Refresh)
		return nil, snapshot, err
	})
	mcp.AddTool(server, getProviderUsage, func(ctx context.Context, _ *mcp.CallToolRequest, in providerUsageInput) (*mcp.CallToolResult, any, error) {
		snapshot, err := usage(ctx, monitor, in.Provider, in.Refresh)
		return nil, snapshot, err
	})
	mcp.AddTool(server, getHealth, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		report, err := monitor.Health(ctx)
		return nil, report, err
	})
	return server
}

func usage(ctx context.Context, monitor Monitor, provider contracts.Provider, refresh bool) (contracts.Snapshot, error) {
	if refresh {
		if err := monitor.Refresh(ctx, provider); err != nil {
			return contracts.Snapshot{}, err
		}
	}
	snapshot, err := monitor.Snapshot(ctx)
	if err != nil || provider == "" {
		return snapshot, err
	}
	return service.Select(snapshot, provider)
}
