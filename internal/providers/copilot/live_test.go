package copilot

import (
	"os"
	"testing"
)

// TestLiveRead reads the installed Copilot CLI. It starts one runtime, asks
// for auth status and quota, and stops. It creates no session and sends no
// prompt. Run it with WINDOWSEAT_LIVE=1.
func TestLiveRead(t *testing.T) {
	if os.Getenv("WINDOWSEAT_LIVE") != "1" {
		t.Skip("set WINDOWSEAT_LIVE=1 to read the installed Copilot CLI")
	}

	snapshot := NewReader(Options{}).Read(t.Context())

	assertValid(t, snapshot)
	t.Logf("state=%s completeness=%s source=%+v", snapshot.State, snapshot.Completeness, *snapshot.Source)
	t.Logf("account present=%t error=%+v", snapshot.Account != nil, snapshot.Error)
	for _, meter := range snapshot.Meters {
		t.Logf("meter %s used=%v entitlement=%v remaining=%v%% window=%+v",
			meter.ID, deref(meter.Used), deref(meter.Entitlement), deref(meter.RemainingPercentage), *meter.Window)
	}
	for _, issue := range snapshot.Issues {
		t.Logf("issue %s %s %s", issue.MeterID, issue.Field, issue.Code)
	}
}

func deref(value *float64) any {
	if value == nil {
		return "absent"
	}
	return *value
}
