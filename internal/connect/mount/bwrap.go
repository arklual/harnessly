package mount

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// BwrapArgs are the bubblewrap arguments that show src at dst (src "" leaves an empty folder there, for a mount made
// inside) in a view of this machine where everything else stays
// where it is: dst need not exist here (/Users/masha/proj on a Linux server), and its folders that are missing are
// made on a tmpfs that keeps the real folder's other entries.
func BwrapArgs(src, dst string) []string {
	dst = filepath.Clean(dst)
	args := []string{"--die-with-parent"}
	args = append(args, layer("/")...)
	parts := strings.Split(strings.TrimPrefix(dst, "/"), "/")
	cur := "/"
	for i, p := range parts[:len(parts)-1] {
		next := filepath.Join(cur, p)
		if fi, err := os.Stat(next); err == nil && fi.IsDir() {
			// The next folder on the way is missing in this real one: it becomes a tmpfs with its entries.
			if _, err := os.Stat(filepath.Join(next, parts[i+1])); err != nil {
				args = append(args, layer(next)...)
			}
			cur = next
			continue
		}
		args = append(args, "--dir", next)
		cur = next
	}
	if src == "" {
		return append(args, "--dir", dst)
	}
	return append(args, "--bind", src, dst)
}

// layer makes dir a tmpfs and puts its real entries back on it.
func layer(dir string) []string {
	args := []string{"--tmpfs", dir}
	ents, _ := os.ReadDir(dir)
	names := make([]string, 0, len(ents))
	for _, e := range ents {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for _, n := range names {
		p := filepath.Join(dir, n)
		switch {
		case dir == "/" && n == "proc":
			args = append(args, "--proc", "/proc")
		case dir == "/" && n == "dev":
			args = append(args, "--dev-bind", "/dev", "/dev")
		default:
			if t, err := os.Readlink(p); err == nil {
				args = append(args, "--symlink", t, p)
			} else {
				args = append(args, "--dev-bind-try", p, p)
			}
		}
	}
	return args
}
