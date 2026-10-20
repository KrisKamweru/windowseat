package contracts

import "time"

const SchemaVersion = "1"

type Provider string

const (
	ProviderClaude  Provider = "claude"
	ProviderCodex   Provider = "codex"
	ProviderCopilot Provider = "copilot"
)

type ProviderState string

const (
	ProviderHealthy            ProviderState = "healthy"
	ProviderPartial            ProviderState = "partial"
	ProviderLowerBound         ProviderState = "lower_bound"
	ProviderStale              ProviderState = "stale"
	ProviderAuthRequired       ProviderState = "auth_required"
	ProviderUnsupportedVersion ProviderState = "unsupported_version"
	ProviderUnavailable        ProviderState = "provider_unavailable"
	ProviderMalformedResponse  ProviderState = "malformed_response"
	ProviderDisabled           ProviderState = "disabled"
)

type Completeness string

const (
	CompletenessExact      Completeness = "exact"
	CompletenessPartial    Completeness = "partial"
	CompletenessLowerBound Completeness = "lower_bound"
)

type FreshnessState string

const (
	FreshnessCurrent FreshnessState = "current"
	FreshnessStale   FreshnessState = "stale"
	FreshnessExpired FreshnessState = "expired"
)

type SourceName string

const (
	SourceClaudeStatusLine  SourceName = "claude_status_line"
	SourceClaudeUsageReport SourceName = "claude_usage_report"
	SourceCodexAppServer    SourceName = "codex_app_server"
	SourceCopilotSDK        SourceName = "copilot_sdk"
)

type MeterKind string

const (
	MeterRollingWindow MeterKind = "rolling_window"
	MeterQuota         MeterKind = "quota"
	MeterCreditBalance MeterKind = "credit_balance"
	MeterModelLimit    MeterKind = "model_limit"
)

type Snapshot struct {
	SchemaVersion string             `json:"schema_version"`
	GeneratedAt   time.Time          `json:"generated_at"`
	Providers     []ProviderSnapshot `json:"providers"`
}

type ProviderSnapshot struct {
	Provider     Provider      `json:"provider"`
	State        ProviderState `json:"state"`
	ObservedAt   time.Time     `json:"observed_at"`
	ReceivedAt   time.Time     `json:"received_at"`
	Source       *Source       `json:"source,omitempty"`
	Freshness    Freshness     `json:"freshness"`
	Completeness Completeness  `json:"completeness"`
	Account      *Account      `json:"account,omitempty"`
	Plan         string        `json:"plan,omitempty"`
	// UsageAllowed is the provider's own allow or deny answer, when it gives one.
	// Windowseat never derives it from meter percentages.
	UsageAllowed *bool              `json:"usage_allowed,omitempty"`
	Meters       []Meter            `json:"meters,omitempty"`
	Issues       []ObservationIssue `json:"issues,omitempty"`
	Error        *ProviderError     `json:"error,omitempty"`
}

type Source struct {
	Name                 SourceName `json:"name"`
	CommandPath          string     `json:"command_path"`
	CommandVersion       string     `json:"command_version"`
	Selection            string     `json:"selection,omitempty"`
	AdapterVersion       string     `json:"adapter_version,omitempty"`
	SDKVersion           string     `json:"sdk_version,omitempty"`
	RuntimeVersion       string     `json:"runtime_version,omitempty"`
	PackageVersion       string     `json:"package_version,omitempty"`
	ProtocolVersion      string     `json:"protocol_version,omitempty"`
	ResponseProfile      string     `json:"response_profile,omitempty"`
	ProfilePreferredFrom string     `json:"profile_preferred_from,omitempty"`
	VersionBeforeProfile bool       `json:"version_before_profile,omitempty"`
}

type ObservationIssue struct {
	Code    string `json:"code"`
	MeterID string `json:"meter_id,omitempty"`
	Field   string `json:"field"`
	Message string `json:"message"`
}

type Freshness struct {
	State         FreshnessState `json:"state"`
	MaxAgeSeconds int64          `json:"max_age_seconds"`
}

type Account struct {
	Label    string `json:"label"`
	AuthType string `json:"auth_type,omitempty"`
	Host     string `json:"host,omitempty"`
}

type Meter struct {
	ID                          string      `json:"id"`
	Label                       string      `json:"label"`
	Kind                        MeterKind   `json:"kind"`
	Scope                       string      `json:"scope,omitempty"`
	Model                       string      `json:"model,omitempty"`
	Unit                        string      `json:"unit,omitempty"`
	Used                        *float64    `json:"used,omitempty"`
	Entitlement                 *float64    `json:"entitlement,omitempty"`
	Remaining                   *float64    `json:"remaining,omitempty"`
	UsedPercentage              *float64    `json:"used_percentage,omitempty"`
	RemainingPercentage         *float64    `json:"remaining_percentage,omitempty"`
	Unlimited                   *bool       `json:"unlimited,omitempty"`
	HasQuota                    *bool       `json:"has_quota,omitempty"`
	TokenBasedBilling           *bool       `json:"token_based_billing,omitempty"`
	UsageAllowedWhenExhausted   *bool       `json:"usage_allowed_when_exhausted,omitempty"`
	OverageAllowedWhenExhausted *bool       `json:"overage_allowed_when_exhausted,omitempty"`
	Overage                     *Overage    `json:"overage,omitempty"`
	Window                      *TimeWindow `json:"window,omitempty"`
}

type Overage struct {
	Entitlement float64 `json:"entitlement"`
	Used        float64 `json:"used"`
}

type TimeWindow struct {
	DurationSeconds *int64     `json:"duration_seconds,omitempty"`
	ResetsAt        *time.Time `json:"resets_at,omitempty"`
	RawResetAt      string     `json:"raw_reset_at,omitempty"`
}

type ErrorPhase string

const (
	ErrorDiscover     ErrorPhase = "discover"
	ErrorStart        ErrorPhase = "start"
	ErrorAuthenticate ErrorPhase = "authenticate"
	ErrorRead         ErrorPhase = "read"
	ErrorParse        ErrorPhase = "parse"
	ErrorStop         ErrorPhase = "stop"
	ErrorCleanup      ErrorPhase = "cleanup"
)

type ProviderError struct {
	Code         string     `json:"code"`
	Phase        ErrorPhase `json:"phase"`
	Message      string     `json:"message"`
	RecoveryHint string     `json:"recovery_hint,omitempty"`
	ObservedAt   time.Time  `json:"observed_at"`
}
