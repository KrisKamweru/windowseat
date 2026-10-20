package install

import (
	"path/filepath"
	"strings"
	"testing"
)

const installDir = `C:\Users\k\AppData\Local\Programs\Windowseat`

func TestPathWithAddsDirOnce(t *testing.T) {
	cases := []struct{ name, path, want string }{
		{"empty", "", installDir},
		{"appends", `C:\bin;%USERPROFILE%\go\bin`, `C:\bin;%USERPROFILE%\go\bin;` + installDir},
		{"trailing separator", `C:\bin;`, `C:\bin;` + installDir},
		{"already there", `C:\bin;` + installDir, `C:\bin;` + installDir},
		{"different case", strings.ToLower(installDir) + `;C:\bin`, strings.ToLower(installDir) + `;C:\bin`},
		{"trailing backslash", installDir + `\;C:\bin`, installDir + `\;C:\bin`},
	}
	for _, c := range cases {
		if got := pathWith(c.path, installDir); got != c.want {
			t.Errorf("%s: pathWith(%q) = %q, want %q", c.name, c.path, got, c.want)
		}
	}
}

func TestPathWithoutRemovesEveryCopyOfDir(t *testing.T) {
	cases := []struct{ name, path, want string }{
		{"empty", "", ""},
		{"absent", `C:\bin;D:\tools`, `C:\bin;D:\tools`},
		{"only entry", installDir, ""},
		{"middle", `C:\bin;` + installDir + `;D:\tools`, `C:\bin;D:\tools`},
		{"case and trailing backslash", `C:\bin;` + strings.ToUpper(installDir) + `\;` + installDir, `C:\bin`},
		{"keeps lookalikes", installDir + `2;C:\bin`, installDir + `2;C:\bin`},
	}
	for _, c := range cases {
		if got := pathWithout(c.path, installDir); got != c.want {
			t.Errorf("%s: pathWithout(%q) = %q, want %q", c.name, c.path, got, c.want)
		}
	}
}

func TestWindowsInstallAddsToPathOnlyWhenAsked(t *testing.T) {
	env := windowsEnv()
	dir := filepath.Join(env.LocalAppData, "Programs", "Windowseat")

	plan, err := InstallPlan(env, Options{AddToPath: true})
	if err != nil {
		t.Fatal(err)
	}
	findStep(t, plan, func(s addToUserPath) bool { return s.dir == dir })

	plan, err = InstallPlan(env, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range plan.Steps {
		if _, ok := step.(addToUserPath); ok {
			t.Errorf("plan changes PATH without --add-to-path: %s", step)
		}
	}
	if len(plan.Notes) == 0 || !strings.Contains(plan.Notes[0], "--add-to-path") {
		t.Errorf("notes = %q, want a hint about --add-to-path", plan.Notes)
	}
}

func TestAddToPathIsWindowsOnly(t *testing.T) {
	for _, env := range []Env{linuxEnv(), darwinEnv()} {
		_, err := InstallPlan(env, Options{AddToPath: true})
		if err == nil || !strings.Contains(err.Error(), "Windows") {
			t.Errorf("%s: err = %v, want a Windows-only error", env.OS, err)
		}
	}
}

func TestWindowsUninstallRemovesDirFromPath(t *testing.T) {
	env := windowsEnv()
	plan, err := UninstallPlan(env)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(env.LocalAppData, "Programs", "Windowseat")
	findStep(t, plan, func(s removeFromUserPath) bool { return s.dir == dir })
}
