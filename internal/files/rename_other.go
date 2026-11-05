//go:build !linux

package files

func renameNoReplace(from, to string) error { return renameChecked(from, to) }
