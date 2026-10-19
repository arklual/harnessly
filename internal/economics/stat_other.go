//go:build !(linux || darwin)

package economics

import "os"

func devIno(os.FileInfo) (dev, ino float64) { return 0, 0 }
