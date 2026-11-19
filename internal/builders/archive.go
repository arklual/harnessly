// Package builders is the client of the hub's pool of builder machines (docs/hub/builders.md): it lists the
// builders, hands the hub a heavy job with the files it needs, follows it, takes the result files back, and when the pool
// has no online builder runs the job on this machine's own heavy slots with draft limits. It talks to the machine agent
// on the loopback (the agent's /jobs routes), which signs for the hub.
package builders

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// skipDirs are never packed: they are big, rebuilt on the builder, or no business of a render.
var skipDirs = map[string]bool{".git": true, "node_modules": true, "__pycache__": true}

// Pack writes dir as a tar.gz of regular files and folders (links are left out). It returns the number of files.
func Pack(dir string, w io.Writer) (int, error) {
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	n := 0
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil || rel == "." {
			return err
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return tw.WriteHeader(&tar.Header{Name: filepath.ToSlash(rel) + "/", Typeflag: tar.TypeDir, Mode: 0o755})
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if err := tw.WriteHeader(&tar.Header{Name: filepath.ToSlash(rel), Size: info.Size(), Mode: int64(info.Mode().Perm()), ModTime: info.ModTime()}); err != nil {
			return err
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		_, err = io.Copy(tw, f)
		f.Close()
		n++
		return err
	})
	if err != nil {
		return n, err
	}
	if err := tw.Close(); err != nil {
		return n, err
	}
	return n, gz.Close()
}

// MaxUnpacked bounds what an archive may unpack to.
const MaxUnpacked = 8 << 30

// Unpack reads a tar.gz into dir. Names that leave dir, links and special files are refused.
func Unpack(r io.Reader, dir string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	var total int64
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		name := path.Clean(strings.TrimPrefix(h.Name, "./"))
		if name == "." {
			continue
		}
		if path.IsAbs(name) || name == ".." || strings.HasPrefix(name, "../") {
			return fmt.Errorf("the archive has a path outside its folder: %q", h.Name)
		}
		dst := filepath.Join(dir, filepath.FromSlash(name))
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(dst, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if total += h.Size; total > MaxUnpacked {
				return errors.New("the archive unpacks to too much")
			}
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(h.Mode&0o777)|0o600)
			if err != nil {
				return err
			}
			_, err = io.Copy(f, io.LimitReader(tr, h.Size))
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return err
			}
		default:
			return fmt.Errorf("the archive has a %q that is neither a file nor a folder", h.Name)
		}
	}
}

// Collect lists the files under dir that the globs match (a glob matching a folder takes the files in it), as slash paths
// relative to dir. A glob that leaves dir matches nothing.
func Collect(dir string, globs []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	add := func(rel string) {
		if !seen[rel] {
			seen[rel] = true
			out = append(out, rel)
		}
	}
	for _, g := range globs {
		g = path.Clean(g)
		if path.IsAbs(g) || g == ".." || strings.HasPrefix(g, "../") {
			continue
		}
		matches, err := filepath.Glob(filepath.Join(dir, filepath.FromSlash(g)))
		if err != nil {
			return nil, fmt.Errorf("bad mask %q: %w", g, err)
		}
		for _, m := range matches {
			st, err := os.Lstat(m)
			if err != nil {
				continue
			}
			switch {
			case st.Mode().IsRegular():
				if rel, err := filepath.Rel(dir, m); err == nil {
					add(filepath.ToSlash(rel))
				}
			case st.IsDir():
				_ = filepath.WalkDir(m, func(p string, d fs.DirEntry, err error) error {
					if err == nil && d.Type().IsRegular() {
						if rel, err := filepath.Rel(dir, p); err == nil {
							add(filepath.ToSlash(rel))
						}
					}
					return nil
				})
			}
		}
	}
	return out, nil
}
