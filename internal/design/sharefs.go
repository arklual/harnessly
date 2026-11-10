package design

import (
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// The share filesystem probe (GitHub issue 30). A guest link serves project files to people
// without a session, and the only thing between a guest's path and the rest of the machine is how openPublic opens it:
// one lookup confined beneath the project (os.Root) and one handle that keeps its inode if the path is swapped after the
// lookup. Some sandbox runtimes (gVisor is the one seen) do not keep those guarantees, and the render doctor cannot tell:
// a PNG and an MP4 come out fine there. So before a guest is served, the runtime checks the guarantees themselves on
// the library's own filesystem, and while one fails every guest read and every new link is refused (fail closed).

// Codes of a failed guarantee. They are stable: docs/design-sharing.md lists them.
const (
	ShareFSSetup   = "SHR-100" // the probe could not lay out its files in the library
	ShareFSInside  = "SHR-101" // a file inside the root does not open through it
	ShareFSDotDot  = "SHR-201" // «..» leaves the root
	ShareFSRelLink = "SHR-202" // a relative symlink out of the root is followed
	ShareFSAbsLink = "SHR-203" // an absolute symlink out of the root is followed
	ShareFSDirLink = "SHR-204" // a symlinked directory out of the root is followed
	ShareFSSwap    = "SHR-301" // an open handle reads what was swapped in after it was opened
)

// ShareUnsafeRuntime is the code of a link or a guest read refused because the probe failed.
const ShareUnsafeRuntime = "share_unsafe_runtime"

// ShareRefusedHeader marks a guest read refused by the probe, so the hub tells it from a machine that is down.
const ShareRefusedHeader = "X-Design-Share-Refused"

// ShareFSHint is what the owner is told to do.
const ShareFSHint = "Guest links need a runtime whose filesystem keeps lookups beneath a folder and open handles stable: " +
	"run the runtime on Linux directly, in a VM, or in a container with the default runc or crun runtime. " +
	"Sandboxes that emulate the kernel (gVisor runsc) do not pass this check; see docs/design-sharing.md."

// ShareFSStep is one guarantee of the probe.
type ShareFSStep struct {
	ID     string `json:"id"`
	OK     bool   `json:"ok"`
	Code   string `json:"code,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// ShareFSReport is the result of ProbeShareFS.
type ShareFSReport struct {
	OK    bool          `json:"ok"`
	At    time.Time     `json:"at"`
	Steps []ShareFSStep `json:"steps"`
	Hint  string        `json:"hint,omitempty"`
}

// Failed is the first guarantee that failed, nil when all hold.
func (r ShareFSReport) Failed() *ShareFSStep {
	for i := range r.Steps {
		if !r.Steps[i].OK {
			return &r.Steps[i]
		}
	}
	return nil
}

// openBeneath opens rel beneath dir the way a guest's file is opened. It is a variable so a test can stand in for a
// runtime that does not keep the guarantees; nothing else replaces it.
var openBeneath = func(dir, rel string) (*os.File, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return root.Open(rel)
}

// ProbeShareFS checks the guarantees in a throwaway folder made inside parent (the library, so the same filesystem the
// projects are on) and removes it after. It never weakens a check: a guarantee that cannot be tested is a failure.
func ProbeShareFS(parent string) (rep ShareFSReport) {
	rep = ShareFSReport{At: time.Now()}
	add := func(id, code string, ok bool, detail string) {
		st := ShareFSStep{ID: id, OK: ok}
		if !ok {
			st.Code, st.Detail = code, detail
		}
		rep.Steps = append(rep.Steps, st)
	}
	defer func() {
		rep.OK = rep.Failed() == nil
		if !rep.OK {
			rep.Hint = ShareFSHint
		}
	}()
	if err := os.MkdirAll(parent, 0o700); err != nil {
		add("setup", ShareFSSetup, false, err.Error())
		return rep
	}
	base, err := os.MkdirTemp(parent, ".share-probe-")
	if err != nil {
		add("setup", ShareFSSetup, false, err.Error())
		return rep
	}
	defer os.RemoveAll(base)
	in, out := filepath.Join(base, "root"), filepath.Join(base, "outside")
	secret := filepath.Join(out, "secret.txt")
	err = firstErr(
		os.Mkdir(in, 0o700), os.Mkdir(out, 0o700),
		os.WriteFile(filepath.Join(in, "inside.txt"), []byte("inside"), 0o600),
		os.WriteFile(filepath.Join(in, "next.txt"), []byte("swapped"), 0o600),
		os.WriteFile(secret, []byte("outside"), 0o600),
		os.Symlink(filepath.Join("..", "outside", "secret.txt"), filepath.Join(in, "rel")),
		os.Symlink(secret, filepath.Join(in, "abs")),
		os.Symlink(filepath.Join("..", "outside"), filepath.Join(in, "dir")),
	)
	if err != nil {
		add("setup", ShareFSSetup, false, err.Error())
		return rep
	}

	f, err := openBeneath(in, "inside.txt")
	if err != nil {
		add("inside", ShareFSInside, false, err.Error())
		return rep
	}
	defer f.Close()
	if got := readProbe(f); got != "inside" {
		add("inside", ShareFSInside, false, "read "+quoteProbe(got))
		return rep
	}
	add("inside", ShareFSInside, true, "")

	for _, c := range []struct{ id, code, rel string }{
		{"dotdot", ShareFSDotDot, filepath.Join("..", "outside", "secret.txt")},
		{"relative-symlink", ShareFSRelLink, "rel"},
		{"absolute-symlink", ShareFSAbsLink, "abs"},
		{"directory-symlink", ShareFSDirLink, filepath.Join("dir", "secret.txt")},
	} {
		g, err := openBeneath(in, c.rel)
		if err != nil {
			add(c.id, c.code, true, "")
			continue
		}
		got := readProbe(g)
		g.Close()
		add(c.id, c.code, false, "opened a file outside the folder ("+quoteProbe(got)+")")
	}

	// The handle opened above must keep reading its own file after another one is renamed over the path.
	if err := os.Rename(filepath.Join(in, "next.txt"), filepath.Join(in, "inside.txt")); err != nil {
		add("swap", ShareFSSwap, false, err.Error())
		return rep
	}
	if got := readProbe(f); got != "inside" {
		add("swap", ShareFSSwap, false, "an open handle read "+quoteProbe(got)+" after the path was swapped")
		return rep
	}
	add("swap", ShareFSSwap, true, "")
	return rep
}

func firstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

func readProbe(f *os.File) string {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "error: " + err.Error()
	}
	b, err := io.ReadAll(io.LimitReader(f, 64))
	if err != nil {
		return "error: " + err.Error()
	}
	return string(b)
}

func quoteProbe(s string) string { return "«" + s + "»" }

// shareFSCache keeps the probe's result: an hour when the guarantees hold, a minute when one fails, so a fixed
// runtime is seen again soon and a working one is not probed on every guest request.
type shareFSCache struct {
	mu   sync.Mutex
	rep  *ShareFSReport
	root string
}

const (
	shareFSOKTTL   = time.Hour
	shareFSFailTTL = time.Minute
)

// ShareFS is the probe's result for the library, run on the caller when the cached one is stale (it takes a few
// milliseconds and touches only its own folder).
func (l *Library) ShareFS() ShareFSReport {
	if l.cfg.ShareProbe != nil {
		return l.cfg.ShareProbe(l.Root())
	}
	c := &l.shareFS
	root := l.Root()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.rep != nil && c.root == root {
		ttl := shareFSOKTTL
		if !c.rep.OK {
			ttl = shareFSFailTTL
		}
		if time.Since(c.rep.At) < ttl {
			return *c.rep
		}
	}
	if root == "" {
		rep := ShareFSReport{At: time.Now(), Steps: []ShareFSStep{{ID: "setup", Code: ShareFSSetup, Detail: "no library folder"}}, Hint: ShareFSHint}
		c.rep, c.root = &rep, root
		return rep
	}
	rep := ProbeShareFS(root)
	c.rep, c.root = &rep, root
	return rep
}
