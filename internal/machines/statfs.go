package machines

import "syscall"

func statfs(path string) (blocks, bfree, bavail float64, err error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, 0, err
	}
	return float64(st.Blocks), float64(st.Bfree), float64(st.Bavail), nil
}
