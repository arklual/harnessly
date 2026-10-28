//go:build !windows

package delivery

import "syscall"

const noFollow = syscall.O_NOFOLLOW
