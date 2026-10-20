package copilot

import (
	"encoding/json"
	"net/url"
	"strconv"
	"time"

	"windowseat/internal/contracts"
	"windowseat/internal/providers/command"
)

const freshnessMaxAgeSeconds = 900

// reading is everything one runtime session returned. Mapping it to a
// snapshot is pure, so recorded fixtures cover it without a process.
type reading struct {
	Command        command.Command
	PackageVersion string
	Runtime        connectResult
	Auth           authStatus
	Quota          json.RawMessage
	ObservedAt     time.Time
	ReceivedAt     time.Time
}

type failure struct {
	State        contracts.ProviderState
	Phase        contracts.ErrorPhase
	Code         string
	Message      string
	RecoveryHint string
}

var (
	commandMissing = failure{
		State: contracts.ProviderUnavailable, Phase: contracts.ErrorDiscover, Code: "command_not_found",
		Message:      "The copilot command was not found on PATH.",
		RecoveryHint: "Install the GitHub Copilot CLI for the service user, or add it to the service PATH.",
	}
	versionUnreadable = failure{
		State: contracts.ProviderUnavailable, Phase: contracts.ErrorDiscover, Code: "version_unreadable",
		Message: "The copilot command did not report a readable version.",
	}
	startFailed = failure{
		State: contracts.ProviderUnavailable, Phase: contracts.ErrorStart, Code: "start_failed",
		Message: "The Copilot runtime did not start or did not complete its handshake.",
	}
	authStatusFailed = failure{
		State: contracts.ProviderUnavailable, Phase: contracts.ErrorAuthenticate, Code: "auth_status_failed",
		Message: "The Copilot runtime did not report its authentication status.",
	}
	notAuthenticated = failure{
		State: contracts.ProviderAuthRequired, Phase: contracts.ErrorAuthenticate, Code: "not_authenticated",
		Message:      "The selected Copilot CLI reported no authenticated account.",
		RecoveryHint: "Sign in with the selected native Copilot CLI under the service user.",
	}
	quotaReadFailed = failure{
		State: contracts.ProviderUnavailable, Phase: contracts.ErrorRead, Code: "quota_read_failed",
		Message: "The Copilot runtime did not return quota.",
	}
	runtimeVersionUnsupported = failure{
		State: contracts.ProviderUnsupportedVersion, Phase: contracts.ErrorStart, Code: "runtime_version_unreadable",
		Message: "The Copilot runtime reported a version that cannot select a response profile.",
	}
	quotaMalformed = failure{
		State: contracts.ProviderMalformedResponse, Phase: contracts.ErrorParse, Code: "malformed_quota",
		Message: "The Copilot quota response had no usable meter.",
	}
)

func readingSnapshot(r reading) contracts.ProviderSnapshot {
	if !r.Auth.IsAuthenticated {
		return failedSnapshot(notAuthenticated, r)
	}
	profile, err := quotaProfiles.Select(r.Runtime.Version)
	if err != nil {
		return failedSnapshot(runtimeVersionUnsupported, r)
	}
	meters, issues, err := profile.Value(r.Quota, r.ObservedAt)
	if err != nil {
		return failedSnapshot(quotaMalformed, r)
	}

	source := r.source()
	source.ResponseProfile = profile.ID
	source.ProfilePreferredFrom = profile.PreferredFrom
	source.VersionBeforeProfile = profile.BeforePreferred

	state, completeness := contracts.ProviderHealthy, contracts.CompletenessExact
	if len(issues) > 0 {
		state, completeness = contracts.ProviderPartial, contracts.CompletenessPartial
	}
	return contracts.ProviderSnapshot{
		Provider:     contracts.ProviderCopilot,
		State:        state,
		ObservedAt:   r.ObservedAt,
		ReceivedAt:   r.ReceivedAt,
		Source:       source,
		Freshness:    contracts.Freshness{State: contracts.FreshnessCurrent, MaxAgeSeconds: freshnessMaxAgeSeconds},
		Completeness: completeness,
		Account:      r.account(),
		Meters:       meters,
		Issues:       issues,
	}
}

// failedSnapshot reports a failure without meters, so a failed read can
// never be mistaken for zero usage.
func failedSnapshot(f failure, r reading) contracts.ProviderSnapshot {
	return contracts.ProviderSnapshot{
		Provider:     contracts.ProviderCopilot,
		State:        f.State,
		ObservedAt:   r.ObservedAt,
		ReceivedAt:   r.ReceivedAt,
		Source:       r.source(),
		Freshness:    contracts.Freshness{State: contracts.FreshnessExpired, MaxAgeSeconds: freshnessMaxAgeSeconds},
		Completeness: contracts.CompletenessPartial,
		Error: &contracts.ProviderError{
			Code:         f.Code,
			Phase:        f.Phase,
			Message:      f.Message,
			RecoveryHint: f.RecoveryHint,
			ObservedAt:   r.ReceivedAt,
		},
	}
}

// source is absent until discovery has selected a command.
func (r reading) source() *contracts.Source {
	if r.Command.Path == "" {
		return nil
	}
	source := &contracts.Source{
		Name:           contracts.SourceCopilotSDK,
		CommandPath:    r.Command.Path,
		CommandVersion: r.Command.Version,
		Selection:      "path",
		RuntimeVersion: r.Runtime.Version,
		PackageVersion: r.PackageVersion,
	}
	if r.Runtime.ProtocolVersion > 0 {
		source.ProtocolVersion = strconv.FormatInt(r.Runtime.ProtocolVersion, 10)
	}
	return source
}

// account labels the signed-in user. The login is an account name, not a
// credential.
func (r reading) account() *contracts.Account {
	if r.Auth.Login == nil || *r.Auth.Login == "" {
		return nil
	}
	return &contracts.Account{
		Label:    *r.Auth.Login,
		AuthType: valueOrEmpty(r.Auth.AuthType),
		Host:     hostName(valueOrEmpty(r.Auth.Host)),
	}
}

// hostName reduces "https://github.com" to "github.com".
func hostName(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return raw
	}
	return parsed.Host
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
