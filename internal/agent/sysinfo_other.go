//go:build !linux && !darwin

package agent

import "errors"

// errNoReader is what a reading this system cannot do answers.
var errNoReader = errors.New("this system is not read")

type noReader struct{}

func newSysReader() sysReader { return noReader{} }

func (noReader) CPUTimes() (uint64, uint64, error)   { return 0, 0, errNoReader }
func (noReader) Memory() (uint64, uint64, error)     { return 0, 0, errNoReader }
func (noReader) Disk(string) (uint64, uint64, error) { return 0, 0, errNoReader }
func (noReader) Hardware(string) hwFacts             { return hwFacts{} }
