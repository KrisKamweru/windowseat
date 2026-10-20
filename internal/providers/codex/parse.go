package codex

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"windowseat/internal/contracts"
)

const defaultLimitID = "codex"

var errNoUsableMeters = errors.New("rate-limit response has no usable meters")

// The response also carries accountId and rateLimitResetCredits. They are
// deliberately not decoded so they cannot reach a snapshot.
type rateLimitsResult struct {
	OrdinaryUsageAllowed *bool                    `json:"ordinaryUsageAllowed"`
	RateLimits           *limitSnapshot           `json:"rateLimits"`
	RateLimitsByLimitID  map[string]limitSnapshot `json:"rateLimitsByLimitId"`
}

type limitSnapshot struct {
	LimitID         string   `json:"limitId"`
	LimitName       string   `json:"limitName"`
	NormalModelSlug string   `json:"normalModelSlug"`
	PlanType        string   `json:"planType"`
	Primary         *window  `json:"primary"`
	Secondary       *window  `json:"secondary"`
	Credits         *credits `json:"credits"`
}

type window struct {
	UsedPercent        *float64 `json:"usedPercent"`
	WindowDurationMins *int64   `json:"windowDurationMins"`
	ResetsAt           *int64   `json:"resetsAt"`
}

type credits struct {
	HasCredits bool    `json:"hasCredits"`
	Unlimited  bool    `json:"unlimited"`
	Balance    *string `json:"balance"`
}

type reading struct {
	plan         string
	usageAllowed *bool
	meters       []contracts.Meter
	issues       []contracts.ObservationIssue
}

// parseRateLimits maps an account/rateLimits/read result to meters. Fields
// that fail to parse become issues; a response with no usable meter fails.
func parseRateLimits(result json.RawMessage, observedAt time.Time) (reading, error) {
	var response rateLimitsResult
	if err := json.Unmarshal(result, &response); err != nil {
		return reading{}, fmt.Errorf("decode rate limits: %w", err)
	}

	limits := response.limits()
	if len(limits) == 0 {
		return reading{}, errNoUsableMeters
	}

	parsed := reading{plan: limits[0].PlanType, usageAllowed: response.OrdinaryUsageAllowed}
	seen := map[string]bool{}
	for _, limit := range limits {
		meters, issues := limit.toMeters(observedAt)
		parsed.issues = append(parsed.issues, issues...)
		for _, meter := range meters {
			if seen[meter.ID] {
				parsed.issues = append(parsed.issues, issue("duplicate_meter", meter.ID, "id", "The provider returned two windows with the same duration."))
				continue
			}
			seen[meter.ID] = true
			parsed.meters = append(parsed.meters, meter)
		}
	}
	if len(parsed.meters) == 0 {
		return reading{}, errNoUsableMeters
	}
	return parsed, nil
}

// limits lists the default Codex limit first, then any other limits by ID.
// Older app-servers only return the single rateLimits view.
func (response rateLimitsResult) limits() []limitSnapshot {
	if len(response.RateLimitsByLimitID) == 0 {
		if response.RateLimits == nil {
			return nil
		}
		return []limitSnapshot{*response.RateLimits}
	}

	ids := make([]string, 0, len(response.RateLimitsByLimitID))
	for id := range response.RateLimitsByLimitID {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		if (ids[i] == defaultLimitID) != (ids[j] == defaultLimitID) {
			return ids[i] == defaultLimitID
		}
		return ids[i] < ids[j]
	})

	limits := make([]limitSnapshot, len(ids))
	for index, id := range ids {
		limit := response.RateLimitsByLimitID[id]
		limit.LimitID = id
		limits[index] = limit
	}
	return limits
}

func (limit limitSnapshot) toMeters(observedAt time.Time) ([]contracts.Meter, []contracts.ObservationIssue) {
	var meters []contracts.Meter
	var issues []contracts.ObservationIssue
	slots := []struct {
		name   string
		window *window
	}{{"primary", limit.Primary}, {"secondary", limit.Secondary}}

	for _, slot := range slots {
		if slot.window == nil {
			continue
		}
		meter, windowIssues := limit.windowMeter(slot.name, *slot.window, observedAt)
		meters = append(meters, meter)
		issues = append(issues, windowIssues...)
	}
	if limit.Credits != nil && (limit.Credits.HasCredits || limit.Credits.Unlimited) {
		meter, creditIssues := limit.creditMeter(*limit.Credits)
		meters = append(meters, meter)
		issues = append(issues, creditIssues...)
	}
	return meters, issues
}

func (limit limitSnapshot) windowMeter(slot string, source window, observedAt time.Time) (contracts.Meter, []contracts.ObservationIssue) {
	id, label := windowName(slot, source.WindowDurationMins)
	meter := contracts.Meter{
		ID:     limit.meterID(id),
		Label:  limit.meterLabel(label),
		Kind:   contracts.MeterRollingWindow,
		Model:  limit.NormalModelSlug,
		Window: &contracts.TimeWindow{},
	}
	var issues []contracts.ObservationIssue

	if duration := source.WindowDurationMins; duration != nil && *duration > 0 {
		seconds := *duration * 60
		meter.Window.DurationSeconds = &seconds
	} else {
		issues = append(issues, issue("missing_field", meter.ID, "window.duration_seconds", "The provider did not report a usable window duration."))
	}

	switch percent := source.UsedPercent; {
	case percent == nil:
		issues = append(issues, issue("missing_field", meter.ID, "used_percentage", "The provider did not report a used percentage."))
	case *percent < 0 || *percent > 100 || math.IsNaN(*percent):
		issues = append(issues, issue("out_of_range", meter.ID, "used_percentage", "The provider reported a used percentage outside 0 to 100."))
	default:
		meter.UsedPercentage = percent
	}

	if source.ResetsAt != nil {
		resetsAt := time.Unix(*source.ResetsAt, 0).UTC()
		if resetsAt.After(observedAt) {
			meter.Window.ResetsAt = &resetsAt
		} else {
			meter.Window.RawResetAt = resetsAt.Format(time.RFC3339)
			issues = append(issues, issue("reset_time_not_future", meter.ID, "window.resets_at", "The provider reset time was not later than the observation time."))
		}
	}

	if *meter.Window == (contracts.TimeWindow{}) {
		meter.Window = nil
	}
	return meter, issues
}

func (limit limitSnapshot) creditMeter(source credits) (contracts.Meter, []contracts.ObservationIssue) {
	meter := contracts.Meter{
		ID:        limit.meterID("credits"),
		Label:     limit.meterLabel("Credits"),
		Kind:      contracts.MeterCreditBalance,
		Unit:      "credits",
		Unlimited: &source.Unlimited,
	}
	if source.Balance == nil {
		return meter, nil
	}
	balance, err := strconv.ParseFloat(*source.Balance, 64)
	if err != nil || balance < 0 || math.IsInf(balance, 0) || math.IsNaN(balance) {
		return meter, []contracts.ObservationIssue{issue("unreadable_field", meter.ID, "remaining", "The provider credit balance was not a non-negative number.")}
	}
	meter.Remaining = &balance
	return meter, nil
}

func windowName(slot string, durationMins *int64) (id, label string) {
	if durationMins == nil || *durationMins <= 0 {
		return slot, strings.ToUpper(slot[:1]) + slot[1:] + " window"
	}
	switch *durationMins {
	case 300:
		return "five_hour", "Five-hour window"
	case 10080:
		return "seven_day", "Seven-day window"
	default:
		return fmt.Sprintf("window_%dm", *durationMins), fmt.Sprintf("%d-minute window", *durationMins)
	}
}

func (limit limitSnapshot) isDefault() bool {
	return limit.LimitID == "" || limit.LimitID == defaultLimitID
}

func (limit limitSnapshot) meterID(id string) string {
	if limit.isDefault() {
		return id
	}
	return limit.LimitID + "_" + id
}

func (limit limitSnapshot) meterLabel(label string) string {
	if limit.isDefault() {
		return label
	}
	name := limit.LimitName
	if name == "" {
		name = limit.LimitID
	}
	return name + " " + strings.ToLower(label[:1]) + label[1:]
}

func issue(code, meterID, field, message string) contracts.ObservationIssue {
	return contracts.ObservationIssue{Code: code, MeterID: meterID, Field: field, Message: message}
}
