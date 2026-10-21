// Command windowseat reports how much AI coding provider quota remains.
package main

//go:generate go run github.com/tc-hib/go-winres@v0.3.3 make --in winres.json --arch amd64,arm64 --out rsrc

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"windowseat/internal/contracts"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "0.1.0-dev"

const usageText = `usage: windowseat <command> [flags]

commands:
  serve      run the monitor service
  usage      print provider usage [--json] [--provider p] [--refresh]
  health     print service and provider health [--json]
  mcp        run the read-only MCP server on stdio
  install    start the service and tray at login [--no-tray] [--add-to-path] [--dry-run]
  uninstall  remove what install added [--dry-run]
  help       print this message
`

var commands = map[string]func(ctx context.Context, args []string) error{
	"serve":     serve,
	"usage":     usage,
	"health":    health,
	"mcp":       runMCP,
	"install":   runInstall,
	"uninstall": runUninstall,
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usageText)
		return 2
	}
	switch args[0] {
	case "help", "-h", "--help":
		fmt.Print(usageText)
		return 0
	}
	command, ok := commands[args[0]]
	if !ok {
		fmt.Fprintf(os.Stderr, "windowseat: unknown command %q\n\n%s", args[0], usageText)
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := command(ctx, args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintln(os.Stderr, "windowseat:", err)
		return 1
	}
	return 0
}

func newFlags(name string) *flag.FlagSet {
	flags := flag.NewFlagSet("windowseat "+name, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	return flags
}

func parseProvider(name string) (contracts.Provider, error) {
	switch provider := contracts.Provider(name); provider {
	case "", contracts.ProviderClaude, contracts.ProviderCodex, contracts.ProviderCopilot:
		return provider, nil
	default:
		return "", fmt.Errorf("unknown provider %q: want claude, codex or copilot", name)
	}
}

func writeJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
