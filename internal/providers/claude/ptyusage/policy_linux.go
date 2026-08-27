//go:build linux

package ptyusage

func checkPlatformManagedPolicy() error {
	return nil
}

func managedPolicyPaths() []string {
	return []string{
		"/etc/claude-code/managed-settings.json",
		"/etc/claude-code/managed-settings.d",
		"/etc/claude-code/managed-mcp.json",
	}
}
