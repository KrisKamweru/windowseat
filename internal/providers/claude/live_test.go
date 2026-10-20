package claude

import (
	"context"
	"os"
	"testing"
)

func TestLiveUsageReport(t *testing.T) {
	if os.Getenv("WINDOWSEAT_LIVE") != "1" {
		t.Skip("set WINDOWSEAT_LIVE=1 to run the real claude /usage command")
	}

	snapshot := NewReader(Options{}).Read(context.Background())

	requireValid(t, snapshot)
	if snapshot.Error != nil {
		t.Fatalf("state = %s, error = %+v", snapshot.State, snapshot.Error)
	}
	t.Logf("state=%s version=%s profile=%s", snapshot.State, snapshot.Source.CommandVersion, snapshot.Source.ResponseProfile)
	for _, meter := range snapshot.Meters {
		t.Logf("meter %s %q kind=%s used=%v window=%+v", meter.ID, meter.Label, meter.Kind, valueOf(meter.UsedPercentage), meter.Window)
	}
	for _, issue := range snapshot.Issues {
		t.Logf("issue %+v", issue)
	}
}

func valueOf(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}
