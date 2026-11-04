package agentbrowser

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// ImportChrome copies a stopped Chrome user-data directory to a new or empty
// destination (the managed profile's chrome directory). The source is untouched.
// It cannot decrypt cookies protected by another system keyring or OS account.
func ImportChrome(ctx context.Context, source, destination string) (warning string, err error) {
	if err = ctx.Err(); err != nil {
		return "", err
	}
	for _, path := range []string{source, destination} {
		if err = importPath(path); err != nil {
			return "", err
		}
	}
	source, err = filepath.Abs(source)
	if err != nil {
		return "", err
	}
	destination, err = filepath.Abs(destination)
	if err != nil {
		return "", err
	}
	if withinPath(source, destination) || withinPath(destination, source) {
		return "", errors.New("source and destination Chrome folders must not overlap")
	}
	root, err := os.OpenRoot(source)
	if err != nil {
		return "", err
	}
	defer root.Close()
	if err = stoppedChrome(root); err != nil {
		return "", err
	}
	if entries, e := os.ReadDir(destination); e == nil {
		if len(entries) != 0 {
			return "", errors.New("Chrome import destination must be empty")
		}
	} else if !os.IsNotExist(e) {
		return "", e
	}
	parent := filepath.Dir(destination)
	if err = os.MkdirAll(parent, 0700); err != nil {
		return "", err
	}
	staging, err := os.MkdirTemp(parent, ".chrome-import-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(staging)
	err = fs.WalkDir(root.FS(), ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if strings.HasPrefix(entry.Name(), "Singleton") {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("Chrome import refuses symlink %q", path)
		}
		out := filepath.Join(staging, filepath.FromSlash(path))
		if info.IsDir() {
			return os.MkdirAll(out, 0700)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("Chrome import refuses special file %q", path)
		}
		return copyChromeFile(ctx, root, path, out, info)
	})
	if err != nil {
		return "", err
	}
	if err = stoppedChrome(root); err != nil {
		return "", err
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	if err = os.Rename(staging, destination); err != nil {
		return "", err
	}
	return "Chrome folder copied. Cookies encrypted with a system keyring, another OS account, or another machine may not work; sign in again if needed. Cookies stored with --password-store=basic can be reused.", nil
}

func importPath(path string) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("Chrome import path is required")
	}
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if part == ".." {
			return errors.New("Chrome import refuses parent traversal")
		}
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	for current := abs; ; current = filepath.Dir(current) {
		st, err := os.Lstat(current)
		if err == nil && st.Mode()&os.ModeSymlink != 0 {
			return errors.New("Chrome import refuses symlinks in paths")
		}
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if current == filepath.Dir(current) {
			return nil
		}
	}
}

func withinPath(parent, path string) bool {
	rel, err := filepath.Rel(parent, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func stoppedChrome(root *os.Root) error {
	st, err := root.Lstat("SingletonLock")
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if st.Mode()&os.ModeSymlink == 0 {
		return errors.New("Chrome SingletonLock cannot be verified; close Chrome and remove the stale lock")
	}
	value, err := root.Readlink("SingletonLock")
	if err != nil {
		return err
	}
	hostname, err := os.Hostname()
	if err != nil {
		return err
	}
	prefix := hostname + "-"
	if !strings.HasPrefix(value, prefix) {
		return errors.New("Chrome SingletonLock belongs to another host; verify Chrome is stopped before importing")
	}
	pid, err := strconv.Atoi(strings.TrimPrefix(value, prefix))
	if err != nil || pid <= 0 {
		return errors.New("invalid Chrome SingletonLock")
	}
	if err = syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		return errors.New("Chrome is running; stop it before importing its folder")
	}
	return nil
}

func copyChromeFile(ctx context.Context, root *os.Root, path, out string, expected fs.FileInfo) error {
	in, err := root.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer in.Close()
	actual, err := in.Stat()
	if err != nil {
		return err
	}
	if !actual.Mode().IsRegular() || !os.SameFile(expected, actual) {
		return errors.New("Chrome import source changed while copying")
	}
	f, err := os.OpenFile(out, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, importReader{ctx: ctx, reader: in})
	if err == nil {
		err = f.Sync()
	}
	return errors.Join(err, f.Close())
}

type importReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r importReader) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(b)
}
