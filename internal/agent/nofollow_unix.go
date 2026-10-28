//go:build linux || darwin

package agent

import "syscall"

// noFollow makes an open refuse a link as the last element of the path: a file the agent writes in a folder the runtime
// can also write is not to be redirected by a link put there.
const noFollow = syscall.O_NOFOLLOW
