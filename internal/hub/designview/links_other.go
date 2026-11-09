//go:build !unix

package designview

import "os"

// linkCount is unknown here: nothing is pruned, the revisions' folders go and the kept contents stay.
func linkCount(os.FileInfo) uint64 { return 0 }
