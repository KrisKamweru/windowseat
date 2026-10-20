//go:build !windows

package main

import "windowseat/internal/tray"

func iconFile(level tray.Level) string { return "state-" + string(level) + "-64.png" }
