package connect

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// maxRead bounds one read: the kernel asks for 128 KiB at most anyway.
const maxRead = 1 << 20

func toStat(fi fs.FileInfo) *Stat {
	st := &Stat{Size: fi.Size(), Mtime: fi.ModTime().UnixNano(), Atime: fi.ModTime().UnixNano()}
	m := fi.Mode()
	perm := uint32(m.Perm())
	switch {
	case m&fs.ModeSymlink != 0:
		st.Mode = SIFLNK | 0o777
	case m.IsDir():
		st.Mode = SIFDIR | perm
	default:
		st.Mode = SIFREG | perm
	}
	fixPerm(st, fi)
	return st
}

// errno names an error the POSIX way, whatever the device's system.
func errno(err error) string {
	if err == nil {
		return ""
	}
	var en syscall.Errno
	if errors.As(err, &en) {
		if name := errnoName(en); name != "" {
			return name
		}
	}
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "ENOENT"
	case errors.Is(err, fs.ErrExist):
		return "EEXIST"
	case errors.Is(err, fs.ErrPermission):
		return "EACCES"
	}
	return "EIO"
}

// fsOp runs one file operation on this device.
func (d *Device) fsOp(r *FSReq) *FSResp {
	p := fromShellPath(r.Path)
	resp := &FSResp{}
	var err error
	switch r.Op {
	case "stat", "lstat":
		var fi fs.FileInfo
		if r.Op == "stat" {
			fi, err = os.Stat(p)
		} else {
			fi, err = os.Lstat(p)
		}
		if err == nil {
			resp.Stat = toStat(fi)
		}
	case "readdir":
		var ents []fs.DirEntry
		ents, err = os.ReadDir(p)
		for _, e := range ents {
			var mode uint32 = SIFREG
			switch {
			case e.Type()&fs.ModeSymlink != 0:
				mode = SIFLNK
			case e.IsDir():
				mode = SIFDIR
			}
			resp.Entries = append(resp.Entries, Dirent{Name: e.Name(), Mode: mode})
		}
	case "read":
		var f *os.File
		if f, err = os.Open(p); err == nil {
			buf := make([]byte, min(max(r.Size, 0), maxRead))
			var n int
			n, err = f.ReadAt(buf, r.Off)
			if errors.Is(err, io.EOF) {
				err = nil
			}
			resp.Data = buf[:n]
			f.Close()
		}
	case "write":
		var f *os.File
		if f, err = os.OpenFile(p, os.O_WRONLY, 0); err == nil {
			resp.N, err = f.WriteAt(r.Data, r.Off)
			if cerr := f.Close(); err == nil {
				err = cerr
			}
		}
	case "create":
		flags := os.O_WRONLY | os.O_CREATE
		if r.Excl {
			flags |= os.O_EXCL
		}
		var f *os.File
		if f, err = os.OpenFile(p, flags, fs.FileMode(r.Mode&0o777)); err == nil {
			f.Close()
		}
	case "mkdir":
		err = os.Mkdir(p, fs.FileMode(r.Mode&0o777))
	case "unlink":
		var fi fs.FileInfo
		if fi, err = os.Lstat(p); err == nil {
			if fi.IsDir() {
				err = syscall.EISDIR
			} else {
				err = os.Remove(p)
			}
		}
	case "rmdir":
		err = removeDir(p)
	case "rename":
		err = os.Rename(p, fromShellPath(r.To))
	case "truncate":
		err = os.Truncate(p, r.Off)
	case "chmod":
		err = os.Chmod(p, fs.FileMode(r.Mode&0o777))
	case "utimens":
		var fi fs.FileInfo
		if fi, err = os.Stat(p); err == nil {
			mt, at := fi.ModTime(), fi.ModTime()
			if r.Mtime != 0 {
				mt = time.Unix(0, r.Mtime)
			}
			if r.Atime != 0 {
				at = time.Unix(0, r.Atime)
			}
			err = os.Chtimes(p, at, mt)
		}
	case "readlink":
		resp.Target, err = os.Readlink(p)
		resp.Target = filepath.ToSlash(resp.Target)
	case "symlink":
		err = os.Symlink(r.To, p)
	default:
		resp.Errno = "ENOSYS"
		return resp
	}
	resp.Errno = errno(err)
	return resp
}

func removeDir(p string) error {
	fi, err := os.Lstat(p)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return syscall.ENOTDIR
	}
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	names, _ := f.Readdirnames(1)
	f.Close()
	if len(names) > 0 {
		return syscall.ENOTEMPTY
	}
	return os.Remove(p)
}
