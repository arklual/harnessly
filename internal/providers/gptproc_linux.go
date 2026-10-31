package providers

import "syscall"

// proxyProcAttr ends the proxy with the runtime: a runtime that dies without stopping it takes it along, so no proxy
// is left holding the port with a relay secret nobody knows.
func proxyProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
}
