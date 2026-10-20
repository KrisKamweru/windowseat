package claude

import (
	"time"

	"windowseat/internal/contracts"
	"windowseat/internal/providers/versionprofile"
)

// reading is the parsed content of one successful usage report.
type reading struct {
	meters []contracts.Meter
	issues []contracts.ObservationIssue
}

type parseFunc func(output []byte, observedAt time.Time) (reading, *failure)

var profiles = versionprofile.MustNew([]versionprofile.Definition[parseFunc]{
	{ID: "claude_usage_report_2_1_304", PreferredFrom: "2.1.304", Value: parseUsageReport},
})
