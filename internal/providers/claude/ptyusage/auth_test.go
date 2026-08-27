package ptyusage

import "testing"

func TestPersonalSubscription(t *testing.T) {
	tests := map[string]bool{
		"free":       true,
		"pro":        true,
		"max":        true,
		"MAX":        true,
		"team":       false,
		"enterprise": false,
		"":           false,
	}
	for subscriptionType, want := range tests {
		t.Run(subscriptionType, func(t *testing.T) {
			if got := personalSubscription(subscriptionType); got != want {
				t.Fatalf("personalSubscription(%q) = %t, want %t", subscriptionType, got, want)
			}
		})
	}
}
