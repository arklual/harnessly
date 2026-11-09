package selfupdate

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const commitA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const commitB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

type member struct {
	name, body, link string
	dir              bool
}

// tgz writes a tarball of the members and returns its path and checksum.
func tgz(t *testing.T, name string, ms []member) (string, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	fh, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	zw := gzip.NewWriter(fh)
	tw := tar.NewWriter(zw)
	for _, m := range ms {
		h := &tar.Header{Name: m.name, Mode: 0o644}
		switch {
		case m.dir:
			h.Typeflag, h.Mode = tar.TypeDir, 0o755
		case m.link != "":
			h.Typeflag, h.Linkname = tar.TypeSymlink, m.link
		default:
			h.Typeflag, h.Size = tar.TypeReg, int64(len(m.body))
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			if _, err := tw.Write([]byte(m.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := fh.Close(); err != nil {
		t.Fatal(err)
	}
	sha, err := fileSHA(p)
	if err != nil {
		t.Fatal(err)
	}
	return p, sha
}

func release(commit, agent string) []member {
	return []member{
		{name: "release/", dir: true},
		{name: "release/" + ReleaseFile, body: `{"version":"0.0.0","commit":"` + commit + `"}`},
		{name: "release/dist/server.js", body: "server"},
		{name: "release/bin/", dir: true},
		{name: "release/agent/harnessly-agent", body: agent},
		{name: "release/link", link: "dist/server.js"},
	}
}

func newFleet(t *testing.T) (*Fleet, *[]string, *string) {
	t.Helper()
	serving := ""
	var restarts []string
	f := &Fleet{Root: filepath.Join(t.TempDir(), "fleet"), Parts: []string{"agent", ReleaseFile}, PollEvery: time.Millisecond,
		HealthTimeout: 50 * time.Millisecond}
	f.Restart = func(context.Context) error {
		restarts = append(restarts, "restart")
		r, err := ReadRelease(f.current())
		if err == nil {
			serving = r.Version
		}
		return nil
	}
	f.Probe = func(context.Context) (string, error) {
		if serving == "" {
			return "", errors.New("no answer")
		}
		return serving, nil
	}
	return f, &restarts, &serving
}

func TestFleetStageActivateAndDelta(t *testing.T) {
	f, restarts, _ := newFleet(t)
	tar1, sha1 := tgz(t, "r1.tgz", release(commitA, "agent one"))
	dir1, err := f.Stage(StageInput{Version: "1.0.0", Tarball: tar1, SHA256: sha1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(tar1); !os.IsNotExist(err) {
		t.Fatal("the staged tarball must be deleted")
	}
	if f.Active() != "" || len(*restarts) != 0 {
		t.Fatal("staging must not touch what runs")
	}
	if r, _ := ReadRelease(dir1); r.Version != "1.0.0" || r.Commit != commitA {
		t.Fatalf("release file: %+v", r)
	}
	if err := f.Activate(context.Background(), "1.0.0", sha1); err != nil {
		t.Fatal(err)
	}
	if f.Active() != mustEval(t, dir1) {
		t.Fatalf("active %s, want %s", f.Active(), dir1)
	}

	// The next release differs only in the agent: it goes as a delta cut from the whole tarball.
	whole2, sha2 := tgz(t, "r2.tgz", release(commitB, "agent two"))
	delta := filepath.Join(t.TempDir(), "d.tgz")
	dsha, err := CutDelta(whole2, delta, commitB, f.Parts)
	if err != nil {
		t.Fatal(err)
	}
	_ = sha2
	dir2, err := f.Stage(StageInput{Version: "1.1.0", Tarball: delta, SHA256: dsha, DeltaBase: commitA})
	if err != nil {
		t.Fatal(err)
	}
	// The unchanged file is the very file of the running release (one inode), the changed one is new.
	a, b := mustStat(t, filepath.Join(dir1, "dist/server.js")), mustStat(t, filepath.Join(dir2, "dist/server.js"))
	if !os.SameFile(a, b) {
		t.Fatal("the delta must share unchanged files with the running release")
	}
	got, _ := os.ReadFile(filepath.Join(dir2, "agent/harnessly-agent"))
	if string(got) != "agent two" {
		t.Fatalf("agent: %q", got)
	}
	old, _ := os.ReadFile(filepath.Join(dir1, "agent/harnessly-agent"))
	if string(old) != "agent one" {
		t.Fatal("staging a delta must not write into the running release")
	}
	if l, _ := os.Readlink(filepath.Join(dir2, "link")); l != "dist/server.js" {
		t.Fatalf("symlink: %q", l)
	}
	if err := f.Activate(context.Background(), "1.1.0", dsha); err != nil {
		t.Fatal(err)
	}
	if st := f.Status(); st.Version != "1.1.0" || st.Commit != commitB {
		t.Fatalf("status %+v", st)
	}
	if err := f.Rollback(context.Background()); err != nil {
		t.Fatal(err)
	}
	if st := f.Status(); st.Version != "1.0.0" {
		t.Fatalf("after rollback %+v", st)
	}
}

func TestFleetDeltaOnAnotherReleaseChangesNothing(t *testing.T) {
	f, _, _ := newFleet(t)
	tar1, sha1 := tgz(t, "r1.tgz", release(commitA, "agent one"))
	if _, err := f.Stage(StageInput{Version: "1.0.0", Tarball: tar1, SHA256: sha1}); err != nil {
		t.Fatal(err)
	}
	if err := f.Activate(context.Background(), "1.0.0", sha1); err != nil {
		t.Fatal(err)
	}
	whole, _ := tgz(t, "w.tgz", release(commitB, "x"))
	delta := filepath.Join(t.TempDir(), "d.tgz")
	dsha, err := CutDelta(whole, delta, commitB, f.Parts)
	if err != nil {
		t.Fatal(err)
	}
	before := f.Status()
	_, err = f.Stage(StageInput{Version: "1.1.0", Tarball: delta, SHA256: dsha, DeltaBase: strings.Repeat("c", 40)})
	if !errors.Is(err, ErrDeltaBase) {
		t.Fatalf("want ErrDeltaBase, got %v", err)
	}
	after := f.Status()
	if len(after.Staged) != len(before.Staged) {
		t.Fatalf("nothing may be staged: %v", after.Staged)
	}
}

func TestFleetFailedCheckRollsBack(t *testing.T) {
	f, _, serving := newFleet(t)
	tar1, sha1 := tgz(t, "r1.tgz", release(commitA, "one"))
	if _, err := f.Stage(StageInput{Version: "1.0.0", Tarball: tar1, SHA256: sha1}); err != nil {
		t.Fatal(err)
	}
	if err := f.Activate(context.Background(), "1.0.0", sha1); err != nil {
		t.Fatal(err)
	}
	tar2, sha2 := tgz(t, "r2.tgz", release(commitB, "two"))
	if _, err := f.Stage(StageInput{Version: "1.1.0", Tarball: tar2, SHA256: sha2}); err != nil {
		t.Fatal(err)
	}
	// The services of the new release never answer.
	f.Restart = func(context.Context) error {
		r, _ := ReadRelease(f.current())
		if r.Version == "1.0.0" {
			*serving = "1.0.0"
		} else {
			*serving = ""
		}
		return nil
	}
	err := f.Activate(context.Background(), "1.1.0", sha2)
	if err == nil || !strings.Contains(err.Error(), "rolled back") {
		t.Fatalf("want a rollback, got %v", err)
	}
	if st := f.Status(); st.Version != "1.0.0" {
		t.Fatalf("the previous release must be back: %+v", st)
	}
}

func TestFleetRefusesBadTarballs(t *testing.T) {
	f, _, _ := newFleet(t)
	bad, sha := tgz(t, "bad.tgz", []member{{name: "release/" + ReleaseFile, body: "{}"}, {name: "../escape", body: "x"}})
	if _, err := f.Stage(StageInput{Version: "1.0.0", Tarball: bad, SHA256: sha}); err == nil {
		t.Fatal("a path outside the release must be refused")
	}
	abs, sha := tgz(t, "abs.tgz", []member{{name: "release/l", link: "/etc/passwd"}})
	if _, err := f.Stage(StageInput{Version: "1.0.0", Tarball: abs, SHA256: sha}); err == nil {
		t.Fatal("an absolute link must be refused")
	}
	ok, _ := tgz(t, "ok.tgz", release(commitA, "a"))
	if _, err := f.Stage(StageInput{Version: "1.0.0", Tarball: ok, SHA256: strings.Repeat("0", 64)}); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("a wrong checksum must be refused: %v", err)
	}
	if _, err := f.Stage(StageInput{Version: "v1", Tarball: ok, SHA256: strings.Repeat("0", 64)}); err == nil {
		t.Fatal("a bad version must be refused")
	}
	// A delta that carries more than its parts.
	_, err := f.Stage(StageInput{Version: "1.0.0", Tarball: ok, SHA256: mustSum(t, ok), DeltaBase: commitA})
	if !errors.Is(err, ErrDeltaBase) {
		t.Fatalf("a delta with no running release: %v", err)
	}
}

func TestFleetDeltaHoldingMoreThanItsPartsIsRefused(t *testing.T) {
	f, _, _ := newFleet(t)
	tar1, sha1 := tgz(t, "r1.tgz", release(commitA, "one"))
	if _, err := f.Stage(StageInput{Version: "1.0.0", Tarball: tar1, SHA256: sha1}); err != nil {
		t.Fatal(err)
	}
	if err := f.Activate(context.Background(), "1.0.0", sha1); err != nil {
		t.Fatal(err)
	}
	whole, sha := tgz(t, "w.tgz", release(commitB, "two"))
	if _, err := f.Stage(StageInput{Version: "1.1.0", Tarball: whole, SHA256: sha, DeltaBase: commitA}); err == nil || errors.Is(err, ErrDeltaBase) {
		t.Fatalf("a whole release sent as a delta must be refused, got %v", err)
	}
}

func TestFleetPruneKeepsActiveAndPrevious(t *testing.T) {
	f, _, _ := newFleet(t)
	f.Keep = 2
	var shas []string
	for i, v := range []string{"1.0.0", "1.1.0", "1.2.0", "1.3.0"} {
		p, sha := tgz(t, "r.tgz", release(strings.Repeat(string(rune('a'+i)), 40), strings.Repeat("x", i+1)))
		if _, err := f.Stage(StageInput{Version: v, Tarball: p, SHA256: sha}); err != nil {
			t.Fatal(err)
		}
		// Older directories get older times so the order does not depend on the clock's grain.
		ts := time.Now().Add(time.Duration(i-10) * time.Hour)
		_ = os.Chtimes(f.Dir(v, sha), ts, ts)
		shas = append(shas, sha)
	}
	if err := f.Activate(context.Background(), "1.3.0", shas[3]); err != nil {
		t.Fatal(err)
	}
	st := f.Status()
	if len(st.Staged) != 2 {
		t.Fatalf("want the active and the newest other, got %v", st.Staged)
	}
	if _, err := os.Stat(f.Dir("1.3.0", shas[3])); err != nil {
		t.Fatal("the active release must stay")
	}
}

func TestFleetOneRolloutAtATime(t *testing.T) {
	f, _, _ := newFleet(t)
	unlock, err := lockRoot(f.Root)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	p, sha := tgz(t, "r.tgz", release(commitA, "a"))
	if _, err := f.Stage(StageInput{Version: "1.0.0", Tarball: p, SHA256: sha}); err == nil || !strings.Contains(err.Error(), "another rollout") {
		t.Fatalf("a second rollout must wait: %v", err)
	}
}

func TestUpdaterFleetModeLeavesTheHubAlone(t *testing.T) {
	f, _, _ := newFleet(t)
	p, sha := tgz(t, "r.tgz", release(commitA, "a"))
	if _, err := f.Stage(StageInput{Version: "2.0.0", Tarball: p, SHA256: sha}); err != nil {
		t.Fatal(err)
	}
	if err := f.Activate(context.Background(), "2.0.0", sha); err != nil {
		t.Fatal(err)
	}
	u := &Updater{HubURL: "http://hub.invalid", Fleet: f, Auto: true}
	if err := u.Apply(context.Background()); !errors.Is(err, ErrFleet) {
		t.Fatalf("apply: %v", err)
	}
	st, err := u.Check(context.Background())
	if err != nil || st.Mode != "fleet" || st.Fleet == nil || st.Fleet.Version != "2.0.0" || st.Version != "2.0.0" {
		t.Fatalf("status %+v err %v", st, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() { u.Run(ctx); close(done) }()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Run must return at once in fleet mode")
	}
}

func mustEval(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func mustStat(t *testing.T, p string) os.FileInfo {
	t.Helper()
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return fi
}

func mustSum(t *testing.T, p string) string {
	t.Helper()
	s, err := fileSHA(p)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
