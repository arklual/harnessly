package design

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The share filesystem probe (GitHub issue 30): on native Linux every guarantee holds and guests are served; a runtime
// whose lookups are not confined (the way a sandbox that fails containment behaves, stood in for by an opener that
// follows every path) fails the probe, and then no guest read is served and no link is made, while the render doctor
// knows nothing of it.

func TestShareFSHoldsOnNativeLinux(t *testing.T) {
	rep := ProbeShareFS(t.TempDir())
	if !rep.OK || rep.Hint != "" {
		t.Fatalf("native filesystem: %+v", rep)
	}
	want := []string{"inside", "dotdot", "relative-symlink", "absolute-symlink", "directory-symlink", "swap"}
	if len(rep.Steps) != len(want) {
		t.Fatalf("steps: %+v", rep.Steps)
	}
	for i, id := range want {
		if rep.Steps[i].ID != id || !rep.Steps[i].OK {
			t.Fatalf("step %d: %+v", i, rep.Steps[i])
		}
	}
}

func TestShareFSLeavesNothingBehind(t *testing.T) {
	dir := t.TempDir()
	ProbeShareFS(dir)
	left, err := os.ReadDir(dir)
	if err != nil || len(left) != 0 {
		t.Fatalf("left in the library: %v %v", left, err)
	}
}

// unconfined stands in for a runtime that does not keep lookups beneath the folder.
func unconfined(t *testing.T) {
	t.Helper()
	was := openBeneath
	openBeneath = func(dir, rel string) (*os.File, error) { return os.Open(filepath.Join(dir, rel)) }
	t.Cleanup(func() { openBeneath = was })
}

func TestShareFSFailsWhereLookupsEscape(t *testing.T) {
	unconfined(t)
	rep := ProbeShareFS(t.TempDir())
	if rep.OK || rep.Hint != ShareFSHint {
		t.Fatalf("an unconfined filesystem passed: %+v", rep)
	}
	failed := map[string]bool{}
	for _, s := range rep.Steps {
		if !s.OK {
			failed[s.Code] = true
		}
	}
	for _, code := range []string{ShareFSDotDot, ShareFSRelLink, ShareFSAbsLink, ShareFSDirLink} {
		if !failed[code] {
			t.Errorf("%s did not fail: %+v", code, rep.Steps)
		}
	}
	if f := rep.Failed(); f == nil || f.Code != ShareFSDotDot || !strings.Contains(f.Detail, "outside") {
		t.Errorf("first failure: %+v", f)
	}
}

func TestShareFSFailsWithoutALibrary(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if rep := ProbeShareFS(filepath.Join(file, "library")); rep.OK || rep.Failed().Code != ShareFSSetup {
		t.Fatalf("a library that cannot be written: %+v", rep)
	}
}

func TestPublicFailsClosedOnAnUnsafeRuntime(t *testing.T) {
	unconfined(t)
	a, _ := publicAPI(t, "")
	guest := scopeToken(t, nil)
	pin := scopeToken(t, map[string]any{"pin": true})
	for _, p := range []string{"p1", "p1/index.html", "p1/assets/a.css", "p1/" + PublicAppDir + "/index.html", "p1/" + PublicGuestDir + "/client.js"} {
		w := publicGet(a, "GET", p, guest)
		if w.Code != 503 || w.Header().Get(ShareRefusedHeader) != ShareUnsafeRuntime {
			t.Errorf("guest %s: %d %q", p, w.Code, w.Header().Get(ShareRefusedHeader))
		}
	}
	for _, p := range []string{PublicShareDir, PublicRevDir} {
		w := publicGet(a, "GET", "p1/"+p, pin)
		var ref ShareRefusal
		if w.Code != 409 || json.Unmarshal(w.Body.Bytes(), &ref) != nil || ref.Code != ShareUnsafeRuntime || !ref.Blocked ||
			!strings.Contains(ref.Error, "runc") || w.Header().Get(ShareRefusedHeader) != ShareUnsafeRuntime {
			t.Errorf("link through %s: %d %s", p, w.Code, w.Body.String())
		}
	}
	// A bad token is still told apart first: the probe says nothing to someone who may not ask.
	if w := publicGet(a, "GET", "p1", "nope"); w.Code != 401 && w.Code != 403 {
		t.Errorf("a bad token: %d", w.Code)
	}
}

func TestPublicServesWhenTheProbeHolds(t *testing.T) {
	a, _ := publicAPI(t, "")
	if w := publicGet(a, "GET", "p1/index.html", scopeToken(t, nil)); w.Code != 200 || w.Header().Get(ShareRefusedHeader) != "" {
		t.Fatalf("native runtime: %d", w.Code)
	}
	if rep := a.L.ShareFS(); !rep.OK {
		t.Fatalf("cached report: %+v", rep)
	}
}
