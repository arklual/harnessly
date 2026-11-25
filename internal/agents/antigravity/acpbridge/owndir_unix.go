//go:build unix

package acpbridge

import (
	"fmt"
	"os"
	"syscall"
)

// ownDir: the folder of the sockets is a real folder of this user that nobody else can enter (it may lie in a shared
// temporary folder).
func ownDir(dir string) error {
	st, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !st.IsDir() || !ok || int(sys.Uid) != os.Getuid() || st.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%s is not a private folder of this user", dir)
	}
	return nil
}
