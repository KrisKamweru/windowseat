// Package copilot reads GitHub Copilot quota from the copilot CLI the user's
// PATH selects. Each read starts a fresh runtime, asks for auth status and
// quota, and stops it. It never creates a session or sends a prompt.
package copilot

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"windowseat/internal/contracts"
	"windowseat/internal/providers"
	"windowseat/internal/providers/command"
)

const defaultTimeout = 30 * time.Second

type Options struct {
	// Timeout bounds one complete read. Zero means 30 seconds.
	Timeout time.Duration
}

type reader struct {
	timeout time.Duration
}

func NewReader(options Options) providers.Reader {
	if options.Timeout <= 0 {
		options.Timeout = defaultTimeout
	}
	return reader{timeout: options.Timeout}
}

func (reader) Provider() contracts.Provider {
	return contracts.ProviderCopilot
}

func (r reader) Read(ctx context.Context) contracts.ProviderSnapshot {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	result, failed := collect(ctx)
	result.ReceivedAt = time.Now()
	if failed != nil {
		return failedSnapshot(*failed, result)
	}
	return readingSnapshot(result)
}

// collect runs one runtime session. The deferred stop runs before the
// caller stamps ReceivedAt, so cleanup counts towards the read.
func collect(ctx context.Context) (reading, *failure) {
	result := reading{ObservedAt: time.Now()}

	selected, err := command.Discover(ctx, "copilot")
	if errors.Is(err, command.ErrNotFound) {
		return result, &commandMissing
	}
	if err != nil {
		return result, &versionUnreadable
	}
	result.Command = selected
	result.PackageVersion = npmPackageVersion(selected.Path)

	session, err := startRuntime(ctx, selected.Path)
	if err != nil {
		return result, &startFailed
	}
	defer session.stop()

	if result.Runtime, err = session.connect(); err != nil {
		return result, &startFailed
	}
	if result.Auth, err = session.authStatus(); err != nil {
		return result, &authStatusFailed
	}
	if !result.Auth.IsAuthenticated {
		result.ObservedAt = time.Now()
		return result, nil
	}
	if result.Quota, err = session.quota(); err != nil {
		return result, &quotaReadFailed
	}
	result.ObservedAt = time.Now()
	return result, nil
}

// npmPackageVersion reads the version of the @github/copilot npm package
// when the selected command is its loader. Other installs report none.
func npmPackageVersion(commandPath string) string {
	loader, err := filepath.EvalSymlinks(commandPath)
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(loader), "package.json"))
	if err != nil {
		return ""
	}
	var manifest struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if json.Unmarshal(data, &manifest) != nil || manifest.Name != "@github/copilot" {
		return ""
	}
	return manifest.Version
}
