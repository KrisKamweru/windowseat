//go:build windows

package ptyusage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const managedPolicyRegistryPath = `SOFTWARE\Policies\ClaudeCode`

func checkPlatformManagedPolicy() error {
	for _, root := range []registry.Key{registry.LOCAL_MACHINE, registry.CURRENT_USER} {
		key, err := registry.OpenKey(root, managedPolicyRegistryPath, registry.QUERY_VALUE)
		if errors.Is(err, registry.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("managed policy registry key cannot be determined")
		}
		names, readErr := key.ReadValueNames(-1)
		closeErr := key.Close()
		if readErr != nil || closeErr != nil {
			return fmt.Errorf("managed policy registry values cannot be determined")
		}
		for _, name := range names {
			if strings.EqualFold(name, "Settings") {
				return fmt.Errorf("managed policy registry value is present")
			}
		}
	}
	return nil
}

func managedPolicyPaths() []string {
	programFiles := os.Getenv("ProgramFiles")
	if programFiles == "" {
		programFiles = `C:\Program Files`
	}
	root := filepath.Join(programFiles, "ClaudeCode")
	return []string{
		filepath.Join(root, "managed-settings.json"),
		filepath.Join(root, "managed-settings.d"),
		filepath.Join(root, "managed-mcp.json"),
	}
}
