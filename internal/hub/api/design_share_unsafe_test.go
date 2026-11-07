package api_test

import (
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ly-solutions/harnessly/internal/design"
	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
)

// GitHub issue 30: a machine whose filesystem fails the share probe (a sandbox that does not keep lookups beneath a
// folder) makes no guest link and serves no guest, and both say why and what to run instead; once the probe holds the
// same machine shares as usual.
func TestDesignShareFailsClosedOnAnUnsafeRuntime(t *testing.T) {
	s := hubtest.New(t, hubtest.Env("HUB_DESIGN_SHARE_URL", "http://"+shareHost))
	owner := s.SignIn("owner@example.com")
	space, _ := owner.NewSpace("Studio")
	a, d := designMachine(t, s, owner, space)
	var unsafe atomic.Bool
	unsafe.Store(true)
	d.api.L = design.New(design.Config{LibraryDir: d.root, ReleasesDir: t.TempDir(), Texts: design.Texts{Tag: "harnessly-design", CLI: "harnessly"},
		ShareProbe: func(dir string) design.ShareFSReport {
			if unsafe.Load() {
				return design.ShareFSReport{Steps: []design.ShareFSStep{{ID: "dotdot", Code: design.ShareFSDotDot}}, Hint: design.ShareFSHint}
			}
			return design.ProbeShareFS(dir)
		}})
	t.Cleanup(d.api.L.StopWatching)
	base := "/m/" + a.MachineID + "/api/platform/design/access"
	created(a, "shop", "owner@example.com")
	s.Eventually("the project recorded", func() bool {
		role, ok := snapshotFor(a, "owner@example.com", "shop")
		return ok && role == "owner"
	})

	for _, version := range []string{"latest", "pinned"} {
		r := owner.Do("POST", base+"/shop/shares", map[string]any{"rights": "view", "version": version})
		if r.Code != 409 || !strings.Contains(r.String(), "share_unsafe_runtime") || !strings.Contains(r.String(), "harnessly doctor share") {
			t.Fatalf("a %s link on an unsafe runtime: %s", version, r)
		}
	}
	var list []map[string]any
	owner.Do("GET", base+"/shop/shares", nil).JSON(&list)
	if len(list) != 0 {
		t.Fatalf("a refused link was kept: %+v", list)
	}

	unsafe.Store(false)
	r := owner.Do("POST", base+"/shop/shares", map[string]any{"rights": "view"})
	if r.Code != 201 {
		t.Fatalf("a link once the probe holds: %s", r)
	}
	var link struct{ URL string }
	r.JSON(&link)
	path := strings.TrimPrefix(link.URL, "http://"+shareHost)
	guest := s.Browser()
	open := func(p string) hubtest.Resp { t.Helper(); return guest.DoAt(s.URL, shareHost, "GET", p, nil) }
	if r := open(path + "/f/Main.dc.html"); r.Code != 200 || !strings.Contains(string(r.Body), "shop main") {
		t.Fatalf("guest page on a safe runtime: %s", r)
	}

	// The runtime turns unsafe (moved into a sandbox): the guest is refused in words of their own, not «machine down».
	unsafe.Store(true)
	for _, p := range []string{"/index.json", "/f/Main.dc.html"} {
		r := open(path + p)
		if r.Code != 503 || strings.Contains(string(r.Body), "shop main") || !strings.Contains(string(r.Body), "гарантий безопасности") {
			t.Fatalf("guest %s on an unsafe runtime: %s", p, r)
		}
	}
}
