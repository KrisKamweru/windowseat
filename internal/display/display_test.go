package display

import (
	"testing"
	"time"
)

func TestHumanDuration(t *testing.T) {
	for d, want := range map[time.Duration]string{
		30 * time.Second:                             "<1m",
		45 * time.Minute:                             "45m",
		2*time.Hour + 10*time.Minute:                 "2h10m",
		6*24*time.Hour + 3*time.Hour:                 "6d3h",
		2*time.Hour + 9*time.Minute + 50*time.Second: "2h10m",
	} {
		if got := HumanDuration(d); got != want {
			t.Errorf("HumanDuration(%s) = %q, want %q", d, got, want)
		}
	}
}
