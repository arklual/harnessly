//go:build !darwin && !linux && !windows

package main

import "errors"

func autostartOn() bool { return false }

func setAutostart(bool) error {
	return errors.New("автозапуск на этой системе не поддерживается")
}
