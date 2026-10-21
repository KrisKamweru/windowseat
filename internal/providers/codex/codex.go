// Package codex reads OpenAI Codex rate limits through `codex app-server`.
package codex

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"time"

	"windowseat/internal/contracts"
	"windowseat/internal/providers"
	"windowseat/internal/providers/command"
	"windowseat/internal/providers/versionprofile"
)

const (
	defaultTimeout         = 30 * time.Second
	freshnessMaxAgeSeconds = 300
	commandName            = "codex"
	installHint            = "Install the Codex CLI and make sure `codex` is on the service user's PATH."
	loginHint              = "Sign in with `codex login` under the service user."
	retryHint              = "Check that `codex app-server` runs for the service user, then retry."
	upgradeOrReportHint    = "Upgrade the Codex CLI or report the response shape to WindowSeat."
)

type parser func(result json.RawMessage, observedAt time.Time) (reading, error)

var profiles = versionprofile.MustNew([]versionprofile.Definition[parser]{
	{ID: "codex_rate_limits_0_149_1", PreferredFrom: "0.149.1", Value: parseRateLimits},
})

var authMessage = regexp.MustCompile(`(?i)\b(auth\w*|login|log in|logged in|sign in|signed in|unauthori[sz]ed)\b`)

type Options struct {
	// Timeout bounds discovery plus the app-server exchange. Zero means 30 seconds.
	Timeout time.Duration
}

type reader struct {
	discover       func(ctx context.Context, name string) (command.Command, error)
	readRateLimits func(ctx context.Context, path string) (json.RawMessage, error)
	now            func() time.Time
	timeout        time.Duration
}

func NewReader(options Options) providers.Reader {
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &reader{
		discover:       command.Discover,
		readRateLimits: runAppServer,
		now:            time.Now,
		timeout:        timeout,
	}
}

func (r *reader) Provider() contracts.Provider { return contracts.ProviderCodex }

func (r *reader) Read(ctx context.Context) contracts.ProviderSnapshot {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	installed, err := r.discover(ctx, commandName)
	if err != nil {
		return r.failed(nil, failure{contracts.ProviderUnavailable, contracts.ErrorDiscover, "command_not_found", "The Codex CLI could not be found or did not report a version.", installHint})
	}
	source := &contracts.Source{
		Name:           contracts.SourceCodexAppServer,
		CommandPath:    installed.Path,
		CommandVersion: installed.Version,
	}
	profile, err := profiles.Select(installed.Version)
	if err != nil {
		return r.failed(source, failure{contracts.ProviderUnsupportedVersion, contracts.ErrorDiscover, "unsupported_version", "The installed Codex version could not be matched to a response profile.", upgradeOrReportHint})
	}

	result, err := r.readRateLimits(ctx, installed.Path)
	if err != nil {
		return r.failed(source, classifyReadError(err))
	}
	observedAt := r.now()
	parsed, err := profile.Value(result, observedAt)
	if err != nil {
		return r.failed(source, failure{contracts.ProviderMalformedResponse, contracts.ErrorParse, "malformed_response", "The Codex rate-limit response had no readable limits.", upgradeOrReportHint})
	}

	source.ResponseProfile = profile.ID
	source.ProfilePreferredFrom = profile.PreferredFrom
	source.VersionBeforeProfile = profile.BeforePreferred
	state, completeness := contracts.ProviderHealthy, contracts.CompletenessExact
	if len(parsed.issues) > 0 {
		state, completeness = contracts.ProviderPartial, contracts.CompletenessPartial
	}
	return contracts.ProviderSnapshot{
		Provider:     contracts.ProviderCodex,
		State:        state,
		ObservedAt:   observedAt,
		ReceivedAt:   observedAt,
		Source:       source,
		Freshness:    contracts.Freshness{State: contracts.FreshnessCurrent, MaxAgeSeconds: freshnessMaxAgeSeconds},
		Completeness: completeness,
		Plan:         parsed.plan,
		UsageAllowed: parsed.usageAllowed,
		Meters:       parsed.meters,
		Issues:       parsed.issues,
	}
}

type failure struct {
	state   contracts.ProviderState
	phase   contracts.ErrorPhase
	code    string
	message string
	hint    string
}

// classifyReadError never copies provider error text into the snapshot,
// because it can name the account.
func classifyReadError(err error) failure {
	var rpcErr *rpcError
	switch {
	case errors.As(err, &rpcErr) && rpcErr.Method == methodInitialize:
		return failure{contracts.ProviderUnavailable, contracts.ErrorStart, "initialize_failed", "The Codex app-server rejected initialization.", retryHint}
	case errors.As(err, &rpcErr) && authMessage.MatchString(rpcErr.Message):
		return failure{contracts.ProviderAuthRequired, contracts.ErrorAuthenticate, "not_authenticated", "The Codex CLI reported that it is not signed in.", loginHint}
	case errors.As(err, &rpcErr):
		return failure{contracts.ProviderUnavailable, contracts.ErrorRead, "rate_limit_read_failed", "The Codex app-server returned an error for the rate-limit read.", retryHint}
	case errors.Is(err, errStart):
		return failure{contracts.ProviderUnavailable, contracts.ErrorStart, "start_failed", "The Codex app-server could not be started.", retryHint}
	case errors.Is(err, context.DeadlineExceeded):
		return failure{contracts.ProviderUnavailable, contracts.ErrorRead, "timeout", "The Codex app-server did not answer in time.", retryHint}
	case errors.Is(err, errMalformedMessage):
		return failure{contracts.ProviderMalformedResponse, contracts.ErrorParse, "malformed_response", "The Codex app-server sent output that is not JSON-RPC.", upgradeOrReportHint}
	default:
		return failure{contracts.ProviderUnavailable, contracts.ErrorRead, "app_server_failed", "The Codex app-server stopped before answering.", retryHint}
	}
}

func (r *reader) failed(source *contracts.Source, cause failure) contracts.ProviderSnapshot {
	now := r.now()
	return contracts.ProviderSnapshot{
		Provider:     contracts.ProviderCodex,
		State:        cause.state,
		ObservedAt:   now,
		ReceivedAt:   now,
		Source:       source,
		Freshness:    contracts.Freshness{State: contracts.FreshnessExpired, MaxAgeSeconds: freshnessMaxAgeSeconds},
		Completeness: contracts.CompletenessPartial,
		Error: &contracts.ProviderError{
			Code:         cause.code,
			Phase:        cause.phase,
			Message:      cause.message,
			RecoveryHint: cause.hint,
			ObservedAt:   now,
		},
	}
}
