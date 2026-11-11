package proxy

import "time"

// SetStale shortens how long a silent machine is answered for and how long a list with a copy waits; the returned func
// puts both back.
func SetStale(silent, wait time.Duration) (restore func()) {
	os, ow := silentFor, staleListWait
	silentFor, staleListWait = silent, wait
	return func() { silentFor, staleListWait = os, ow }
}
