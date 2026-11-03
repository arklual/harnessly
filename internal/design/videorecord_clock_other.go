//go:build !linux

package design

import "errors"

func monotonicMs() (float64, error) {
	return 0, errors.New("the interface recording runs on Linux only: chrome-headless-shell steps frames on its own clock there")
}
