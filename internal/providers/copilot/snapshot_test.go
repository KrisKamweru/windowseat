package copilot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"windowseat/internal/contracts"
	"windowseat/internal/providers/command"
)

// recordedReadAt follows the recorded resetDate, which runtime 1.0.80 fills
// with the time the quota was read.
var recordedReadAt = time.Date(2026, 10, 20, 12, 45, 16, 0, time.UTC)

func TestRecordedReadMapsToPartialSnapshot(t *testing.T) {
	snapshot := readingSnapshot(recordedReading(t))

	assertValid(t, snapshot)
	if snapshot.State != contracts.ProviderPartial || snapshot.Completeness != contracts.CompletenessPartial {
		t.Fatalf("state = %q, completeness = %q; want partial, partial", snapshot.State, snapshot.Completeness)
	}
	if snapshot.Freshness != (contracts.Freshness{State: contracts.FreshnessCurrent, MaxAgeSeconds: 900}) {
		t.Fatalf("Freshness = %+v", snapshot.Freshness)
	}
	wantAccount := contracts.Account{Label: "fixture-copilot-account", AuthType: "user", Host: "github.com"}
	if snapshot.Account == nil || *snapshot.Account != wantAccount {
		t.Fatalf("Account = %+v, want %+v", snapshot.Account, wantAccount)
	}

	source := snapshot.Source
	if source.Name != contracts.SourceCopilotSDK || source.CommandPath != "/usr/bin/copilot" || source.CommandVersion != "1.0.80" {
		t.Fatalf("Source = %+v", source)
	}
	if source.RuntimeVersion != "1.0.80" || source.PackageVersion != "1.0.80" || source.ProtocolVersion != "3" {
		t.Fatalf("Source versions = %+v", source)
	}
	if source.ResponseProfile != "copilot_quota_1_0_78" || source.ProfilePreferredFrom != "1.0.78" || source.VersionBeforeProfile {
		t.Fatalf("Source profile = %+v", source)
	}

	ids := meterIDs(snapshot.Meters)
	if ids != "chat,completions,premium_interactions" {
		t.Fatalf("meter IDs = %s", ids)
	}
	premium := snapshot.Meters[2]
	if premium.Label != "Premium interactions" || premium.Kind != contracts.MeterQuota {
		t.Fatalf("premium meter = %+v", premium)
	}
	if *premium.Used != 1838 || *premium.Entitlement != 2000 || *premium.RemainingPercentage != 8.1 {
		t.Fatalf("premium values used=%v entitlement=%v remaining=%v", *premium.Used, *premium.Entitlement, *premium.RemainingPercentage)
	}
	if *premium.Unlimited || !*premium.HasQuota || !*premium.TokenBasedBilling ||
		!*premium.UsageAllowedWhenExhausted || !*premium.OverageAllowedWhenExhausted {
		t.Fatalf("premium flags = %+v", premium)
	}
	if premium.Overage == nil || *premium.Overage != (contracts.Overage{}) {
		t.Fatalf("premium overage = %+v", premium.Overage)
	}
	if premium.Window.ResetsAt != nil || premium.Window.RawResetAt != "2026-10-20T12:45:15.895Z" {
		t.Fatalf("premium window = %+v", premium.Window)
	}

	if len(snapshot.Issues) != 3 {
		t.Fatalf("Issues = %+v, want one per meter", snapshot.Issues)
	}
	for _, issue := range snapshot.Issues {
		if issue.Code != "reset_time_not_future" || issue.Field != "window.resets_at" {
			t.Fatalf("issue = %+v", issue)
		}
	}
}

func TestFutureResetDateBecomesResetTime(t *testing.T) {
	reading := recordedReading(t)
	reading.ObservedAt = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	reading.ReceivedAt = reading.ObservedAt

	snapshot := readingSnapshot(reading)

	assertValid(t, snapshot)
	if snapshot.State != contracts.ProviderHealthy || len(snapshot.Issues) != 0 {
		t.Fatalf("state = %q, issues = %+v; want healthy without issues", snapshot.State, snapshot.Issues)
	}
	window := snapshot.Meters[0].Window
	want := time.Date(2026, 10, 20, 12, 45, 15, 895_000_000, time.UTC)
	if window.ResetsAt == nil || !window.ResetsAt.Equal(want) || window.RawResetAt != "" {
		t.Fatalf("window = %+v, want resets_at %s", window, want)
	}
}

func TestUnknownQuotaKeyIsKeptWithReadableLabel(t *testing.T) {
	reading := recordedReading(t)
	reading.Quota = quotaWith(t, map[string]any{"code_review": completeQuotaFields()})

	snapshot := readingSnapshot(reading)

	assertValid(t, snapshot)
	if len(snapshot.Meters) != 1 || snapshot.Meters[0].ID != "code_review" || snapshot.Meters[0].Label != "Code review" {
		t.Fatalf("Meters = %+v", snapshot.Meters)
	}
}

func TestOptionalQuotaFieldsMayBeAbsent(t *testing.T) {
	fields := completeQuotaFields()
	delete(fields, "hasQuota")
	delete(fields, "tokenBasedBilling")
	delete(fields, "overageEntitlement")
	reading := recordedReading(t)
	reading.Quota = quotaWith(t, map[string]any{"chat": fields})

	snapshot := readingSnapshot(reading)

	assertValid(t, snapshot)
	if snapshot.State != contracts.ProviderHealthy {
		t.Fatalf("state = %q, issues = %+v; want healthy", snapshot.State, snapshot.Issues)
	}
	meter := snapshot.Meters[0]
	if meter.HasQuota != nil || meter.TokenBasedBilling != nil || meter.Overage != nil {
		t.Fatalf("optional fields were invented: %+v", meter)
	}
}

func TestMissingOrUnreadableFieldsBecomeIssues(t *testing.T) {
	fields := completeQuotaFields()
	delete(fields, "usedRequests")
	fields["entitlementRequests"] = "many"
	reading := recordedReading(t)
	reading.Quota = quotaWith(t, map[string]any{"chat": fields})

	snapshot := readingSnapshot(reading)

	assertValid(t, snapshot)
	if snapshot.State != contracts.ProviderPartial {
		t.Fatalf("state = %q, want partial", snapshot.State)
	}
	meter := snapshot.Meters[0]
	if meter.Used != nil || meter.Entitlement != nil || meter.RemainingPercentage == nil {
		t.Fatalf("meter = %+v", meter)
	}
	got := issueSummary(snapshot.Issues)
	want := "field_missing:used,field_unreadable:entitlement"
	if got != want {
		t.Fatalf("issues = %s, want %s", got, want)
	}
}

func TestUnlimitedEntitlementSentinelIsOmitted(t *testing.T) {
	fields := completeQuotaFields()
	fields["isUnlimitedEntitlement"] = true
	fields["entitlementRequests"] = -1
	reading := recordedReading(t)
	reading.Quota = quotaWith(t, map[string]any{"chat": fields})

	snapshot := readingSnapshot(reading)

	assertValid(t, snapshot)
	if snapshot.State != contracts.ProviderHealthy || snapshot.Meters[0].Entitlement != nil {
		t.Fatalf("state = %q, meter = %+v", snapshot.State, snapshot.Meters[0])
	}
}

func TestUnusableQuotaIsMalformedNotZero(t *testing.T) {
	noPercentage := completeQuotaFields()
	delete(noPercentage, "remainingPercentage")

	cases := map[string]json.RawMessage{
		"not json":            json.RawMessage(`not json`),
		"no snapshots":        json.RawMessage(`{"quotaSnapshots":{}}`),
		"missing snapshots":   json.RawMessage(`{}`),
		"snapshot not object": json.RawMessage(`{"quotaSnapshots":{"chat":3}}`),
		"no percentage":       quotaWith(t, map[string]any{"chat": noPercentage}),
	}
	for name, quota := range cases {
		t.Run(name, func(t *testing.T) {
			reading := recordedReading(t)
			reading.Quota = quota

			snapshot := readingSnapshot(reading)

			assertValid(t, snapshot)
			if snapshot.State != contracts.ProviderMalformedResponse || len(snapshot.Meters) != 0 {
				t.Fatalf("state = %q, meters = %d; want malformed_response without meters", snapshot.State, len(snapshot.Meters))
			}
			if snapshot.Error.Phase != contracts.ErrorParse {
				t.Fatalf("Error = %+v", snapshot.Error)
			}
		})
	}
}

func TestSignedOutAccountRequiresAuthentication(t *testing.T) {
	reading := recordedReading(t)
	readFixture(t, "auth-status-signed-out.json", &reading.Auth)
	reading.Quota = nil

	snapshot := readingSnapshot(reading)

	assertValid(t, snapshot)
	if snapshot.State != contracts.ProviderAuthRequired || len(snapshot.Meters) != 0 || snapshot.Account != nil {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if snapshot.Error.Code != "not_authenticated" || snapshot.Error.Phase != contracts.ErrorAuthenticate {
		t.Fatalf("Error = %+v", snapshot.Error)
	}
}

func TestRuntimeVersionSelectsProfile(t *testing.T) {
	older := recordedReading(t)
	older.Runtime.Version = "1.0.70"
	unreadable := recordedReading(t)
	unreadable.Runtime.Version = "next"

	olderSnapshot := readingSnapshot(older)
	unreadableSnapshot := readingSnapshot(unreadable)

	assertValid(t, olderSnapshot)
	if !olderSnapshot.Source.VersionBeforeProfile || olderSnapshot.Source.ResponseProfile != "copilot_quota_1_0_78" {
		t.Fatalf("older Source = %+v", olderSnapshot.Source)
	}
	assertValid(t, unreadableSnapshot)
	if unreadableSnapshot.State != contracts.ProviderUnsupportedVersion {
		t.Fatalf("unreadable state = %q", unreadableSnapshot.State)
	}
}

func recordedReading(t *testing.T) reading {
	t.Helper()
	var auth authStatus
	readFixture(t, "auth-status.json", &auth)
	quota, err := os.ReadFile(filepath.Join("testdata", "1.0.80", "quota.json"))
	if err != nil {
		t.Fatal(err)
	}
	return reading{
		Command:        command.Command{Path: "/usr/bin/copilot", Version: "1.0.80"},
		PackageVersion: "1.0.80",
		Runtime:        connectResult{ProtocolVersion: 3, Version: "1.0.80"},
		Auth:           auth,
		Quota:          quota,
		ObservedAt:     recordedReadAt,
		ReceivedAt:     recordedReadAt.Add(time.Second),
	}
}

func completeQuotaFields() map[string]any {
	return map[string]any{
		"entitlementRequests":              300,
		"hasQuota":                         true,
		"isUnlimitedEntitlement":           false,
		"overage":                          0,
		"overageAllowedWithExhaustedQuota": false,
		"overageEntitlement":               0,
		"remainingPercentage":              50,
		"resetDate":                        "2026-11-01T00:00:00Z",
		"tokenBasedBilling":                false,
		"usageAllowedWithExhaustedQuota":   false,
		"usedRequests":                     150,
	}
}

func quotaWith(t *testing.T, snapshots map[string]any) json.RawMessage {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{"quotaSnapshots": snapshots})
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func readFixture(t *testing.T, name string, target any) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "1.0.80", name))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatal(err)
	}
}

func assertValid(t *testing.T, provider contracts.ProviderSnapshot) {
	t.Helper()
	snapshot := contracts.Snapshot{
		SchemaVersion: contracts.SchemaVersion,
		GeneratedAt:   provider.ReceivedAt,
		Providers:     []contracts.ProviderSnapshot{provider},
	}
	if err := snapshot.Validate(); err != nil {
		t.Fatalf("Validate() error = %v\nsnapshot = %+v", err, provider)
	}
}

func meterIDs(meters []contracts.Meter) string {
	ids := make([]string, len(meters))
	for index, meter := range meters {
		ids[index] = meter.ID
	}
	return strings.Join(ids, ",")
}

func issueSummary(issues []contracts.ObservationIssue) string {
	parts := make([]string, len(issues))
	for index, issue := range issues {
		parts[index] = issue.Code + ":" + issue.Field
	}
	return strings.Join(parts, ",")
}
