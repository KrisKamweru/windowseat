// Package install registers Windowseat to start at login and removes it again.
//
// Install and uninstall first build a Plan from an Env. Building a plan
// touches nothing, so every OS's plan is testable on any machine. Run then
// prints each step and, unless it is a dry run, carries it out.
package install

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Env is everything about the machine that a plan depends on.
type Env struct {
	OS           string
	Home         string
	ConfigHome   string // XDG_CONFIG_HOME, or ~/.config
	LocalAppData string
	TempDir      string
	SourceDir    string // directory of the running windowseat binary
	Executable   string // the running windowseat binary
	TrayBuilt    bool   // windowseat-tray sits next to the running binary
	User         string // DOMAIN\user, for Windows tasks
	UID          int    // for macOS launchctl domains
	HasDisplay   bool
	Path         string // PATH for the service, so it finds provider CLIs
	StateDir     string // holds the saved snapshot
	SocketDir    string // holds the IPC socket; empty on Windows
}

type Options struct {
	NoTray bool
}

// Plan is the ordered work for one command, plus notes to print at the end.
type Plan struct {
	Steps []Step
	Notes []string
}

func InstallPlan(env Env, opts Options) (Plan, error) {
	if !opts.NoTray && !env.TrayBuilt {
		return Plan{}, fmt.Errorf("%s is missing; build it or pass --no-tray", filepath.Join(env.SourceDir, env.exe("windowseat-tray")))
	}
	switch env.OS {
	case "linux":
		return systemdInstall(env, opts), nil
	case "windows":
		return schtasksInstall(env, opts), nil
	case "darwin":
		return launchdInstall(env, opts), nil
	}
	return Plan{}, unsupported(env)
}

func UninstallPlan(env Env) (Plan, error) {
	switch env.OS {
	case "linux":
		return systemdUninstall(env), nil
	case "windows":
		return schtasksUninstall(env), nil
	case "darwin":
		return launchdUninstall(env), nil
	}
	return Plan{}, unsupported(env)
}

func unsupported(env Env) error {
	return fmt.Errorf("install supports linux, windows and darwin, not %s", env.OS)
}

func (env Env) exe(name string) string {
	if env.OS == "windows" {
		return name + ".exe"
	}
	return name
}

// copyBinaries copies each named binary from the source dir into dir,
// unless the user is already running the installed copy.
func copyBinaries(env Env, dir string, names ...string) []Step {
	if env.samePath(env.SourceDir, dir) {
		return nil
	}
	steps := make([]Step, len(names))
	for i, name := range names {
		steps[i] = copyFile{from: filepath.Join(env.SourceDir, name), to: filepath.Join(dir, name)}
	}
	return steps
}

func removeFiles(dir string, names ...string) []Step {
	steps := make([]Step, len(names))
	for i, name := range names {
		steps[i] = removeFile{path: filepath.Join(dir, name)}
	}
	return steps
}

// removeState deletes Windowseat's own state and socket dirs. Both always end
// in a windowseat directory; anything else is left alone.
func removeState(env Env) []Step {
	dirs := []string{env.StateDir}
	if env.SocketDir != "" && !env.samePath(env.SocketDir, env.StateDir) {
		dirs = append(dirs, env.SocketDir)
	}
	var steps []Step
	for _, dir := range dirs {
		if strings.EqualFold(filepath.Base(dir), "windowseat") {
			steps = append(steps, removeDir{path: dir})
		}
	}
	return steps
}

func (env Env) samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if env.OS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// binaries names the binaries to install, leaving out the tray when skipped.
func (env Env) binaries(opts Options) []string {
	if opts.NoTray {
		return []string{env.exe("windowseat")}
	}
	return []string{env.exe("windowseat"), env.exe("windowseat-tray")}
}
