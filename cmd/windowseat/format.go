package main

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"windowseat/internal/contracts"
	"windowseat/internal/display"
)

func formatUsage(w io.Writer, snapshot contracts.Snapshot, now time.Time, loc *time.Location) {
	table := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, p := range snapshot.Providers {
		if len(p.Meters) == 0 {
			fmt.Fprintf(table, "%s\t[%s] %s\n", p.Provider, p.State, display.ErrorText(p.Error))
			continue
		}
		for _, meter := range p.Meters {
			cells := []string{string(p.Provider), meter.Label, display.MeterValue(meter)}
			if tail := display.ResetText(meter.Window, now, loc) + markers(p); tail != "" {
				cells = append(cells, tail)
			}
			fmt.Fprintln(table, strings.Join(cells, "\t"))
		}
		if p.Error != nil {
			fmt.Fprintf(table, "%s\tlast read failed: %s\n", p.Provider, display.ErrorText(p.Error))
		}
		if p.UsageAllowed != nil && !*p.UsageAllowed {
			fmt.Fprintf(table, "%s\tprovider reports usage not allowed\n", p.Provider)
		}
	}
	table.Flush()
}

// markers flags anything an agent or person should not take at face value.
func markers(p contracts.ProviderSnapshot) string {
	var flags []string
	if p.State != contracts.ProviderHealthy {
		flags = append(flags, string(p.State))
	}
	if p.Freshness.State != contracts.FreshnessCurrent && string(p.Freshness.State) != string(p.State) {
		flags = append(flags, string(p.Freshness.State))
	}
	if len(flags) == 0 {
		return ""
	}
	return "  [" + strings.Join(flags, ", ") + "]"
}

func formatHealth(w io.Writer, report contracts.HealthReport, loc *time.Location) {
	readiness := "not ready"
	if report.Ready {
		readiness = "ready"
	}
	if report.Service.Version != "" {
		readiness = "version " + report.Service.Version + ", " + readiness
	}
	fmt.Fprintf(w, "%-8s%s  %s\n", "service", report.Service.State, readiness)

	for _, p := range report.Providers {
		parts := []string{string(p.OperationalState)}
		if p.State != contracts.ProviderHealthy {
			parts[0] += " [" + string(p.State) + "]"
		}
		parts = append(parts, "checked "+p.CheckedAt.In(loc).Format("15:04"))
		if p.LastError != nil {
			parts = append(parts, "last error: "+display.ErrorText(p.LastError))
		}
		fmt.Fprintf(w, "%-8s%s\n", p.Provider, strings.Join(parts, "  "))
	}
}
