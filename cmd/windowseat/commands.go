package main

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"windowseat/internal/ipc"
	"windowseat/internal/mcpbridge"
	"windowseat/internal/service"
)

func serve(ctx context.Context, args []string) error {
	if err := newFlags("serve").Parse(args); err != nil {
		return err
	}
	addr, err := ipc.DefaultAddress()
	if err != nil {
		return err
	}
	statePath, err := service.DefaultStatePath()
	if err != nil {
		return err
	}
	readers := newReaders()
	if len(readers) == 0 {
		return service.ErrNoProviders
	}

	svc := service.New(service.Options{Readers: readers, StatePath: statePath, Version: version})
	ln, err := ipc.Listen(addr)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ran := make(chan error, 1)
	go func() { ran <- svc.Run(ctx) }()
	served := ipc.Serve(ctx, ln, svc)
	cancel()
	return errors.Join(served, <-ran)
}

func usage(ctx context.Context, args []string) error {
	flags := newFlags("usage")
	asJSON := flags.Bool("json", false, "print the contract snapshot as JSON")
	providerName := flags.String("provider", "", "only this provider: claude, codex or copilot")
	refresh := flags.Bool("refresh", false, "read the providers again now (rate limited)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	provider, err := parseProvider(*providerName)
	if err != nil {
		return err
	}

	m, err := newMonitor(ctx, provider, false)
	if err != nil {
		return err
	}
	defer m.Close()
	if *refresh {
		if err := m.Refresh(ctx, provider); err != nil {
			return err
		}
	}
	snapshot, err := m.Snapshot(ctx)
	if err != nil {
		return err
	}
	if provider != "" {
		if snapshot, err = service.Select(snapshot, provider); err != nil {
			return err
		}
	}

	if *asJSON {
		return writeJSON(os.Stdout, snapshot)
	}
	formatUsage(os.Stdout, snapshot, time.Now(), time.Local)
	return nil
}

func health(ctx context.Context, args []string) error {
	flags := newFlags("health")
	asJSON := flags.Bool("json", false, "print the contract health report as JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}

	m, err := newMonitor(ctx, "", false)
	if err != nil {
		return err
	}
	defer m.Close()
	report, err := m.Health(ctx)
	if err != nil {
		return err
	}

	if *asJSON {
		return writeJSON(os.Stdout, report)
	}
	formatHealth(os.Stdout, report, time.Local)
	return nil
}

// runMCP serves MCP on stdio. When the client closes stdin it answers the
// requests already received, then returns.
func runMCP(ctx context.Context, args []string) error {
	if err := newFlags("mcp").Parse(args); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	m, err := newMonitor(ctx, "", true)
	if err != nil {
		return err
	}
	defer m.Close()
	return mcpbridge.NewServer(m, version).Run(ctx, mcpbridge.Draining(&mcp.StdioTransport{}))
}
