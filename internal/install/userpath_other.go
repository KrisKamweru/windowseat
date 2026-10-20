//go:build !windows

package install

import "errors"

func updateUserPath(func(string) string) error {
	return errors.New("the user PATH is only kept in the registry on Windows")
}
