package ptyusage

import (
	"errors"
	"fmt"
	"os"
)

func CheckManagedPolicyFilesAbsent() error {
	if err := checkPlatformManagedPolicy(); err != nil {
		return err
	}
	paths := managedPolicyPaths()
	if len(paths) == 0 {
		return fmt.Errorf("managed policy locations are unsupported on this platform")
	}
	for _, path := range paths {
		_, err := os.Lstat(path)
		if err == nil {
			return fmt.Errorf("managed policy path is present")
		}
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("managed policy path cannot be determined")
		}
	}
	return nil
}
