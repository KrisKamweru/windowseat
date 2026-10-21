package install

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

const systemdService = "windowseat.service"

type linuxPaths struct {
	bin, tray, unit, autostart string
}

func linuxLayout(env Env) linuxPaths {
	bin := filepath.Join(env.Home, ".local", "bin")
	return linuxPaths{
		bin:       bin,
		tray:      filepath.Join(bin, "windowseat-tray"),
		unit:      filepath.Join(env.ConfigHome, "systemd", "user", systemdService),
		autostart: filepath.Join(env.ConfigHome, "autostart", "windowseat-tray.desktop"),
	}
}

func systemdInstall(env Env, opts Options) Plan {
	paths := linuxLayout(env)
	steps := []Step{command{args: systemctl("stop", systemdService), mayFail: true}}
	if !opts.NoTray {
		steps = append(steps, stopProcesses{exe: paths.tray})
	}
	steps = append(steps, copyBinaries(env, paths.bin, env.binaries(opts)...)...)
	steps = append(steps,
		writeFile{path: paths.unit, data: systemdUnit(filepath.Join(paths.bin, "windowseat"), env.Path)},
		command{args: systemctl("daemon-reload")},
		command{args: systemctl("enable", "--now", systemdService)},
	)
	if opts.NoTray {
		return Plan{Steps: steps}
	}

	steps = append(steps, writeFile{path: paths.autostart, data: desktopEntry(paths.tray)})
	if !env.HasDisplay {
		return Plan{Steps: steps, Notes: []string{"No graphical display found, so the tray starts at your next graphical login."}}
	}
	return Plan{Steps: append(steps, startDetached{exe: paths.tray})}
}

func systemdUninstall(env Env) Plan {
	paths := linuxLayout(env)
	steps := []Step{
		command{args: systemctl("disable", "--now", systemdService), mayFail: true},
		removeFile{path: paths.unit},
		command{args: systemctl("daemon-reload"), mayFail: true},
		removeFile{path: paths.autostart},
		stopProcesses{exe: paths.tray},
	}
	steps = append(steps, removeFiles(paths.bin, "windowseat", "windowseat-tray")...)
	return Plan{Steps: append(steps, removeState(env)...)}
}

func systemctl(args ...string) []string {
	return append([]string{"systemctl", "--user"}, args...)
}

// systemdUnit carries the installer's PATH because systemd starts user
// services with its own short PATH, which misses nvm and ~/.local/bin.
func systemdUnit(exe, path string) string {
	return fmt.Sprintf(`[Unit]
Description=Windowseat AI coding quota monitor

[Service]
ExecStart=%q serve
Environment=%s
Restart=on-failure

[Install]
WantedBy=default.target
`, exe, strings.ReplaceAll(strconv.Quote("PATH="+path), "%", "%%"))
}

func desktopEntry(exe string) string {
	return fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=Windowseat
Comment=AI coding quota in the system tray
Exec=%q
Terminal=false
`, exe)
}
