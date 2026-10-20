package install

import (
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"runtime"

	"windowseat/internal/ipc"
	"windowseat/internal/service"
)

// Current reads the Env of this machine and the running binary.
func Current() (Env, error) {
	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	if err != nil {
		return Env{}, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Env{}, err
	}
	account, err := user.Current()
	if err != nil {
		return Env{}, err
	}
	statePath, err := service.DefaultStatePath()
	if err != nil {
		return Env{}, err
	}

	env := Env{
		OS:           runtime.GOOS,
		Home:         home,
		ConfigHome:   os.Getenv("XDG_CONFIG_HOME"),
		LocalAppData: os.Getenv("LOCALAPPDATA"),
		TempDir:      os.TempDir(),
		SourceDir:    filepath.Dir(exe),
		Executable:   exe,
		User:         account.Username,
		UID:          os.Getuid(),
		HasDisplay:   os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != "",
		Path:         os.Getenv("PATH"),
		StateDir:     filepath.Dir(statePath),
	}
	if env.ConfigHome == "" {
		env.ConfigHome = filepath.Join(home, ".config")
	}
	if env.OS == "windows" && env.LocalAppData == "" {
		return Env{}, errors.New("LOCALAPPDATA is not set")
	}
	_, err = os.Stat(filepath.Join(env.SourceDir, env.exe("windowseat-tray")))
	env.TrayBuilt = err == nil

	if env.OS != "windows" {
		socket, err := ipc.DefaultAddress()
		if err != nil {
			return Env{}, err
		}
		env.SocketDir = filepath.Dir(socket)
	}
	return env, nil
}
