package main

import (
	"windowseat/internal/providers"
	"windowseat/internal/providers/claude"
	"windowseat/internal/providers/codex"
	"windowseat/internal/providers/copilot"
)

// newReaders is the single place where provider adapters are wired in.
func newReaders() []providers.Reader {
	return []providers.Reader{
		claude.NewReader(claude.Options{}),
		codex.NewReader(codex.Options{}),
		copilot.NewReader(copilot.Options{}),
	}
}
