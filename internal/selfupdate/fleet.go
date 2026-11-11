package selfupdate

// Fleet mode: a runtime release that is rolled out to a machine from outside (a pipeline, an operator over ssh) instead
// of being pulled from the hub by the hour. The release is a directory the whole runtime runs from, so one switch
// replaces everything at once and one switch puts it back.
//
//	<Root>/releases/<version>-<sha12>/   one release, complete when it holds .harnessly-complete
//	<Root>/current                       symlink to the release the services run from
//	<Root>/deployed.json                 what was activated last
//
// Two steps, the way a machine takes a release without a pause in its work. Stage checks the tarball against its
// checksum and unpacks it next to the running release; it changes nothing that runs, so every machine of a fleet can
// stage at once and a machine that cannot take the release stops the rollout while no machine has changed. Activate
// points current at the staged release, restarts the services and waits for the health check; a failed check puts the
// previous release back and restarts again. A delta is a tarball with only the parts that change between two releases:
// it is applied on top of a hard-linked copy of the running release (no second copy on the disk), and only when that
// release is the one the delta was cut against.

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	completeMark = ".harnessly-complete"
	// ReleaseFile is the file at the top of a release that names its version and commit.
	ReleaseFile = ".harnessly-release.json"
	// DefaultKeep is the number of newest releases kept on the disk (the active and the previous ones count, and stay anyway).
	DefaultKeep = 3
	// DefaultHealthTimeout is the time a restarted service has to pass its health check.
	DefaultHealthTimeout = 90 * time.Second
	// deltaFileLimit bounds the bytes one unpacking may write.
	deltaFileLimit = int64(4 << 30)
)

// ErrDeltaBase: the machine does not run the release this delta was cut against; nothing was changed. The whole
// release is the answer.
var ErrDeltaBase = errors.New("the running release is not the one this delta was cut against")

var (
	versionRe = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
	shaRe     = regexp.MustCompile(`^[0-9a-f]{64}$`)
	commitRe  = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

// Release is what ReleaseFile holds.
type Release struct {
	Version string `json:"version"`
	Commit  string `json:"commit,omitempty"`
	BuiltAt string `json:"builtAt,omitempty"`
}

// Fleet manages the releases of one machine.
type Fleet struct {
	// Root holds releases/, current and deployed.json.
	Root string
	// Keep is the number of newest releases kept (DefaultKeep when zero).
	Keep int
	// Parts are the top-level names a delta may replace; a delta holding anything else is refused.
	Parts []string
	// Restart restarts the services that run from current; nil means nothing to restart.
	Restart func(ctx context.Context) error
	// Probe answers the version the services serve now, or an error while they do not answer; nil skips the check.
	Probe func(ctx context.Context) (string, error)
	// HealthTimeout bounds the wait for Probe (DefaultHealthTimeout when zero).
	HealthTimeout time.Duration
	// PollEvery is the pause between probes (2 s when zero).
	PollEvery time.Duration
	// InUse tells that a release directory is still used by something that cannot be seen from here (a unit that
	// names it, a process that runs from it); nil means nothing is.
	InUse func(dir string) bool
	// Log gets one line per step; may be nil.
	Log func(msg string)
	// Services are the services that run from current, when each can be restarted on its own (fleet_stagger.go): they
	// are restarted one at a time, in this order, by RestartOne, and Restart and Probe are not used.
	Services []Service
	// RestartOne restarts the service of that name; nil restarts nothing.
	RestartOne func(ctx context.Context, name string) error
	// SettleTimeout bounds the wait for a restarted service's chats before the next one (DefaultSettleTimeout when zero).
	SettleTimeout time.Duration
	// MinGap is the machine's window: Activate refuses a release that comes sooner after the last one (ErrTooSoon).
	// Zero is no window. Force lets one activation through, for a rollout a person asked for.
	MinGap time.Duration
	Force  bool
	// Units is the machine's unit files (fleet_units.go): Activate renders and writes them after current
	// moves and before the services restart; nil leaves the units alone, as before.
	Units *UnitsSpec
	// UnitsErr is why the spec named in the environment could not be read; Activate refuses while it is set.
	UnitsErr error
	// UnitDir is the folder of the units (/etc/systemd/system when empty); Host names the machine for the pairs of the
	// spec; Systemctl replaces the real command in tests.
	UnitDir   string
	Host      string
	Systemctl func(ctx context.Context, args ...string) error
	// Now is the clock (time.Now when nil).
	Now func() time.Time
}

// StageInput says what to stage.
type StageInput struct {
	Version string
	// Tarball is the file: a whole release (one top-level directory `release/`) or, with DeltaBase, a delta.
	Tarball string
	// SHA256 is the checksum the tarball must have.
	SHA256 string
	// DeltaBase is the commit the delta was cut against; empty for a whole release.
	DeltaBase string
}

func (f *Fleet) say(format string, a ...any) {
	if f.Log != nil {
		f.Log(fmt.Sprintf(format, a...))
	}
}

func (f *Fleet) releases() string { return filepath.Join(f.Root, "releases") }
func (f *Fleet) current() string  { return filepath.Join(f.Root, "current") }

// Dir is the directory a staged release of this version and checksum lives in.
func (f *Fleet) Dir(version, sha string) string {
	return filepath.Join(f.releases(), version+"-"+sha[:12])
}

// Active is the release current points to, or "" when there is none.
func (f *Fleet) Active() string {
	p, err := filepath.EvalSymlinks(f.current())
	if err != nil {
		return ""
	}
	return p
}

// ReadRelease reads the release file of a release directory.
func ReadRelease(dir string) (Release, error) {
	var r Release
	raw, err := os.ReadFile(filepath.Join(dir, ReleaseFile))
	if err != nil {
		return r, err
	}
	return r, json.Unmarshal(raw, &r)
}

func fileSum(p string) (string, error) { return fileSHA(p) }

// Stage unpacks a release without touching anything that runs and returns its directory. A staged release is not
// unpacked again. The tarball is deleted when it was used.
func (f *Fleet) Stage(in StageInput) (string, error) {
	if !versionRe.MatchString(in.Version) {
		return "", fmt.Errorf("not a release version: %s", in.Version)
	}
	in.SHA256 = strings.ToLower(in.SHA256)
	if !shaRe.MatchString(in.SHA256) {
		return "", fmt.Errorf("not a sha256: %s", in.SHA256)
	}
	if in.DeltaBase != "" && !commitRe.MatchString(in.DeltaBase) {
		return "", fmt.Errorf("not a commit: %s", in.DeltaBase)
	}
	if err := os.MkdirAll(f.releases(), 0o755); err != nil {
		return "", err
	}
	unlock, err := lockRoot(f.Root)
	if err != nil {
		return "", err
	}
	defer unlock()

	dir := f.Dir(in.Version, in.SHA256)
	if _, err := os.Stat(filepath.Join(dir, completeMark)); err == nil {
		f.say("release %s is already unpacked in %s", in.Version, dir)
		_ = os.Remove(in.Tarball)
		return dir, nil
	}
	got, err := fileSum(in.Tarball)
	if err != nil {
		return "", err
	}
	if got != in.SHA256 {
		return "", errors.New("the uploaded release does not match its checksum")
	}
	// A delta is checked before anything is created: a machine that runs another release answers ErrDeltaBase.
	var base string
	if in.DeltaBase != "" {
		base = f.Active()
		if base == "" {
			_ = os.Remove(in.Tarball)
			return "", ErrDeltaBase
		}
		if r, err := ReadRelease(base); err != nil || r.Commit != in.DeltaBase {
			_ = os.Remove(in.Tarball)
			return "", ErrDeltaBase
		}
		if _, err := os.Stat(filepath.Join(base, completeMark)); err != nil {
			_ = os.Remove(in.Tarball)
			return "", ErrDeltaBase
		}
		if err := f.checkDelta(in.Tarball); err != nil {
			return "", err
		}
	}
	_ = os.RemoveAll(dir)
	incoming, err := os.MkdirTemp(f.releases(), ".incoming.")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(incoming)
	target := filepath.Join(incoming, "release")
	if base != "" {
		if err := linkTree(base, target); err != nil {
			return "", fmt.Errorf("copying the running release by hard links: %w", err)
		}
		_ = os.Remove(filepath.Join(target, completeMark))
		for _, p := range f.Parts {
			if err := os.RemoveAll(filepath.Join(target, p)); err != nil {
				return "", err
			}
		}
	}
	if err := extractTarball(in.Tarball, incoming, deltaFileLimit); err != nil {
		return "", err
	}
	rel, err := ReadRelease(target)
	if err != nil {
		return "", fmt.Errorf("the release has no %s: %w", ReleaseFile, err)
	}
	// The tarball is packed before the release has a number; the version the services report is the rollout's.
	rel.Version = in.Version
	raw, _ := json.Marshal(rel)
	if err := os.WriteFile(filepath.Join(target, ReleaseFile), append(raw, '\n'), 0o644); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(target, completeMark), nil, 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(target, dir); err != nil {
		return "", err
	}
	_ = os.Remove(in.Tarball)
	if base != "" {
		f.say("unpacked %s into %s on top of %s (delta)", in.Version, dir, base)
	} else {
		f.say("unpacked %s into %s", in.Version, dir)
	}
	return dir, nil
}

// checkDelta refuses a delta that holds more than Parts, a path with .. or no part at all.
func (f *Fleet) checkDelta(tarball string) error {
	allowed := map[string]bool{}
	for _, p := range f.Parts {
		allowed[p] = true
	}
	if len(allowed) == 0 {
		return errors.New("this machine accepts no delta: no parts are configured")
	}
	names, err := tarNames(tarball)
	if err != nil {
		return fmt.Errorf("the delta is not a tarball: %w", err)
	}
	seen := false
	for _, n := range names {
		clean := path.Clean(strings.TrimPrefix(n, "./"))
		if clean == "release" || clean == "." {
			continue
		}
		if !strings.HasPrefix(clean, "release/") || strings.Contains(clean, "..") {
			return fmt.Errorf("the delta names a path outside release/: %s", n)
		}
		top := strings.SplitN(strings.TrimPrefix(clean, "release/"), "/", 2)[0]
		if !allowed[top] {
			return fmt.Errorf("the delta holds %s, which is not one of its parts", top)
		}
		seen = true
	}
	if !seen {
		return errors.New("the delta is empty")
	}
	return nil
}

func tarNames(p string) ([]string, error) {
	fh, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	zr, err := gzip.NewReader(fh)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	tr := tar.NewReader(zr)
	var out []string
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		out = append(out, h.Name)
	}
}

// extractTarball unpacks a .tgz into dir. Only regular files, directories and symlinks that stay inside dir are
// accepted; a path that leaves dir is an error. limit bounds the bytes written.
func extractTarball(p, dir string, limit int64) error {
	fh, err := os.Open(p)
	if err != nil {
		return err
	}
	defer fh.Close()
	zr, err := gzip.NewReader(fh)
	if err != nil {
		return err
	}
	defer zr.Close()
	tr := tar.NewReader(zr)
	root := filepath.Clean(dir)
	var written int64
	var hard [][2]string
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		dest := filepath.Join(root, filepath.FromSlash(h.Name))
		if dest != root && !strings.HasPrefix(dest, root+string(filepath.Separator)) {
			return fmt.Errorf("the tarball names a path outside the release: %s", h.Name)
		}
		mode := fs.FileMode(h.Mode).Perm()
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode|0o200)
			if err != nil {
				return err
			}
			n, err := io.Copy(out, io.LimitReader(tr, limit-written+1))
			if cerr := out.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return err
			}
			written += n
			if written > limit {
				return errors.New("the tarball is larger than allowed")
			}
			if err := os.Chmod(dest, mode); err != nil {
				return err
			}
		case tar.TypeSymlink:
			// A relative link may point anywhere inside the release; one that leaves it is refused.
			target := h.Linkname
			resolved := target
			if !filepath.IsAbs(target) {
				resolved = filepath.Join(filepath.Dir(dest), target)
			}
			if !filepath.IsAbs(target) && resolved != root && !strings.HasPrefix(resolved, root+string(filepath.Separator)) {
				return fmt.Errorf("the link %s leaves the release: %s", h.Name, target)
			}
			if filepath.IsAbs(target) {
				return fmt.Errorf("the link %s is absolute: %s", h.Name, target)
			}
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return err
			}
			_ = os.Remove(dest)
			if err := os.Symlink(target, dest); err != nil {
				return err
			}
		case tar.TypeLink:
			src := filepath.Join(root, filepath.FromSlash(h.Linkname))
			if src != root && !strings.HasPrefix(src, root+string(filepath.Separator)) {
				return fmt.Errorf("the link %s leaves the release: %s", h.Name, h.Linkname)
			}
			hard = append(hard, [2]string{src, dest})
		default:
			// Devices, fifos and the like have no place in a release.
			return fmt.Errorf("the tarball holds %s of an unsupported type", h.Name)
		}
	}
	for _, l := range hard {
		if err := os.MkdirAll(filepath.Dir(l[1]), 0o755); err != nil {
			return err
		}
		_ = os.Remove(l[1])
		if err := os.Link(l[0], l[1]); err != nil {
			return err
		}
	}
	return nil
}

// linkTree copies src to dst: directories and symlinks are made again, files are hard links (nothing is copied, nothing
// is written into src).
func linkTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		to := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			return os.MkdirAll(to, info.Mode().Perm()|0o700)
		case info.Mode()&fs.ModeSymlink != 0:
			target, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return os.Symlink(target, to)
		case info.Mode().IsRegular():
			return os.Link(p, to)
		}
		return nil
	})
}

func (f *Fleet) pointCurrent(dir string) error {
	tmp := f.current() + ".new"
	_ = os.Remove(tmp)
	if err := os.Symlink(dir, tmp); err != nil {
		return err
	}
	return os.Rename(tmp, f.current())
}

func (f *Fleet) wait(ctx context.Context, expected string) error {
	if f.Probe == nil {
		return nil
	}
	limit := f.HealthTimeout
	if limit <= 0 {
		limit = DefaultHealthTimeout
	}
	every := f.PollEvery
	if every <= 0 {
		every = 2 * time.Second
	}
	deadline := time.Now().Add(limit)
	var last string
	for {
		got, err := f.Probe(ctx)
		switch {
		case err == nil && (expected == "" || got == expected):
			return nil
		case err != nil:
			last = err.Error()
		default:
			last = "serves " + got
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the services did not pass the check in %s: %s (want %s)", limit, last, expected)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(every):
		}
	}
}

func (f *Fleet) restart(ctx context.Context) error {
	if f.Restart == nil {
		return nil
	}
	return f.Restart(ctx)
}

// Activate points current at a staged release, restarts the services and waits for them to serve its version. When any
// step fails the previous release is put back and the services are restarted again; the error says why.
func (f *Fleet) Activate(ctx context.Context, version, sha string) error {
	if !versionRe.MatchString(version) || !shaRe.MatchString(strings.ToLower(sha)) {
		return fmt.Errorf("not a release: %s %s", version, sha)
	}
	unlock, err := lockRoot(f.Root)
	if err != nil {
		return err
	}
	defer unlock()
	dir := f.Dir(version, strings.ToLower(sha))
	if _, err := os.Stat(filepath.Join(dir, completeMark)); err != nil {
		return fmt.Errorf("release %s is not staged in %s", version, dir)
	}
	if f.UnitsErr != nil {
		return fmt.Errorf("the units spec is unusable, nothing changed: %w", f.UnitsErr)
	}
	previous := f.Active()
	if previous == dir {
		// Already the active one: the services are checked, not restarted.
		if err := f.check(ctx, version); err != nil {
			return err
		}
		f.say("release %s is already active", version)
		return nil
	}
	if left := f.tooSoon(); left > 0 {
		return fmt.Errorf("%w: the next one may come in %s", ErrTooSoon, left.Round(time.Second))
	}
	if err := f.pointCurrent(dir); err != nil {
		return err
	}
	changed, err := f.units(ctx, version, dir)
	if err != nil {
		// Nothing was restarted: only current goes back.
		if previous == "" {
			_ = os.Remove(f.current())
		} else {
			_ = f.pointCurrent(previous)
		}
		return fmt.Errorf("release %s: the units were not written, the previous release is back: %w", version, err)
	}
	restarted, err := f.cycle(ctx, version, nil, true)
	if err != nil {
		f.restoreUnits(ctx, changed)
		f.rollbackTo(ctx, previous, restarted)
		return fmt.Errorf("release %s failed its checks and was rolled back: %w", version, err)
	}
	rec, _ := json.Marshal(map[string]string{"version": version, "release": dir, "previous": previous,
		"activatedAt": f.now().UTC().Format(time.RFC3339)})
	_ = os.WriteFile(filepath.Join(f.Root, "deployed.json"), append(rec, '\n'), 0o644)
	f.say("active: %s", version)
	f.Prune(previous)
	return nil
}

// rollbackTo puts previous back. Of the services restarted one at a time only those in restarted go back: the others
// never left the previous release.
func (f *Fleet) rollbackTo(ctx context.Context, previous string, restarted []string) {
	f.say("rolling back to %s", orNothing(previous))
	if previous == "" {
		_ = os.Remove(f.current())
	} else {
		_ = f.pointCurrent(previous)
	}
	if !f.staggered() {
		_ = f.restart(ctx)
		if previous != "" {
			if r, err := ReadRelease(previous); err == nil {
				if err := f.wait(ctx, r.Version); err != nil {
					f.say("after rollback: %v", err)
				}
			}
		}
		return
	}
	if previous == "" {
		// Nothing to serve: the services are only told that current is gone.
		for _, name := range restarted {
			if f.RestartOne != nil {
				_ = f.RestartOne(ctx, name)
			}
		}
		return
	}
	expected := ""
	if r, err := ReadRelease(previous); err == nil {
		expected = r.Version
	}
	if _, err := f.cycle(ctx, expected, restarted, false); err != nil {
		f.say("after rollback: %v", err)
	}
}

func orNothing(s string) string {
	if s == "" {
		return "nothing"
	}
	return s
}

// Rollback puts the release that was active before the last activation back (deployed.json names it).
func (f *Fleet) Rollback(ctx context.Context) error {
	unlock, err := lockRoot(f.Root)
	if err != nil {
		return err
	}
	defer unlock()
	raw, err := os.ReadFile(filepath.Join(f.Root, "deployed.json"))
	if err != nil {
		return errors.New("nothing was activated here: there is no previous release")
	}
	m := map[string]string{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return err
	}
	rec := struct{ Previous, Release string }{m["previous"], m["release"]}
	if rec.Previous == "" {
		return errors.New("the last activation had no previous release")
	}
	if _, err := os.Stat(filepath.Join(rec.Previous, completeMark)); err != nil {
		return fmt.Errorf("the previous release %s is gone", rec.Previous)
	}
	if err := f.pointCurrent(rec.Previous); err != nil {
		return err
	}
	r, _ := ReadRelease(rec.Previous)
	if _, err := f.cycle(ctx, r.Version, nil, false); err != nil {
		return err
	}
	out, _ := json.Marshal(map[string]string{"version": r.Version, "release": rec.Previous, "previous": rec.Release,
		"activatedAt": time.Now().UTC().Format(time.RFC3339)})
	_ = os.WriteFile(filepath.Join(f.Root, "deployed.json"), append(out, '\n'), 0o644)
	f.say("rolled back to %s", r.Version)
	return nil
}

// Prune removes the releases beyond the Keep newest, except the active one, the given previous one and any that InUse
// reports. It returns the directories removed.
func (f *Fleet) Prune(previous string) []string {
	keep := f.Keep
	if keep <= 0 {
		keep = DefaultKeep
	}
	entries, err := os.ReadDir(f.releases())
	if err != nil {
		return nil
	}
	type rel struct {
		dir string
		at  time.Time
	}
	var all []rel
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		all = append(all, rel{filepath.Join(f.releases(), e.Name()), info.ModTime()})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].at.After(all[j].at) })
	active := f.Active()
	var removed []string
	kept := 0
	for _, r := range all {
		if r.dir == active || r.dir == previous || kept < keep {
			kept++
			continue
		}
		if f.InUse != nil && f.InUse(r.dir) {
			f.say("keeping %s: still in use", filepath.Base(r.dir))
			continue
		}
		if err := os.RemoveAll(r.dir); err == nil {
			f.say("removed %s", filepath.Base(r.dir))
			removed = append(removed, r.dir)
		}
	}
	return removed
}

// FleetStatus is what a machine in fleet mode tells.
type FleetStatus struct {
	Root     string   `json:"root"`
	Active   string   `json:"active,omitempty"`
	Version  string   `json:"version,omitempty"`
	Commit   string   `json:"commit,omitempty"`
	Staged   []string `json:"staged,omitempty"`
	Deployed string   `json:"deployedAt,omitempty"`
}

// Status reads the releases on the disk.
func (f *Fleet) Status() FleetStatus {
	st := FleetStatus{Root: f.Root, Active: f.Active()}
	if r, err := ReadRelease(st.Active); err == nil && st.Active != "" {
		st.Version, st.Commit = r.Version, r.Commit
	}
	if entries, err := os.ReadDir(f.releases()); err == nil {
		for _, e := range entries {
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
				st.Staged = append(st.Staged, e.Name())
			}
		}
	}
	if raw, err := os.ReadFile(filepath.Join(f.Root, "deployed.json")); err == nil {
		m := map[string]string{}
		if json.Unmarshal(raw, &m) == nil {
			st.Deployed = m["activatedAt"]
		}
	}
	return st
}

// CutDelta writes a delta from a whole release tarball: the members under release/<part> for each part, and nothing
// else. It fails when the tarball lacks a part or was not packed from commit head (the release file says so), so a
// delta is always made of the bytes of the very release it stands for. Returns the sha256 of the delta.
func CutDelta(tarball, out, head string, parts []string) (string, error) {
	if !commitRe.MatchString(head) {
		return "", fmt.Errorf("not a commit: %s", head)
	}
	if len(parts) == 0 {
		return "", errors.New("no parts to cut")
	}
	src, err := os.Open(tarball)
	if err != nil {
		return "", err
	}
	defer src.Close()
	zr, err := gzip.NewReader(src)
	if err != nil {
		return "", err
	}
	defer zr.Close()
	tr := tar.NewReader(zr)
	want := map[string]bool{}
	for _, p := range parts {
		want[p] = true
	}
	tmp, err := os.CreateTemp(filepath.Dir(out), ".delta-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	hash := sha256.New()
	zw, _ := gzip.NewWriterLevel(io.MultiWriter(tmp, hash), gzip.BestSpeed)
	tw := tar.NewWriter(zw)
	found := map[string]bool{}
	headOK := false
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", err
		}
		clean := path.Clean(strings.TrimPrefix(h.Name, "./"))
		top := strings.SplitN(strings.TrimPrefix(clean, "release/"), "/", 2)[0]
		if !strings.HasPrefix(clean, "release/") || !want[top] {
			continue
		}
		found[top] = true
		hdr := *h
		if err := tw.WriteHeader(&hdr); err != nil {
			return "", err
		}
		if h.Typeflag == tar.TypeReg {
			if top == ReleaseFile {
				raw, err := io.ReadAll(io.LimitReader(tr, 1<<20))
				if err != nil {
					return "", err
				}
				headOK = strings.Contains(string(raw), `"commit":"`+head+`"`)
				if _, err := tw.Write(raw); err != nil {
					return "", err
				}
				continue
			}
			if _, err := io.Copy(tw, tr); err != nil {
				return "", err
			}
		}
	}
	for _, p := range parts {
		if !found[p] {
			return "", fmt.Errorf("the tarball lacks %s: it cannot be cut into a delta", p)
		}
	}
	if want[ReleaseFile] && !headOK {
		return "", fmt.Errorf("the tarball was not packed from %s", head)
	}
	if err := tw.Close(); err != nil {
		return "", err
	}
	if err := zw.Close(); err != nil {
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmp.Name(), out); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
