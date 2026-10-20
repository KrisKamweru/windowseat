// Package claude reads Claude Code subscription limits from the headless
// `claude -p /usage` report, which runs locally and spends no quota.
package claude

import (
	"bytes"
	"context"
	"errors"
	"time"

	"windowseat/internal/contracts"
	"windowseat/internal/providers"
	"windowseat/internal/providers/command"
)

const (
	commandName     = "claude"
	defaultTimeout  = 60 * time.Second
	maxAgeSeconds   = 300
	authRecoveryTip = "Sign in with the selected native claude command under the service user."
)

// Runner executes the usage command and returns its standard output.
type Runner func(ctx context.Context, commandPath string) ([]byte, error)

// Options override how the reader finds and runs claude. Zero values select
// the real command on PATH, the real clock, and a 60-second timeout.
type Options struct {
	Discover func(ctx context.Context, name string) (command.Command, error)
	Run      Runner
	Now      func() time.Time
	Timeout  time.Duration
}

type reader struct {
	discover func(ctx context.Context, name string) (command.Command, error)
	run      Runner
	now      func() time.Time
	timeout  time.Duration
}

func NewReader(options Options) providers.Reader {
	r := reader{
		discover: command.Discover,
		run:      runUsageCommand,
		now:      time.Now,
		timeout:  defaultTimeout,
	}
	if options.Discover != nil {
		r.discover = options.Discover
	}
	if options.Run != nil {
		r.run = options.Run
	}
	if options.Now != nil {
		r.now = options.Now
	}
	if options.Timeout > 0 {
		r.timeout = options.Timeout
	}
	return r
}

func (r reader) Provider() contracts.Provider {
	return contracts.ProviderClaude
}

func (r reader) Read(ctx context.Context) contracts.ProviderSnapshot {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	cmd, err := r.discover(ctx, commandName)
	if err != nil {
		return r.failed(nil, discoveryFailure(err))
	}
	source := &contracts.Source{
		Name:           contracts.SourceClaudeUsageReport,
		CommandPath:    cmd.Path,
		CommandVersion: cmd.Version,
	}
	profile, err := profiles.Select(cmd.Version)
	if err != nil {
		return r.failed(source, unsupportedVersion)
	}
	source.ResponseProfile = profile.ID
	source.ProfilePreferredFrom = profile.PreferredFrom
	source.VersionBeforeProfile = profile.BeforePreferred

	output, runErr := r.run(ctx, cmd.Path)
	observedAt := r.now()
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return r.failed(source, commandTimedOut)
	case runErr != nil && len(bytes.TrimSpace(output)) == 0:
		return r.failed(source, commandFailed)
	}

	reading, fail := profile.Value(output, observedAt)
	if fail != nil {
		return r.failed(source, *fail)
	}
	return reading.snapshot(source, observedAt, r.now())
}

func (r reading) snapshot(source *contracts.Source, observedAt, receivedAt time.Time) contracts.ProviderSnapshot {
	snapshot := contracts.ProviderSnapshot{
		Provider:     contracts.ProviderClaude,
		State:        contracts.ProviderHealthy,
		ObservedAt:   observedAt,
		ReceivedAt:   receivedAt,
		Source:       source,
		Freshness:    contracts.Freshness{State: contracts.FreshnessCurrent, MaxAgeSeconds: maxAgeSeconds},
		Completeness: contracts.CompletenessExact,
		Meters:       r.meters,
		Issues:       r.issues,
	}
	if len(r.issues) > 0 {
		snapshot.State = contracts.ProviderPartial
		snapshot.Completeness = contracts.CompletenessPartial
	}
	return snapshot
}

func (r reader) failed(source *contracts.Source, fail failure) contracts.ProviderSnapshot {
	at := r.now()
	return contracts.ProviderSnapshot{
		Provider:     contracts.ProviderClaude,
		State:        fail.state,
		ObservedAt:   at,
		ReceivedAt:   at,
		Source:       source,
		Freshness:    contracts.Freshness{State: contracts.FreshnessExpired, MaxAgeSeconds: maxAgeSeconds},
		Completeness: contracts.CompletenessPartial,
		Error: &contracts.ProviderError{
			Code:         fail.code,
			Phase:        fail.phase,
			Message:      fail.message,
			RecoveryHint: fail.hint,
			ObservedAt:   at,
		},
	}
}
