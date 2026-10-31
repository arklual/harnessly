//go:build !linux

package providers

import "syscall"

// proxyProcAttr has nothing to add where a child cannot be tied to its parent's end: a proxy a dead runtime left is
// stopped by the next one (GPTProxy.killStale).
func proxyProcAttr() *syscall.SysProcAttr { return nil }
