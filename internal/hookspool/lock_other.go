//go:build !unix

package hookspool

import (
	"os"
	"time"
)

// Without file locks an event appended while the runtime reads the spool can be lost; the hook's own delivery still
// carries it.
func lock(*os.File, time.Duration) error { return nil }

func unlock(*os.File) {}
