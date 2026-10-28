//go:build !linux && !darwin

package preflight

import "errors"

func diskUsage(string) (total, free int64, err error) { return 0, 0, errors.New("not supported here") }

func osName(string) string { return "" }

func memory(string, string) (total, available int64) { return 0, 0 }
