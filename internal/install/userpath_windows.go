//go:build windows

package install

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var sendMessageTimeout = windows.NewLazySystemDLL("user32.dll").NewProc("SendMessageTimeoutW")

// updateUserPath rewrites HKCU\Environment\Path through change, keeping its
// registry type so %VARIABLES% in it still expand, then tells running
// programs such as Explorer so new terminals pick it up.
func updateUserPath(change func(string) string) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, "Environment", registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()

	path, kind, err := key.GetStringValue("Path")
	if errors.Is(err, registry.ErrNotExist) {
		path, kind, err = "", registry.EXPAND_SZ, nil
	}
	if err != nil {
		return err
	}
	updated := change(path)
	if updated == path {
		return nil
	}
	if kind == registry.SZ {
		err = key.SetStringValue("Path", updated)
	} else {
		err = key.SetExpandStringValue("Path", updated)
	}
	if err != nil {
		return err
	}
	announceEnvironmentChange()
	return nil
}

func announceEnvironmentChange() {
	const (
		hwndBroadcast   = 0xFFFF
		wmSettingChange = 0x001A
		smtoAbortIfHung = 0x0002
		timeoutMillis   = 5000
	)
	environment, _ := windows.UTF16PtrFromString("Environment")
	var result uintptr
	sendMessageTimeout.Call(hwndBroadcast, wmSettingChange, 0, uintptr(unsafe.Pointer(environment)), smtoAbortIfHung, timeoutMillis, uintptr(unsafe.Pointer(&result)))
}
