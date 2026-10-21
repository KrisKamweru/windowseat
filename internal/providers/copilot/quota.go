package copilot

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"windowseat/internal/contracts"
	"windowseat/internal/providers/versionprofile"
)

// quotaParser turns an account.getQuota result into meters and field issues.
// It returns an error only when no usable meter remains.
type quotaParser func(raw json.RawMessage, observedAt time.Time) ([]contracts.Meter, []contracts.ObservationIssue, error)

var quotaProfiles = versionprofile.MustNew([]versionprofile.Definition[quotaParser]{
	{ID: "copilot_quota_1_0_78", PreferredFrom: "1.0.78", Value: parseQuota},
})

func parseQuota(raw json.RawMessage, observedAt time.Time) ([]contracts.Meter, []contracts.ObservationIssue, error) {
	var response struct {
		QuotaSnapshots map[string]json.RawMessage `json:"quotaSnapshots"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, nil, fmt.Errorf("quota response is not an object: %w", err)
	}
	if len(response.QuotaSnapshots) == 0 {
		return nil, nil, errors.New("quota response has no snapshots")
	}

	var meters []contracts.Meter
	var issues []contracts.ObservationIssue
	for _, key := range slices.Sorted(maps.Keys(response.QuotaSnapshots)) {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(response.QuotaSnapshots[key], &fields); err != nil || fields == nil || key == "" {
			return nil, nil, fmt.Errorf("quota snapshot %q is not an object", key)
		}
		snapshot := snapshotFields{meterID: key, fields: fields}
		meters = append(meters, snapshot.meter(observedAt))
		issues = append(issues, snapshot.issues...)
	}

	if !slices.ContainsFunc(meters, func(meter contracts.Meter) bool { return meter.RemainingPercentage != nil }) {
		return nil, nil, errors.New("no quota snapshot has a readable remaining percentage")
	}
	return meters, issues, nil
}

// snapshotFields reads one provider quota snapshot and records an issue for
// every known field it cannot read.
type snapshotFields struct {
	meterID string
	fields  map[string]json.RawMessage
	issues  []contracts.ObservationIssue
}

func (s *snapshotFields) meter(observedAt time.Time) contracts.Meter {
	unlimited := readField[bool](s, "isUnlimitedEntitlement", "unlimited", true)
	return contracts.Meter{
		ID:                          s.meterID,
		Label:                       meterLabel(s.meterID),
		Kind:                        contracts.MeterQuota,
		Used:                        s.count("usedRequests", "used", true),
		Entitlement:                 s.entitlement(unlimited),
		RemainingPercentage:         s.percentage("remainingPercentage", "remaining_percentage"),
		Unlimited:                   unlimited,
		HasQuota:                    readField[bool](s, "hasQuota", "has_quota", false),
		TokenBasedBilling:           readField[bool](s, "tokenBasedBilling", "token_based_billing", false),
		UsageAllowedWhenExhausted:   readField[bool](s, "usageAllowedWithExhaustedQuota", "usage_allowed_when_exhausted", true),
		OverageAllowedWhenExhausted: readField[bool](s, "overageAllowedWithExhaustedQuota", "overage_allowed_when_exhausted", true),
		Overage:                     s.overage(),
		Window:                      s.window(observedAt),
	}
}

// entitlement omits the -1 sentinel that unlimited entitlements may carry.
func (s *snapshotFields) entitlement(unlimited *bool) *float64 {
	value := readField[float64](s, "entitlementRequests", "entitlement", true)
	if value == nil || *value >= 0 {
		return value
	}
	if unlimited == nil || !*unlimited {
		s.unreadable("entitlementRequests", "entitlement")
	}
	return nil
}

func (s *snapshotFields) count(name, field string, required bool) *float64 {
	value := readField[float64](s, name, field, required)
	if value != nil && *value < 0 {
		s.unreadable(name, field)
		return nil
	}
	return value
}

func (s *snapshotFields) percentage(name, field string) *float64 {
	value := s.count(name, field, true)
	if value != nil && *value > 100 {
		s.unreadable(name, field)
		return nil
	}
	return value
}

// overage needs both values because the contract has no partial overage.
// overageEntitlement is optional: SDK 1.0.23 dropped it from its type.
func (s *snapshotFields) overage() *contracts.Overage {
	used := s.count("overage", "overage.used", true)
	entitlement := s.count("overageEntitlement", "overage.entitlement", false)
	if used == nil || entitlement == nil {
		return nil
	}
	return &contracts.Overage{Entitlement: *entitlement, Used: *used}
}

// window keeps resetDate as a reset time only when it lies in the future.
// From runtime 1.0.78 it holds the time Copilot last read its quota, so a
// past value means the reset time is unknown, not that the read went wrong.
// It is kept as raw diagnostic data.
func (s *snapshotFields) window(observedAt time.Time) *contracts.TimeWindow {
	const field = "window.resets_at"
	raw := readField[string](s, "resetDate", field, true)
	if raw == nil {
		return nil
	}
	resetAt, err := time.Parse(time.RFC3339Nano, *raw)
	if err != nil {
		s.unreadable("resetDate", field)
		return &contracts.TimeWindow{RawResetAt: *raw}
	}
	if !resetAt.After(observedAt) {
		return &contracts.TimeWindow{RawResetAt: *raw}
	}
	return &contracts.TimeWindow{ResetsAt: &resetAt}
}

// readField decodes a provider field. A missing required field and any
// undecodable field become issues; a missing optional field stays absent.
func readField[T any](s *snapshotFields, name, field string, required bool) *T {
	raw, present := s.fields[name]
	if !present || string(raw) == "null" {
		if required {
			s.addIssue("field_missing", field, fmt.Sprintf("The provider did not return %s.", name))
		}
		return nil
	}
	var value T
	if err := json.Unmarshal(raw, &value); err != nil {
		s.unreadable(name, field)
		return nil
	}
	return &value
}

func (s *snapshotFields) unreadable(name, field string) {
	s.addIssue("field_unreadable", field, fmt.Sprintf("The provider returned an unreadable %s.", name))
}

func (s *snapshotFields) addIssue(code, field, message string) {
	s.issues = append(s.issues, contracts.ObservationIssue{Code: code, MeterID: s.meterID, Field: field, Message: message})
}

// meterLabel turns a provider key such as premium_interactions into
// "Premium interactions".
func meterLabel(key string) string {
	words := strings.ReplaceAll(key, "_", " ")
	first, size := utf8.DecodeRuneInString(words)
	return string(unicode.ToUpper(first)) + words[size:]
}
