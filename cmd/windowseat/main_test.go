package main

import "testing"

func TestRunHelpExitsZero(t *testing.T) {
	for _, args := range [][]string{{"help"}, {"-h"}, {"--help"}, {"usage", "-h"}} {
		if code := run(args); code != 0 {
			t.Errorf("run(%q) = %d, want 0", args, code)
		}
	}
}
