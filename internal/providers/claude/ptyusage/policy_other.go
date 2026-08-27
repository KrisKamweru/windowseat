//go:build !linux && !windows

package ptyusage

func checkPlatformManagedPolicy() error {
	return nil
}

func managedPolicyPaths() []string {
	return nil
}
