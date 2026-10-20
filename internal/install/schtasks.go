package install

import (
	"fmt"
	"path/filepath"
	"strings"
)

type task struct {
	name, binary, args string
}

var (
	serviceTask = task{name: "Windowseat", binary: "windowseat.exe", args: "serve"}
	trayTask    = task{name: "Windowseat Tray", binary: "windowseat-tray.exe"}
)

func windowsDir(env Env) string {
	return filepath.Join(env.LocalAppData, "Programs", "Windowseat")
}

func windowsTasks(opts Options) []task {
	if opts.NoTray {
		return []task{serviceTask}
	}
	return []task{serviceTask, trayTask}
}

func schtasksInstall(env Env, opts Options) Plan {
	dir := windowsDir(env)
	tasks := windowsTasks(opts)
	var steps []Step
	for _, t := range tasks {
		steps = append(steps, command{args: schtasks("/End", t.name), mayFail: true})
	}
	steps = append(steps, copyBinaries(env, dir, env.binaries(opts)...)...)
	if opts.AddToPath {
		steps = append(steps, addToUserPath{dir: dir})
	}
	for _, t := range tasks {
		xml := taskXML(env.User, filepath.Join(dir, t.binary), t.args)
		steps = append(steps, registerTask{name: t.name, xml: xml, tempDir: env.TempDir})
	}
	for _, t := range tasks {
		steps = append(steps, command{args: schtasks("/Run", t.name)})
	}
	if opts.AddToPath {
		return Plan{Steps: steps, Notes: []string{"Open a new terminal to run windowseat from anywhere."}}
	}
	note := fmt.Sprintf("To run windowseat from a terminal, install again with --add-to-path or add %s to your PATH.", dir)
	return Plan{Steps: steps, Notes: []string{note}}
}

func schtasksUninstall(env Env) Plan {
	dir := windowsDir(env)
	var steps []Step
	for _, t := range windowsTasks(Options{}) {
		steps = append(steps,
			command{args: schtasks("/End", t.name), mayFail: true},
			command{args: append(schtasks("/Delete", t.name), "/F"), mayFail: true},
		)
	}

	service := filepath.Join(dir, serviceTask.binary)
	if env.samePath(env.Executable, service) {
		steps = append(steps, moveAside{from: service, to: filepath.Join(env.TempDir, "windowseat-uninstalled.exe")})
	} else {
		steps = append(steps, removeFile{path: service})
	}
	steps = append(steps,
		removeFile{path: filepath.Join(dir, trayTask.binary)},
		removeEmptyDir{path: dir},
		removeFromUserPath{dir: dir},
	)
	return Plan{Steps: append(steps, removeState(env)...)}
}

func schtasks(verb, name string) []string {
	return []string{"schtasks", verb, "/TN", name}
}

// taskXML describes a task that starts exe at the user's logon, as that
// user, without elevation, and restarts it if it fails.
func taskXML(user, exe, args string) string {
	arguments := ""
	if args != "" {
		arguments = "\n      <Arguments>" + escapeXML(args) + "</Arguments>"
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <Triggers>
    <LogonTrigger>
      <Enabled>true</Enabled>
      <UserId>%[1]s</UserId>
    </LogonTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">
      <UserId>%[1]s</UserId>
      <LogonType>InteractiveToken</LogonType>
      <RunLevel>LeastPrivilege</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <RestartOnFailure>
      <Interval>PT1M</Interval>
      <Count>3</Count>
    </RestartOnFailure>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>%[2]s</Command>%[3]s
    </Exec>
  </Actions>
</Task>
`, escapeXML(user), escapeXML(exe), arguments)
}

var escapeXML = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;").Replace
