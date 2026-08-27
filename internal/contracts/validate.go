package contracts

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

func (s Snapshot) Validate() error {
	if s.SchemaVersion != SchemaVersion {
		return fmt.Errorf("schema_version: got %q, want %q", s.SchemaVersion, SchemaVersion)
	}
	if s.GeneratedAt.IsZero() {
		return errors.New("generated_at: required")
	}
	if len(s.Providers) == 0 {
		return errors.New("providers: at least one provider is required")
	}

	seen := make(map[Provider]struct{}, len(s.Providers))
	for i, provider := range s.Providers {
		if _, ok := seen[provider.Provider]; ok {
			return fmt.Errorf("providers[%d].provider: duplicate %q", i, provider.Provider)
		}
		seen[provider.Provider] = struct{}{}
		if err := provider.validate(); err != nil {
			return fmt.Errorf("providers[%d]: %w", i, err)
		}
	}

	return nil
}

func (p ProviderSnapshot) validate() error {
	if !p.Provider.valid() {
		return fmt.Errorf("provider: unsupported value %q", p.Provider)
	}
	if !p.State.valid() {
		return fmt.Errorf("state: unsupported value %q", p.State)
	}
	if p.ObservedAt.IsZero() {
		return errors.New("observed_at: required")
	}
	if p.ReceivedAt.IsZero() {
		return errors.New("received_at: required")
	}
	if p.ReceivedAt.Before(p.ObservedAt) {
		return errors.New("received_at: must not precede observed_at")
	}
	if p.Source != nil {
		if err := p.Source.validate(); err != nil {
			return fmt.Errorf("source: %w", err)
		}
	}
	if err := p.Freshness.validate(); err != nil {
		return fmt.Errorf("freshness: %w", err)
	}
	if !p.Completeness.valid() {
		return fmt.Errorf("completeness: unsupported value %q", p.Completeness)
	}
	if p.Account != nil && strings.TrimSpace(p.Account.Label) == "" {
		return errors.New("account.label: required when account is present")
	}

	if err := p.validateState(); err != nil {
		return err
	}

	seen := make(map[string]struct{}, len(p.Meters))
	for i, meter := range p.Meters {
		if _, ok := seen[meter.ID]; ok {
			return fmt.Errorf("meters[%d].id: duplicate %q", i, meter.ID)
		}
		seen[meter.ID] = struct{}{}
		if err := meter.validate(p.ObservedAt); err != nil {
			return fmt.Errorf("meters[%d]: %w", i, err)
		}
	}

	seenIssues := make(map[string]struct{}, len(p.Issues))
	for i, issue := range p.Issues {
		if err := issue.validate(); err != nil {
			return fmt.Errorf("issues[%d]: %w", i, err)
		}
		key := issue.MeterID + "\x00" + issue.Field + "\x00" + issue.Code
		if _, ok := seenIssues[key]; ok {
			return fmt.Errorf("issues[%d]: duplicate issue for meter %q field %q code %q", i, issue.MeterID, issue.Field, issue.Code)
		}
		seenIssues[key] = struct{}{}
	}

	if p.Error != nil {
		if err := p.Error.validate(); err != nil {
			return fmt.Errorf("error: %w", err)
		}
	}

	return nil
}

func (p ProviderSnapshot) validateState() error {
	switch p.State {
	case ProviderHealthy:
		if p.Completeness != CompletenessExact || p.Freshness.State != FreshnessCurrent {
			return errors.New("state: healthy requires exact, current data")
		}
		if len(p.Issues) != 0 {
			return errors.New("issues: healthy reading must not contain issues")
		}
	case ProviderPartial:
		if p.Completeness != CompletenessPartial || p.Freshness.State != FreshnessCurrent {
			return errors.New("state: partial requires partial, current data")
		}
		if len(p.Issues) == 0 {
			return errors.New("issues: partial reading requires at least one issue")
		}
	case ProviderLowerBound:
		if p.Completeness != CompletenessLowerBound || p.Freshness.State != FreshnessCurrent {
			return errors.New("state: lower_bound requires lower_bound, current data")
		}
	case ProviderStale:
		if p.Freshness.State == FreshnessCurrent {
			return errors.New("state: stale requires stale or expired freshness")
		}
		if p.Source == nil || len(p.Meters) == 0 {
			return errors.New("state: stale requires the last valid source and meters")
		}
	case ProviderAuthRequired, ProviderUnsupportedVersion, ProviderUnavailable, ProviderMalformedResponse:
		if p.Error == nil {
			return errors.New("error: required for provider failure state")
		}
		if len(p.Meters) != 0 {
			return errors.New("meters: provider failure state must not return meters")
		}
		if len(p.Issues) != 0 {
			return errors.New("issues: provider failure state must not return field issues")
		}
	case ProviderDisabled:
		if len(p.Meters) != 0 {
			return errors.New("meters: disabled provider must not return meters")
		}
		if len(p.Issues) != 0 {
			return errors.New("issues: disabled provider must not return field issues")
		}
	}

	if p.State.currentReading() {
		if p.Source == nil {
			return errors.New("source: current provider reading requires a source")
		}
		if strings.TrimSpace(p.Source.ResponseProfile) == "" {
			return errors.New("source.response_profile: current provider reading requires a response profile")
		}
		if len(p.Meters) == 0 {
			return errors.New("meters: current provider reading requires at least one meter")
		}
		if p.Error != nil {
			return errors.New("error: current provider reading must not include an error")
		}
	}
	if p.State == ProviderStale && strings.TrimSpace(p.Source.ResponseProfile) == "" {
		return errors.New("source.response_profile: stale provider reading requires the last valid response profile")
	}

	return nil
}

func (s Source) validate() error {
	if !s.Name.valid() {
		return fmt.Errorf("name: unsupported value %q", s.Name)
	}
	if strings.TrimSpace(s.CommandPath) == "" {
		return errors.New("command_path: required")
	}
	if strings.TrimSpace(s.CommandVersion) == "" {
		return errors.New("command_version: required")
	}
	responseProfile := strings.TrimSpace(s.ResponseProfile)
	preferredFrom := strings.TrimSpace(s.ProfilePreferredFrom)
	if (s.ResponseProfile != "" && responseProfile == "") ||
		(s.ProfilePreferredFrom != "" && preferredFrom == "") {
		return errors.New("response_profile and profile_preferred_from: must not be whitespace")
	}
	if (responseProfile == "") != (preferredFrom == "") {
		return errors.New("response_profile and profile_preferred_from: both are required when either is present")
	}
	if s.VersionBeforeProfile && responseProfile == "" {
		return errors.New("version_before_profile: response profile is required")
	}
	return nil
}

func (issue ObservationIssue) validate() error {
	if strings.TrimSpace(issue.Code) == "" {
		return errors.New("code: required")
	}
	if strings.TrimSpace(issue.Field) == "" {
		return errors.New("field: required")
	}
	if strings.TrimSpace(issue.Message) == "" {
		return errors.New("message: required")
	}
	return nil
}

func (f Freshness) validate() error {
	if !f.State.valid() {
		return fmt.Errorf("state: unsupported value %q", f.State)
	}
	if f.MaxAgeSeconds <= 0 {
		return errors.New("max_age_seconds: must be greater than zero")
	}
	return nil
}

func (m Meter) validate(observedAt time.Time) error {
	if strings.TrimSpace(m.ID) == "" {
		return errors.New("id: required")
	}
	if strings.TrimSpace(m.Label) == "" {
		return errors.New("label: required")
	}
	if !m.Kind.valid() {
		return fmt.Errorf("kind: unsupported value %q", m.Kind)
	}

	for name, value := range map[string]*float64{
		"used":                 m.Used,
		"entitlement":          m.Entitlement,
		"remaining":            m.Remaining,
		"used_percentage":      m.UsedPercentage,
		"remaining_percentage": m.RemainingPercentage,
	} {
		if value != nil && (math.IsNaN(*value) || math.IsInf(*value, 0) || *value < 0) {
			return fmt.Errorf("%s: must be a finite non-negative number", name)
		}
	}

	for name, value := range map[string]*float64{
		"used_percentage":      m.UsedPercentage,
		"remaining_percentage": m.RemainingPercentage,
	} {
		if value != nil && *value > 100 {
			return fmt.Errorf("%s: must not exceed 100", name)
		}
	}

	if m.Overage != nil {
		if !finiteNonNegative(m.Overage.Entitlement) || !finiteNonNegative(m.Overage.Used) {
			return errors.New("overage: values must be finite and non-negative")
		}
	}

	if m.Window != nil {
		if m.Window.DurationSeconds != nil && *m.Window.DurationSeconds <= 0 {
			return errors.New("window.duration_seconds: must be greater than zero")
		}
		if m.Window.ResetsAt != nil && !m.Window.ResetsAt.After(observedAt) {
			return errors.New("window.resets_at: must be later than observed_at")
		}
		if m.Window.ResetsAt != nil && m.Window.RawResetAt != "" {
			return errors.New("window: resets_at and raw_reset_at are mutually exclusive")
		}
	}

	return nil
}

func (e ProviderError) validate() error {
	if strings.TrimSpace(e.Code) == "" {
		return errors.New("code: required")
	}
	if !e.Phase.valid() {
		return fmt.Errorf("phase: unsupported value %q", e.Phase)
	}
	if strings.TrimSpace(e.Message) == "" {
		return errors.New("message: required")
	}
	if e.ObservedAt.IsZero() {
		return errors.New("observed_at: required")
	}
	return nil
}

func finiteNonNegative(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0
}

func (p Provider) valid() bool {
	switch p {
	case ProviderClaude, ProviderCodex, ProviderCopilot:
		return true
	default:
		return false
	}
}

func (s ProviderState) valid() bool {
	switch s {
	case ProviderHealthy, ProviderPartial, ProviderLowerBound, ProviderStale,
		ProviderAuthRequired, ProviderUnsupportedVersion, ProviderUnavailable,
		ProviderMalformedResponse, ProviderDisabled:
		return true
	default:
		return false
	}
}

func (s ProviderState) currentReading() bool {
	return s == ProviderHealthy || s == ProviderPartial || s == ProviderLowerBound
}

func (c Completeness) valid() bool {
	return c == CompletenessExact || c == CompletenessPartial || c == CompletenessLowerBound
}

func (s FreshnessState) valid() bool {
	return s == FreshnessCurrent || s == FreshnessStale || s == FreshnessExpired
}

func (s SourceName) valid() bool {
	switch s {
	case SourceClaudeStatusLine, SourceClaudePTYUsage, SourceCodexAppServer, SourceCopilotSDK:
		return true
	default:
		return false
	}
}

func (k MeterKind) valid() bool {
	switch k {
	case MeterRollingWindow, MeterQuota, MeterCreditBalance, MeterModelLimit:
		return true
	default:
		return false
	}
}

func (p ErrorPhase) valid() bool {
	switch p {
	case ErrorDiscover, ErrorStart, ErrorAuthenticate, ErrorRead, ErrorParse, ErrorStop, ErrorCleanup:
		return true
	default:
		return false
	}
}
