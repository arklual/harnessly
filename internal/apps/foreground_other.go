//go:build !linux

package apps

import "errors"

// ForegroundExec needs Linux: the wall it runs in is a Linux mount namespace.
func ForegroundExec(argv []string) error {
	return errors.New("sandbox-exec needs Linux")
}
