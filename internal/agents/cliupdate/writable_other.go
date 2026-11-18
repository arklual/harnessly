//go:build !unix

package cliupdate

func writable(string) bool { return false }
