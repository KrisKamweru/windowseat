package claude

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"windowseat/internal/contracts"
)

type streamEvent struct {
	Type         string       `json:"type"`
	UsageReport  *usageReport `json:"usage_report"`
	IsError      bool         `json:"is_error"`
	NumTurns     float64      `json:"num_turns"`
	TotalCostUSD float64      `json:"total_cost_usd"`
	Usage        tokenUsage   `json:"usage"`
	Result       string       `json:"result"`
	LocalCommand string       `json:"local_command"`
}

type tokenUsage struct {
	InputTokens              float64 `json:"input_tokens"`
	OutputTokens             float64 `json:"output_tokens"`
	CacheCreationInputTokens float64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     float64 `json:"cache_read_input_tokens"`
}

type usageReport struct {
	RateLimits *struct {
		Limits     []rateLimit `json:"limits"`
		ExtraUsage *extraUsage `json:"extra_usage"`
	} `json:"rate_limits"`
}

// authWording appears in claude's error text when no subscription login is usable.
// A bare "subscription" match is too broad: the healthy report says "using your subscription".
var authWording = []string{"not logged in", "/login", "log in", "invalid api key", "authentication"}

// parseUsageReport reads the claude_usage_report_2_1_304 stream: one assistant
// event carrying usage_report, then a result event proving nothing was spent.
func parseUsageReport(output []byte, observedAt time.Time) (reading, *failure) {
	report, result, fail := splitStream(output)
	if fail != nil {
		return reading{}, fail
	}
	if result.spentQuota() {
		return reading{}, &safetyCheckFailed
	}
	if result.IsError {
		if result.mentionsAuth() {
			return reading{}, &notAuthenticated
		}
		return reading{}, &commandReportedError
	}
	if result.LocalCommand != "usage" {
		return reading{}, &safetyCheckFailed
	}
	if report == nil || report.RateLimits == nil {
		if result.mentionsAuth() {
			return reading{}, &notAuthenticated
		}
		return reading{}, &reportMissing
	}
	return readLimits(report.RateLimits.Limits, report.RateLimits.ExtraUsage, observedAt)
}

func splitStream(output []byte) (*usageReport, *streamEvent, *failure) {
	var report *usageReport
	var result *streamEvent
	decoder := json.NewDecoder(bytes.NewReader(output))
	for {
		var event streamEvent
		err := decoder.Decode(&event)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, &streamUnreadable
		}
		switch {
		case event.Type == "assistant" && event.UsageReport != nil:
			report = event.UsageReport
		case event.Type == "result":
			result = &event
		}
	}
	if result == nil {
		return nil, nil, &resultMissing
	}
	return report, result, nil
}

func (e streamEvent) spentQuota() bool {
	usage := e.Usage
	return e.NumTurns != 0 || e.TotalCostUSD != 0 ||
		usage.InputTokens != 0 || usage.OutputTokens != 0 ||
		usage.CacheCreationInputTokens != 0 || usage.CacheReadInputTokens != 0
}

func (e streamEvent) mentionsAuth() bool {
	text := strings.ToLower(e.Result)
	for _, wording := range authWording {
		if strings.Contains(text, wording) {
			return true
		}
	}
	return false
}

func readLimits(limits []rateLimit, extra *extraUsage, observedAt time.Time) (reading, *failure) {
	if len(limits) == 0 {
		return reading{}, &reportEmpty
	}

	var result reading
	seen := make(map[string]bool, len(limits))
	hasValue := false
	for index, limit := range limits {
		meter, issues := limit.meter(index, observedAt)
		result.issues = append(result.issues, issues...)
		if meter == nil {
			continue
		}
		if seen[meter.ID] {
			return reading{}, &duplicateMeter
		}
		seen[meter.ID] = true
		hasValue = hasValue || meter.UsedPercentage != nil || (meter.Window != nil && meter.Window.ResetsAt != nil)
		result.meters = append(result.meters, *meter)
	}
	if !hasValue {
		return reading{}, &reportUnreadable
	}

	if extra != nil && extra.IsEnabled {
		meter, issues := extra.meter()
		result.meters = append(result.meters, meter)
		result.issues = append(result.issues, issues...)
	}
	return result, nil
}

func issue(code, meterID, field string) contracts.ObservationIssue {
	return contracts.ObservationIssue{Code: code, MeterID: meterID, Field: field, Message: issueMessages[code]}
}

var issueMessages = map[string]string{
	"limit_kind_missing":    "The provider reported a limit without a kind.",
	"percent_missing":       "The provider did not report a usage percentage.",
	"percent_out_of_range":  "The provider usage percentage was outside 0 to 100.",
	"reset_time_unreadable": "The provider reset time could not be read.",
	"reset_time_not_future": "The provider reset time was not later than the observation time.",
	"value_out_of_range":    "The provider reported a negative value.",
}
