package service

import (
	"context"
	"time"

	"windowseat/internal/contracts"
)

// Health reports the service and every provider that has data.
func (s *Service) Health(context.Context) (contracts.HealthReport, error) {
	now := s.now()
	snapshot := s.build()

	report := contracts.HealthReport{
		SchemaVersion: contracts.SchemaVersion,
		ObservedAt:    now,
		Live:          true,
		Ready:         len(snapshot.Providers) > 0,
	}
	for _, sl := range s.slots {
		if e := sl.current(); e.known() {
			report.Providers = append(report.Providers, providerHealth(e, view(e, now)))
		}
	}
	report.Service = s.serviceHealth(snapshot, report.Providers, now)
	return report, nil
}

func (s *Service) serviceHealth(snapshot contracts.Snapshot, providers []contracts.ProviderHealth, now time.Time) contracts.ComponentHealth {
	startedAt := s.startedAt
	checks := []contracts.HealthCheck{
		snapshotCheck(snapshot, now),
		stateFileCheck(s.store, now),
	}
	return contracts.ComponentHealth{
		Component: contracts.ComponentService,
		State:     serviceState(checks, providers),
		Version:   s.version,
		StartedAt: &startedAt,
		Checks:    checks,
	}
}

func serviceState(checks []contracts.HealthCheck, providers []contracts.ProviderHealth) contracts.ComponentState {
	for _, check := range checks {
		if check.State == contracts.CheckFail {
			return contracts.ComponentDegraded
		}
	}
	for _, p := range providers {
		if p.OperationalState != contracts.ProviderOperationalHealthy && p.OperationalState != contracts.ProviderOperationalDisabled {
			return contracts.ComponentDegraded
		}
	}
	return contracts.ComponentHealthy
}

func snapshotCheck(snapshot contracts.Snapshot, now time.Time) contracts.HealthCheck {
	check := contracts.HealthCheck{Name: "snapshot_schema", State: contracts.CheckPass, CheckedAt: now}
	switch err := snapshot.Validate(); {
	case len(snapshot.Providers) == 0:
		check.State = contracts.CheckUnknown
		check.Message = "No provider has been read yet."
	case err != nil:
		check.State = contracts.CheckFail
		check.Code = "invalid_snapshot"
		check.Message = err.Error()
	}
	return check
}

func stateFileCheck(st *store, now time.Time) contracts.HealthCheck {
	check := contracts.HealthCheck{Name: "state_file", State: contracts.CheckPass, CheckedAt: now}
	switch err := st.err(); {
	case !st.enabled():
		check.State = contracts.CheckUnknown
		check.Message = "Persistence is off."
	case err != nil:
		check.State = contracts.CheckFail
		check.Code = "state_file_error"
		check.Message = err.Error()
	}
	return check
}

func providerHealth(e entry, served contracts.ProviderSnapshot) contracts.ProviderHealth {
	health := contracts.ProviderHealth{
		Provider:         served.Provider,
		State:            served.State,
		OperationalState: operationalState(served.State),
		CheckedAt:        e.attemptedAt,
		Source:           served.Source,
	}
	if health.CheckedAt.IsZero() {
		health.CheckedAt = e.newest().ReceivedAt
	}
	if e.lastValid != nil {
		health.LastSuccessfulReadAt = &e.lastValid.ReceivedAt
	}
	if e.lastFailure != nil {
		health.LastError = e.lastFailure.Error
	}
	if !e.attemptedAt.IsZero() && served.State != contracts.ProviderDisabled {
		health.Checks = []contracts.HealthCheck{readCheck(e)}
	}
	return health
}

func readCheck(e entry) contracts.HealthCheck {
	check := contracts.HealthCheck{Name: "read", State: contracts.CheckPass, CheckedAt: e.attemptedAt}
	if e.lastFailure != nil && e.lastFailure.Error != nil {
		check.State = contracts.CheckFail
		check.Code = e.lastFailure.Error.Code
		check.Message = e.lastFailure.Error.Message
	}
	return check
}

func operationalState(state contracts.ProviderState) contracts.ProviderOperationalState {
	switch state {
	case contracts.ProviderHealthy:
		return contracts.ProviderOperationalHealthy
	case contracts.ProviderPartial, contracts.ProviderLowerBound, contracts.ProviderStale:
		return contracts.ProviderOperationalDegraded
	case contracts.ProviderDisabled:
		return contracts.ProviderOperationalDisabled
	default:
		return contracts.ProviderOperationalUnavailable
	}
}
