// Package icon embeds the tray icons that render.py draws.
package icon

import "embed"

// Files holds state-<level>-64.png for Linux and macOS and state-<level>.ico for Windows.
//
//go:embed state-*-64.png state-*.ico
var Files embed.FS
