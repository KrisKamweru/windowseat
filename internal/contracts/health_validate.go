package contracts

import (
	"errors"
	"fmt"
	"math"
	"strings"
)

func (h HealthReport) Validate() error {
	if h.SchemaVersion != SchemaVersion {
		return fmt.Errorf("schema_version: got %q, want %q", h.SchemaVersion, SchemaVersion)
	}
	if h.ObservedAt.IsZero() {
		return errors.New("observed_at: required")
	}
	if h.Service.Component != ComponentService {
		return errors.New("service.component: must be service")
	}
	if err := h.Service.validate(); err != nil {
		return fmt.Errorf("service: %w", err)
	}

	seenProviders := make(map[Provider]struct{}, len(h.Providers))
	for i, provider := range h.Providers {
		if _, ok := seenProviders[provider.Provider]; ok {
			return fmt.Errorf("providers[%d].provider: duplicate %q", i, provider.Provider)
		}
		seenProviders[provider.Provider] = struct{}{}
		if err := provider.validate(); err != nil {
			return fmt.Errorf("providers[%d]: %w", i, err)
		}
	}

	seenComponents := map[Component]struct{}{ComponentService: {}}
	for i, component := range h.Components {
		if _, ok := seenComponents[component.Component]; ok {
			return fmt.Errorf("components[%d].component: duplicate %q", i, component.Component)
		}
		seenComponents[component.Component] = struct{}{}
		if err := component.validate(); err != nil {
			return fmt.Errorf("components[%d]: %w", i, err)
		}
	}

	return nil
}

func (c ComponentHealth) validate() error {
	if !c.Component.valid() {
		return fmt.Errorf("component: unsupported value %q", c.Component)
	}
	if !c.State.valid() {
		return fmt.Errorf("state: unsupported value %q", c.State)
	}
	return validateChecks(c.Checks)
}

func (p ProviderHealth) validate() error {
	if !p.Provider.valid() {
		return fmt.Errorf("provider: unsupported value %q", p.Provider)
	}
	if !p.State.valid() {
		return fmt.Errorf("state: unsupported value %q", p.State)
	}
	if !p.OperationalState.valid() {
		return fmt.Errorf("operational_state: unsupported value %q", p.OperationalState)
	}
	if p.CheckedAt.IsZero() {
		return errors.New("checked_at: required")
	}
	if p.Source != nil {
		if err := p.Source.validate(); err != nil {
			return fmt.Errorf("source: %w", err)
		}
	}
	if p.LastError != nil {
		if err := p.LastError.validate(); err != nil {
			return fmt.Errorf("last_error: %w", err)
		}
	}

	switch p.State {
	case ProviderHealthy, ProviderPartial, ProviderLowerBound, ProviderStale:
		if p.Source == nil {
			return errors.New("source: required when provider has a reading")
		}
	case ProviderAuthRequired, ProviderUnsupportedVersion, ProviderUnavailable, ProviderMalformedResponse:
		if p.LastError == nil {
			return errors.New("last_error: required for provider failure state")
		}
	}

	if err := validateChecks(p.Checks); err != nil {
		return err
	}
	return p.validateOperationalState()
}

func (p ProviderHealth) validateOperationalState() error {
	switch p.OperationalState {
	case ProviderOperationalHealthy:
		if p.State != ProviderHealthy {
			return errors.New("operational_state: healthy requires a healthy provider state")
		}
	case ProviderOperationalDegraded:
		if p.State != ProviderHealthy && p.State != ProviderPartial &&
			p.State != ProviderLowerBound && p.State != ProviderStale {
			return errors.New("operational_state: degraded requires a provider reading")
		}
	case ProviderOperationalUnavailable:
		if p.State != ProviderAuthRequired && p.State != ProviderUnsupportedVersion &&
			p.State != ProviderUnavailable && p.State != ProviderMalformedResponse {
			return errors.New("operational_state: unavailable requires a provider failure state")
		}
	case ProviderOperationalDisabled:
		if p.State != ProviderDisabled {
			return errors.New("operational_state: disabled requires a disabled provider state")
		}
	}

	return nil
}

func validateChecks(checks []HealthCheck) error {
	seen := make(map[string]struct{}, len(checks))
	for i, check := range checks {
		if strings.TrimSpace(check.Name) == "" {
			return fmt.Errorf("checks[%d].name: required", i)
		}
		if _, ok := seen[check.Name]; ok {
			return fmt.Errorf("checks[%d].name: duplicate %q", i, check.Name)
		}
		seen[check.Name] = struct{}{}
		if !check.State.valid() {
			return fmt.Errorf("checks[%d].state: unsupported value %q", i, check.State)
		}
		if check.CheckedAt.IsZero() {
			return fmt.Errorf("checks[%d].checked_at: required", i)
		}
		if check.DurationMS != nil && (math.IsNaN(*check.DurationMS) || math.IsInf(*check.DurationMS, 0) || *check.DurationMS < 0) {
			return fmt.Errorf("checks[%d].duration_ms: must be a finite non-negative number", i)
		}
	}
	return nil
}

func (c Component) valid() bool {
	switch c {
	case ComponentService, ComponentTray, ComponentWake, ComponentMCPBridge:
		return true
	default:
		return false
	}
}

func (s ComponentState) valid() bool {
	switch s {
	case ComponentHealthy, ComponentDegraded, ComponentUnavailable, ComponentDisabled, ComponentNotInstalled:
		return true
	default:
		return false
	}
}

func (s CheckState) valid() bool {
	return s == CheckPass || s == CheckWarning || s == CheckFail || s == CheckUnknown
}

func (s ProviderOperationalState) valid() bool {
	return s == ProviderOperationalHealthy || s == ProviderOperationalDegraded ||
		s == ProviderOperationalUnavailable || s == ProviderOperationalDisabled
}
