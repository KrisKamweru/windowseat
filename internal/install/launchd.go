package install

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

type agent struct {
	label, binary string
	args          []string
	keepAlive     bool
}

var (
	serviceAgent = agent{label: "dev.windowseat.service", binary: "windowseat", args: []string{"serve"}, keepAlive: true}
	trayAgent    = agent{label: "dev.windowseat.tray", binary: "windowseat-tray"}
)

func macAppDir(env Env) string {
	return filepath.Join(env.Home, "Library", "Application Support", "Windowseat")
}

func macAgents(opts Options) []agent {
	if opts.NoTray {
		return []agent{serviceAgent}
	}
	return []agent{serviceAgent, trayAgent}
}

func (a agent) plist(env Env) string {
	return filepath.Join(env.Home, "Library", "LaunchAgents", a.label+".plist")
}

func guiDomain(env Env) string {
	return "gui/" + strconv.Itoa(env.UID)
}

func launchdInstall(env Env, opts Options) Plan {
	bin := filepath.Join(macAppDir(env), "bin")
	agents := macAgents(opts)
	var steps []Step
	for _, a := range agents {
		steps = append(steps, command{args: []string{"launchctl", "bootout", guiDomain(env) + "/" + a.label}, mayFail: true})
	}
	steps = append(steps, copyBinaries(env, bin, env.binaries(opts)...)...)
	for _, a := range agents {
		program := append([]string{filepath.Join(bin, a.binary)}, a.args...)
		steps = append(steps, writeFile{path: a.plist(env), data: launchAgent(a.label, a.keepAlive, env.Path, program...)})
	}
	for _, a := range agents {
		steps = append(steps, command{args: []string{"launchctl", "bootstrap", guiDomain(env), a.plist(env)}})
	}
	return Plan{Steps: steps}
}

func launchdUninstall(env Env) Plan {
	app := macAppDir(env)
	bin := filepath.Join(app, "bin")
	var steps []Step
	for _, a := range macAgents(Options{}) {
		steps = append(steps,
			command{args: []string{"launchctl", "bootout", guiDomain(env) + "/" + a.label}, mayFail: true},
			removeFile{path: a.plist(env)},
		)
	}
	steps = append(steps, removeFiles(bin, "windowseat", "windowseat-tray")...)
	steps = append(steps, removeEmptyDir{path: bin}, removeEmptyDir{path: app})
	return Plan{Steps: append(steps, removeState(env)...)}
}

// launchAgent carries the installer's PATH because launchd starts agents
// with a short default PATH that misses Homebrew and npm.
func launchAgent(label string, keepAlive bool, path string, program ...string) string {
	var args strings.Builder
	for _, arg := range program {
		args.WriteString("\n\t\t<string>" + escapeXML(arg) + "</string>")
	}
	keep := ""
	if keepAlive {
		keep = "\n\t<key>KeepAlive</key>\n\t<true/>"
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>%s
	</array>
	<key>EnvironmentVariables</key>
	<dict>
		<key>PATH</key>
		<string>%s</string>
	</dict>
	<key>RunAtLoad</key>
	<true/>%s
</dict>
</plist>
`, escapeXML(label), args.String(), escapeXML(path), keep)
}
