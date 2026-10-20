package providers

import (
	"context"

	"windowseat/internal/contracts"
)

// Reader collects one provider's current limits through its official CLI.
// Read never returns a Go error: failures are reported in the snapshot's
// State and Error so that a failed read can never look like zero usage.
type Reader interface {
	Provider() contracts.Provider
	Read(ctx context.Context) contracts.ProviderSnapshot
}
