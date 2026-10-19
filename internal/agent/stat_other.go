//go:build !(linux || darwin)

package agent

import "os"

func fileID(info os.FileInfo) (inode uint64, mtimeMs float64) {
	t := info.ModTime()
	return 0, float64(t.Unix())*1000 + float64(t.Nanosecond())/1e6
}
