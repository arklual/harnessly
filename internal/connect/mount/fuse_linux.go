package mount

import (
	"path"
	"syscall"
	"time"

	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/hanwen/go-fuse/v2/fuse/nodefs"
	"github.com/hanwen/go-fuse/v2/fuse/pathfs"

	"github.com/ly-solutions/harnessly/internal/connect"
)

var errnos = map[string]fuse.Status{
	"ENOENT": fuse.ENOENT, "EEXIST": fuse.Status(syscall.EEXIST), "EACCES": fuse.EACCES, "ENOTDIR": fuse.ENOTDIR,
	"EISDIR": fuse.Status(syscall.EISDIR), "ENOTEMPTY": fuse.Status(syscall.ENOTEMPTY), "EINVAL": fuse.EINVAL,
	"EXDEV": fuse.EXDEV, "ENOSPC": fuse.Status(syscall.ENOSPC), "EROFS": fuse.EROFS, "ELOOP": fuse.Status(syscall.ELOOP),
	"ENAMETOOLONG": fuse.Status(syscall.ENAMETOOLONG), "ENOSYS": fuse.ENOSYS, "ENOTCONN": fuse.Status(syscall.ENOTCONN),
	"ETIMEDOUT": fuse.Status(syscall.ETIMEDOUT), "EINTR": fuse.EINTR,
}

func status(r *connect.FSResp) fuse.Status {
	if r.Errno == "" {
		return fuse.OK
	}
	if s, ok := errnos[r.Errno]; ok {
		return s
	}
	return fuse.EIO
}

// FS is the device's folder Root as a pathfs file system.
type FS struct {
	pathfs.FileSystem
	C    *Client
	Root string // the folder on the device, its shell's form
	UID  uint32
	GID  uint32
}

// New makes the file system of a device's folder.
func New(c *Client, root string) *FS {
	return &FS{FileSystem: pathfs.NewDefaultFileSystem(), C: c, Root: root,
		UID: uint32(syscall.Getuid()), GID: uint32(syscall.Getgid())}
}

func (f *FS) p(name string) string { return path.Join(f.Root, name) }

func (f *FS) do(req connect.FSReq) *connect.FSResp { return f.C.Do(&req) }

func (f *FS) attr(st *connect.Stat) *fuse.Attr {
	a := &fuse.Attr{Mode: st.Mode, Size: uint64(st.Size), Nlink: 1}
	a.SetTimes(ptime(st.Atime), ptime(st.Mtime), ptime(st.Mtime))
	a.Owner = fuse.Owner{Uid: f.UID, Gid: f.GID}
	a.Blocks = (a.Size + 511) / 512
	return a
}

func ptime(ns int64) *time.Time { t := time.Unix(0, ns); return &t }

func (f *FS) String() string { return "harnessly-connect:" + f.C.Device + ":" + f.Root }

func (f *FS) GetAttr(name string, _ *fuse.Context) (*fuse.Attr, fuse.Status) {
	r := f.do(connect.FSReq{Op: "lstat", Path: f.p(name)})
	if s := status(r); !s.Ok() {
		return nil, s
	}
	return f.attr(r.Stat), fuse.OK
}

func (f *FS) OpenDir(name string, _ *fuse.Context) ([]fuse.DirEntry, fuse.Status) {
	r := f.do(connect.FSReq{Op: "readdir", Path: f.p(name)})
	if s := status(r); !s.Ok() {
		return nil, s
	}
	out := make([]fuse.DirEntry, 0, len(r.Entries))
	for _, e := range r.Entries {
		out = append(out, fuse.DirEntry{Name: e.Name, Mode: e.Mode})
	}
	return out, fuse.OK
}

func (f *FS) Open(name string, flags uint32, _ *fuse.Context) (nodefs.File, fuse.Status) {
	if flags&syscall.O_TRUNC != 0 {
		if s := status(f.do(connect.FSReq{Op: "truncate", Path: f.p(name)})); !s.Ok() {
			return nil, s
		}
	}
	return &file{File: nodefs.NewDefaultFile(), fs: f, path: f.p(name)}, fuse.OK
}

func (f *FS) Create(name string, flags uint32, mode uint32, _ *fuse.Context) (nodefs.File, fuse.Status) {
	r := f.do(connect.FSReq{Op: "create", Path: f.p(name), Mode: mode, Excl: flags&syscall.O_EXCL != 0})
	if s := status(r); !s.Ok() {
		return nil, s
	}
	if flags&syscall.O_TRUNC != 0 {
		f.do(connect.FSReq{Op: "truncate", Path: f.p(name)})
	}
	return &file{File: nodefs.NewDefaultFile(), fs: f, path: f.p(name)}, fuse.OK
}

func (f *FS) Mkdir(name string, mode uint32, _ *fuse.Context) fuse.Status {
	return status(f.do(connect.FSReq{Op: "mkdir", Path: f.p(name), Mode: mode}))
}

func (f *FS) Unlink(name string, _ *fuse.Context) fuse.Status {
	return status(f.do(connect.FSReq{Op: "unlink", Path: f.p(name)}))
}

func (f *FS) Rmdir(name string, _ *fuse.Context) fuse.Status {
	return status(f.do(connect.FSReq{Op: "rmdir", Path: f.p(name)}))
}

func (f *FS) Rename(from, to string, _ *fuse.Context) fuse.Status {
	return status(f.do(connect.FSReq{Op: "rename", Path: f.p(from), To: f.p(to)}))
}

func (f *FS) Truncate(name string, size uint64, _ *fuse.Context) fuse.Status {
	return status(f.do(connect.FSReq{Op: "truncate", Path: f.p(name), Off: int64(size)}))
}

func (f *FS) Chmod(name string, mode uint32, _ *fuse.Context) fuse.Status {
	return status(f.do(connect.FSReq{Op: "chmod", Path: f.p(name), Mode: mode}))
}

// Chown: the device's files belong to the person; ownership is not theirs to change from here.
func (f *FS) Chown(string, uint32, uint32, *fuse.Context) fuse.Status { return fuse.OK }

func (f *FS) Utimens(name string, at, mt *time.Time, _ *fuse.Context) fuse.Status {
	req := connect.FSReq{Op: "utimens", Path: f.p(name)}
	if at != nil {
		req.Atime = at.UnixNano()
	}
	if mt != nil {
		req.Mtime = mt.UnixNano()
	}
	return status(f.do(req))
}

func (f *FS) Access(string, uint32, *fuse.Context) fuse.Status { return fuse.OK }

func (f *FS) Readlink(name string, _ *fuse.Context) (string, fuse.Status) {
	r := f.do(connect.FSReq{Op: "readlink", Path: f.p(name)})
	return r.Target, status(r)
}

func (f *FS) Symlink(value, link string, _ *fuse.Context) fuse.Status {
	return status(f.do(connect.FSReq{Op: "symlink", Path: f.p(link), To: value}))
}

func (f *FS) StatFs(string) *fuse.StatfsOut {
	return &fuse.StatfsOut{Blocks: 1 << 30, Bfree: 1 << 29, Bavail: 1 << 29, Files: 1 << 20, Ffree: 1 << 19, Bsize: 4096, NameLen: 255}
}

type file struct {
	nodefs.File
	fs   *FS
	path string
}

func (fl *file) Read(dest []byte, off int64) (fuse.ReadResult, fuse.Status) {
	r := fl.fs.do(connect.FSReq{Op: "read", Path: fl.path, Off: off, Size: len(dest)})
	if s := status(r); !s.Ok() {
		return nil, s
	}
	return fuse.ReadResultData(r.Data), fuse.OK
}

func (fl *file) Write(data []byte, off int64) (uint32, fuse.Status) {
	r := fl.fs.do(connect.FSReq{Op: "write", Path: fl.path, Off: off, Data: data})
	return uint32(r.N), status(r)
}

func (fl *file) Truncate(size uint64) fuse.Status {
	return status(fl.fs.do(connect.FSReq{Op: "truncate", Path: fl.path, Off: int64(size)}))
}

func (fl *file) GetAttr(out *fuse.Attr) fuse.Status {
	r := fl.fs.do(connect.FSReq{Op: "stat", Path: fl.path})
	if s := status(r); !s.Ok() {
		return s
	}
	*out = *fl.fs.attr(r.Stat)
	return fuse.OK
}

func (fl *file) Flush() fuse.Status    { return fuse.OK }
func (fl *file) Fsync(int) fuse.Status { return fuse.OK }
func (fl *file) Chmod(mode uint32) fuse.Status {
	return status(fl.fs.do(connect.FSReq{Op: "chmod", Path: fl.path, Mode: mode}))
}
func (fl *file) Utimens(at, mt *time.Time) fuse.Status {
	req := connect.FSReq{Op: "utimens", Path: fl.path}
	if at != nil {
		req.Atime = at.UnixNano()
	}
	if mt != nil {
		req.Mtime = mt.UnixNano()
	}
	return status(fl.fs.do(req))
}

// Mount mounts the device's folder at dir; Serve runs until it is unmounted. Direct calls mount(2) itself: inside a
// user namespace with CAP_SYS_ADMIN, where the setuid fusermount does not work (the runtime runs NoNewPrivileges).
func Mount(c *Client, root, dir string, direct bool) (*fuse.Server, error) {
	nfs := pathfs.NewPathNodeFs(New(c, root), &pathfs.PathNodeFsOptions{ClientInodes: false})
	opts := &nodefs.Options{EntryTimeout: time.Second, AttrTimeout: time.Second, NegativeTimeout: 500 * time.Millisecond}
	conn := nodefs.NewFileSystemConnector(nfs.Root(), opts)
	srv, err := fuse.NewServer(conn.RawFS(), dir, &fuse.MountOptions{
		FsName: "harnessly-connect", Name: "connect", MaxWrite: 1 << 20, DisableXAttrs: true, DirectMountStrict: direct,
	})
	if err != nil {
		return nil, err
	}
	go srv.Serve()
	if err := srv.WaitMount(); err != nil {
		_ = srv.Unmount()
		return nil, err
	}
	return srv, nil
}
