package install

import (
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func linuxEnv() Env {
	return Env{
		OS:         "linux",
		Path:       "/home/k/.nvm/bin:/usr/bin",
		Home:       "/home/k",
		ConfigHome: "/home/k/.config",
		TempDir:    "/tmp",
		SourceDir:  "/src/dist",
		Executable: "/src/dist/windowseat",
		TrayBuilt:  true,
		HasDisplay: true,
		StateDir:   "/home/k/.local/state/windowseat",
		SocketDir:  "/run/user/1000/windowseat",
	}
}

func windowsEnv() Env {
	return Env{
		OS:           "windows",
		Home:         `C:\Users\k`,
		LocalAppData: `C:\Users\k\AppData\Local`,
		TempDir:      `C:\Temp`,
		SourceDir:    `C:\src`,
		Executable:   `C:\src\windowseat.exe`,
		TrayBuilt:    true,
		User:         `PC\k`,
		HasDisplay:   true,
		StateDir:     filepath.Join(`C:\Users\k\AppData\Local`, "windowseat"),
	}
}

func darwinEnv() Env {
	return Env{
		OS:         "darwin",
		Path:       "/opt/homebrew/bin:/usr/bin",
		Home:       "/Users/k",
		TempDir:    "/tmp",
		SourceDir:  "/src",
		Executable: "/src/windowseat",
		TrayBuilt:  true,
		UID:        501,
		HasDisplay: true,
		StateDir:   "/Users/k/Library/Caches/windowseat",
		SocketDir:  "/Users/k/Library/Caches/windowseat",
	}
}

// needsSlashPaths skips a test whose expected paths are written with forward
// slashes, which filepath.Join on a Windows host would not produce.
func needsSlashPaths(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("expected paths use forward slashes")
	}
}

func descriptions(plan Plan) []string {
	lines := make([]string, len(plan.Steps))
	for i, step := range plan.Steps {
		lines[i] = step.String()
	}
	return lines
}

func findStep[T Step](t *testing.T, plan Plan, match func(T) bool) T {
	t.Helper()
	for _, step := range plan.Steps {
		if typed, ok := step.(T); ok && match(typed) {
			return typed
		}
	}
	var zero T
	t.Fatalf("no matching %T step in plan:\n%s", zero, strings.Join(descriptions(plan), "\n"))
	return zero
}

func TestLinuxInstallPlan(t *testing.T) {
	needsSlashPaths(t)
	plan, err := InstallPlan(linuxEnv(), Options{})
	if err != nil {
		t.Fatal(err)
	}

	want := []Step{
		command{args: []string{"systemctl", "--user", "stop", "windowseat.service"}, mayFail: true},
		stopProcesses{exe: "/home/k/.local/bin/windowseat-tray"},
		copyFile{from: "/src/dist/windowseat", to: "/home/k/.local/bin/windowseat"},
		copyFile{from: "/src/dist/windowseat-tray", to: "/home/k/.local/bin/windowseat-tray"},
		writeFile{path: "/home/k/.config/systemd/user/windowseat.service", data: systemdUnit("/home/k/.local/bin/windowseat", "/home/k/.nvm/bin:/usr/bin")},
		command{args: []string{"systemctl", "--user", "daemon-reload"}},
		command{args: []string{"systemctl", "--user", "enable", "--now", "windowseat.service"}},
		writeFile{path: "/home/k/.config/autostart/windowseat-tray.desktop", data: desktopEntry("/home/k/.local/bin/windowseat-tray")},
		startDetached{exe: "/home/k/.local/bin/windowseat-tray"},
	}
	if !reflect.DeepEqual(plan.Steps, want) {
		t.Errorf("plan:\n%s", strings.Join(descriptions(plan), "\n"))
	}
}

func TestLinuxUnitAndDesktopEntry(t *testing.T) {
	unit := systemdUnit("/home/k/.local/bin/windowseat", `/home/k/100%/bin:/usr/bin`)
	for _, line := range []string{`ExecStart="/home/k/.local/bin/windowseat" serve`, `Environment="PATH=/home/k/100%%/bin:/usr/bin"`, "Restart=on-failure", "WantedBy=default.target"} {
		if !strings.Contains(unit, line+"\n") {
			t.Errorf("unit lacks %q:\n%s", line, unit)
		}
	}
	entry := desktopEntry("/home/k/.local/bin/windowseat-tray")
	for _, line := range []string{"[Desktop Entry]", "Type=Application", `Exec="/home/k/.local/bin/windowseat-tray"`} {
		if !strings.Contains(entry, line+"\n") {
			t.Errorf("desktop entry lacks %q:\n%s", line, entry)
		}
	}
}

func TestLinuxInstallWithoutDisplayStartsNoTrayAndSaysSo(t *testing.T) {
	env := linuxEnv()
	env.HasDisplay = false
	plan, err := InstallPlan(env, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range plan.Steps {
		if _, ok := step.(startDetached); ok {
			t.Errorf("plan starts the tray without a display: %s", step)
		}
	}
	if len(plan.Notes) == 0 {
		t.Error("plan has no note about the tray starting at the next graphical login")
	}
}

func TestInstallNoTraySkipsEveryTrayStep(t *testing.T) {
	for _, env := range []Env{linuxEnv(), windowsEnv(), darwinEnv()} {
		env.TrayBuilt = false
		plan, err := InstallPlan(env, Options{NoTray: true})
		if err != nil {
			t.Fatalf("%s: %v", env.OS, err)
		}
		for _, line := range descriptions(plan) {
			if strings.Contains(strings.ToLower(line), "tray") {
				t.Errorf("%s: --no-tray plan has a tray step: %s", env.OS, line)
			}
		}
	}
}

func TestInstallFailsWhenTrayBinaryIsMissing(t *testing.T) {
	env := linuxEnv()
	env.TrayBuilt = false
	_, err := InstallPlan(env, Options{})
	if err == nil || !strings.Contains(err.Error(), "--no-tray") {
		t.Fatalf("err = %v, want a hint about --no-tray", err)
	}
}

func TestInstallSkipsCopyWhenRunningFromInstallDir(t *testing.T) {
	env := linuxEnv()
	env.SourceDir = "/home/k/.local/bin"
	env.Executable = "/home/k/.local/bin/windowseat"
	plan, err := InstallPlan(env, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range plan.Steps {
		if _, ok := step.(copyFile); ok {
			t.Errorf("plan copies a binary onto itself: %s", step)
		}
	}
}

func TestInstallRejectsUnsupportedOS(t *testing.T) {
	env := linuxEnv()
	env.OS = "plan9"
	if _, err := InstallPlan(env, Options{}); err == nil {
		t.Error("InstallPlan accepted plan9")
	}
	if _, err := UninstallPlan(env, Options{}); err == nil {
		t.Error("UninstallPlan accepted plan9")
	}
}

func TestLinuxUninstallPlan(t *testing.T) {
	needsSlashPaths(t)
	plan, err := UninstallPlan(linuxEnv(), Options{})
	if err != nil {
		t.Fatal(err)
	}

	want := []Step{
		command{args: []string{"systemctl", "--user", "disable", "--now", "windowseat.service"}, mayFail: true},
		removeFile{path: "/home/k/.config/systemd/user/windowseat.service"},
		command{args: []string{"systemctl", "--user", "daemon-reload"}, mayFail: true},
		removeFile{path: "/home/k/.config/autostart/windowseat-tray.desktop"},
		stopProcesses{exe: "/home/k/.local/bin/windowseat-tray"},
		removeFile{path: "/home/k/.local/bin/windowseat"},
		removeFile{path: "/home/k/.local/bin/windowseat-tray"},
		removeDir{path: "/home/k/.local/state/windowseat"},
		removeDir{path: "/run/user/1000/windowseat"},
	}
	if !reflect.DeepEqual(plan.Steps, want) {
		t.Errorf("plan:\n%s", strings.Join(descriptions(plan), "\n"))
	}
}

func TestUninstallRemovesSharedStateDirOnce(t *testing.T) {
	plan, err := UninstallPlan(darwinEnv(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, step := range plan.Steps {
		if step == (removeDir{path: "/Users/k/Library/Caches/windowseat"}) {
			count++
		}
	}
	if count != 1 {
		t.Errorf("state dir removed %d times, want 1:\n%s", count, strings.Join(descriptions(plan), "\n"))
	}
}

func TestUninstallLeavesDirsNotNamedWindowSeat(t *testing.T) {
	env := linuxEnv()
	env.StateDir = "/home/k/.local/state"
	env.SocketDir = "/tmp"
	plan, err := UninstallPlan(env, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range plan.Steps {
		if dir, ok := step.(removeDir); ok {
			t.Errorf("plan removes a shared dir: %s", dir)
		}
	}
}

func TestWindowsInstallPlan(t *testing.T) {
	env := windowsEnv()
	plan, err := InstallPlan(env, Options{})
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(env.LocalAppData, "Programs", "WindowSeat")
	service := filepath.Join(dir, "windowseat.exe")
	tray := filepath.Join(dir, "windowseat-tray.exe")

	want := []Step{
		command{args: []string{"schtasks", "/End", "/TN", "WindowSeat"}, mayFail: true},
		command{args: []string{"schtasks", "/End", "/TN", "WindowSeat Tray"}, mayFail: true},
		copyFile{from: filepath.Join(env.SourceDir, "windowseat.exe"), to: service},
		copyFile{from: filepath.Join(env.SourceDir, "windowseat-tray.exe"), to: tray},
		registerTask{name: "WindowSeat", xml: taskXML(`PC\k`, service, "serve"), tempDir: env.TempDir},
		registerTask{name: "WindowSeat Tray", xml: taskXML(`PC\k`, tray, ""), tempDir: env.TempDir},
		command{args: []string{"schtasks", "/Run", "/TN", "WindowSeat"}},
		command{args: []string{"schtasks", "/Run", "/TN", "WindowSeat Tray"}},
	}
	if !reflect.DeepEqual(plan.Steps, want) {
		t.Errorf("plan:\n%s", strings.Join(descriptions(plan), "\n"))
	}
	if len(plan.Notes) == 0 || !strings.Contains(plan.Notes[0], dir) {
		t.Errorf("notes = %q, want a PATH note naming %s", plan.Notes, dir)
	}
}

func TestWindowsInstallSkipsCopyWhenPathsDifferOnlyInCase(t *testing.T) {
	env := windowsEnv()
	env.SourceDir = filepath.Join(env.LocalAppData, "programs", "windowseat")
	env.Executable = filepath.Join(env.SourceDir, "windowseat.exe")
	plan, err := InstallPlan(env, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range plan.Steps {
		if _, ok := step.(copyFile); ok {
			t.Errorf("plan copies a binary onto itself: %s", step)
		}
	}
}

func TestTaskXML(t *testing.T) {
	xml := taskXML(`PC\k&co`, `C:\Program Files\windowseat.exe`, "serve")
	for _, part := range []string{
		"<LogonTrigger>", `<UserId>PC\k&amp;co</UserId>`,
		"<LogonType>InteractiveToken</LogonType>", "<RunLevel>LeastPrivilege</RunLevel>",
		"<MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>",
		"<DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>",
		"<StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>",
		"<ExecutionTimeLimit>PT0S</ExecutionTimeLimit>",
		"<Interval>PT1M</Interval>", "<Count>3</Count>",
		`<Command>C:\Program Files\windowseat.exe</Command>`, "<Arguments>serve</Arguments>",
	} {
		if !strings.Contains(xml, part) {
			t.Errorf("task XML lacks %q:\n%s", part, xml)
		}
	}
	if strings.Count(xml, `<UserId>PC\k&amp;co</UserId>`) != 2 {
		t.Error("task XML should name the user in the trigger and the principal")
	}
	if strings.Contains(taskXML(`PC\k`, `C:\tray.exe`, ""), "<Arguments>") {
		t.Error("task XML has an Arguments element for a command without arguments")
	}
}

func TestWindowsUninstallPlan(t *testing.T) {
	env := windowsEnv()
	env.Executable = filepath.Join(env.LocalAppData, "Programs", "WindowSeat", "windowseat.exe")
	plan, err := UninstallPlan(env, Options{})
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(env.LocalAppData, "Programs", "WindowSeat")

	want := []Step{
		command{args: []string{"schtasks", "/End", "/TN", "WindowSeat"}, mayFail: true},
		command{args: []string{"schtasks", "/Delete", "/TN", "WindowSeat", "/F"}, mayFail: true},
		command{args: []string{"schtasks", "/End", "/TN", "WindowSeat Tray"}, mayFail: true},
		command{args: []string{"schtasks", "/Delete", "/TN", "WindowSeat Tray", "/F"}, mayFail: true},
		moveAside{from: filepath.Join(dir, "windowseat.exe"), to: filepath.Join(env.TempDir, "windowseat-uninstalled.exe")},
		removeFile{path: filepath.Join(dir, "windowseat-tray.exe")},
		removeEmptyDir{path: dir},
		removeFromUserPath{dir: dir},
		removeDir{path: env.StateDir},
	}
	if !reflect.DeepEqual(plan.Steps, want) {
		t.Errorf("plan:\n%s", strings.Join(descriptions(plan), "\n"))
	}
	if !strings.Contains(plan.Steps[4].String(), "running") {
		t.Errorf("rename step does not explain itself: %s", plan.Steps[4])
	}
}

func TestWindowsUninstallFromAnotherCopyDeletesInstalledBinary(t *testing.T) {
	plan, err := UninstallPlan(windowsEnv(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	installed := filepath.Join(windowsEnv().LocalAppData, "Programs", "WindowSeat", "windowseat.exe")
	findStep(t, plan, func(s removeFile) bool { return s.path == installed })
}

func TestDarwinInstallPlan(t *testing.T) {
	needsSlashPaths(t)
	plan, err := InstallPlan(darwinEnv(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	dir := "/Users/k/Library/Application Support/WindowSeat/bin"
	agents := "/Users/k/Library/LaunchAgents"

	want := []Step{
		command{args: []string{"launchctl", "bootout", "gui/501/dev.windowseat.service"}, mayFail: true},
		command{args: []string{"launchctl", "bootout", "gui/501/dev.windowseat.tray"}, mayFail: true},
		copyFile{from: "/src/windowseat", to: dir + "/windowseat"},
		copyFile{from: "/src/windowseat-tray", to: dir + "/windowseat-tray"},
		writeFile{path: agents + "/dev.windowseat.service.plist", data: launchAgent("dev.windowseat.service", true, "/opt/homebrew/bin:/usr/bin", dir+"/windowseat", "serve")},
		writeFile{path: agents + "/dev.windowseat.tray.plist", data: launchAgent("dev.windowseat.tray", false, "/opt/homebrew/bin:/usr/bin", dir+"/windowseat-tray")},
		command{args: []string{"launchctl", "bootstrap", "gui/501", agents + "/dev.windowseat.service.plist"}},
		command{args: []string{"launchctl", "bootstrap", "gui/501", agents + "/dev.windowseat.tray.plist"}},
	}
	if !reflect.DeepEqual(plan.Steps, want) {
		t.Errorf("plan:\n%s", strings.Join(descriptions(plan), "\n"))
	}
}

func TestLaunchAgent(t *testing.T) {
	plist := launchAgent("dev.windowseat.service", true, "/a&b:/usr/bin", "/a b/windowseat", "serve")
	for _, part := range []string{
		"<key>EnvironmentVariables</key>\n\t<dict>\n\t\t<key>PATH</key>\n\t\t<string>/a&amp;b:/usr/bin</string>\n\t</dict>",
		"<key>Label</key>\n\t<string>dev.windowseat.service</string>",
		"<string>/a b/windowseat</string>\n\t\t<string>serve</string>",
		"<key>RunAtLoad</key>\n\t<true/>",
		"<key>KeepAlive</key>\n\t<true/>",
	} {
		if !strings.Contains(plist, part) {
			t.Errorf("plist lacks %q:\n%s", part, plist)
		}
	}
	if strings.Contains(launchAgent("dev.windowseat.tray", false, "/usr/bin", "/tray"), "KeepAlive") {
		t.Error("tray plist keeps the tray alive")
	}
}

func TestDarwinUninstallPlan(t *testing.T) {
	needsSlashPaths(t)
	plan, err := UninstallPlan(darwinEnv(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	app := "/Users/k/Library/Application Support/WindowSeat"
	agents := "/Users/k/Library/LaunchAgents"

	want := []Step{
		command{args: []string{"launchctl", "bootout", "gui/501/dev.windowseat.service"}, mayFail: true},
		removeFile{path: agents + "/dev.windowseat.service.plist"},
		command{args: []string{"launchctl", "bootout", "gui/501/dev.windowseat.tray"}, mayFail: true},
		removeFile{path: agents + "/dev.windowseat.tray.plist"},
		removeFile{path: app + "/bin/windowseat"},
		removeFile{path: app + "/bin/windowseat-tray"},
		removeEmptyDir{path: app + "/bin"},
		removeEmptyDir{path: app},
		removeDir{path: "/Users/k/Library/Caches/windowseat"},
	}
	if !reflect.DeepEqual(plan.Steps, want) {
		t.Errorf("plan:\n%s", strings.Join(descriptions(plan), "\n"))
	}
}

func TestWindowsInstallAndUninstallUseTheChosenDir(t *testing.T) {
	env := windowsEnv()
	chosen := Options{Dir: `D:\Tools\WindowSeat`}
	service := filepath.Join(chosen.Dir, "windowseat.exe")

	install, err := InstallPlan(env, chosen)
	if err != nil {
		t.Fatal(err)
	}
	findStep(t, install, func(s copyFile) bool { return s.to == service })
	findStep(t, install, func(s registerTask) bool { return s.xml == taskXML(`PC\k`, service, "serve") })

	uninstall, err := UninstallPlan(env, chosen)
	if err != nil {
		t.Fatal(err)
	}
	findStep(t, uninstall, func(s removeFile) bool { return s.path == service })
	findStep(t, uninstall, func(s removeEmptyDir) bool { return s.path == chosen.Dir })
	findStep(t, uninstall, func(s removeFromUserPath) bool { return s.dir == chosen.Dir })
}

func TestDirIsWindowsOnlyAndAbsolute(t *testing.T) {
	if _, err := InstallPlan(linuxEnv(), Options{Dir: "/opt/windowseat"}); err == nil {
		t.Error("linux install accepted --dir")
	}
	if _, err := UninstallPlan(darwinEnv(), Options{Dir: "/opt/windowseat"}); err == nil {
		t.Error("darwin uninstall accepted --dir")
	}
	if _, err := InstallPlan(windowsEnv(), Options{Dir: `Tools\WindowSeat`}); err == nil {
		t.Error("windows install accepted a relative --dir")
	}
}
