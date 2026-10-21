package design

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func sha(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

type relEntry struct {
	ID, Status, Engine string
	Bad                bool
}

// releasesMirror is a mirror folder with an engine per release; a Bad one lists a checksum its engine does not have.
func releasesMirror(t *testing.T, entries ...relEntry) string {
	t.Helper()
	dir := t.TempDir()
	var list []map[string]string
	for _, e := range entries {
		rel := "upstream/design/" + e.ID + "/dc-runtime.js"
		writeFile(t, filepath.Join(dir, rel), e.Engine)
		sum := sha(e.Engine)
		if e.Bad {
			sum = sha("something else")
		}
		writeFile(t, filepath.Join(dir, "skills", "design-"+e.ID, "SKILL.md"), "# "+e.ID)
		list = append(list, map[string]string{"release": e.ID, "date": "2026-10-21", "status": e.Status, "engine": rel,
			"engineSha256": sum, "skill": "skills/design-" + e.ID})
	}
	b, _ := json.Marshal(map[string]any{"v": 1, "types": map[string]any{"design": map[string]any{"releases": list}}})
	writeFile(t, filepath.Join(dir, ReleasesFile), string(b))
	return dir
}

func releasesLibrary(t *testing.T, mirror string) (*Library, string) {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "p1", "project", "Main.dc.html"), `<html><head><script src="./support.js"></script></head><body>x</body></html>`)
	writeFile(t, filepath.Join(root, "p1", "project", "sub", "Inner.dc.html"), `<html><head><script src="../support.js"></script></head><body>y</body></html>`)
	writeFile(t, filepath.Join(root, "p1", "project", "support.js"), "/* own engine */")
	l := New(Config{LibraryDir: root, ReleasesDir: mirror, Texts: Texts{Tag: "test-design", CLI: "acme"}})
	t.Cleanup(l.StopWatching)
	return l, root
}

func libGet(l *Library, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", path, nil)
	r.Host = "127.0.0.1:18470"
	r.Header.Set("Accept-Encoding", "identity;q=1, gzip;q=0")
	l.serveLibrary(w, r, 18470)
	return w
}

func TestReleasesListSkipsBrokenAndUnknown(t *testing.T) {
	mirror := releasesMirror(t,
		relEntry{ID: "legacy", Status: "stable", Engine: "legacy engine"},
		relEntry{ID: "r2", Status: "stable", Engine: "r2 engine"},
		relEntry{ID: "r3", Status: "beta", Engine: "r3 engine"},
		relEntry{ID: "r4", Status: "stable", Engine: "r4 engine", Bad: true},
	)
	l, _ := releasesLibrary(t, mirror)
	list := l.Releases()
	var ids []string
	for _, r := range list {
		ids = append(ids, r.ID+":"+r.Status)
	}
	if strings.Join(ids, ",") != "legacy:stable,r2:stable,r3:beta" {
		t.Fatalf("releases: %v", ids)
	}
	if NewestStable(list) != "r2" {
		t.Fatalf("newest stable: %q", NewestStable(list))
	}
	if l2 := New(Config{LibraryDir: t.TempDir()}); l2.Releases() != nil {
		t.Fatal("no mirror: releases are not used")
	}
}

func TestEngineEscapingTheMirrorIsNotServed(t *testing.T) {
	mirror := releasesMirror(t, relEntry{ID: "r1", Status: "stable", Engine: "e"})
	outside := filepath.Join(t.TempDir(), "evil.js")
	writeFile(t, outside, "evil")
	if err := os.Symlink(outside, filepath.Join(mirror, "evil.js")); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]any{"types": map[string]any{"design": map[string]any{"releases": []map[string]string{
		{"release": "r1", "status": "stable", "engine": "upstream/design/r1/dc-runtime.js", "engineSha256": sha("e")},
		{"release": "x", "status": "stable", "engine": "evil.js", "engineSha256": sha("evil")},
		{"release": "y", "status": "stable", "engine": "../outside.js", "engineSha256": sha("evil")},
		{"release": "../z", "status": "stable", "engine": "upstream/design/r1/dc-runtime.js", "engineSha256": sha("e")},
	}}}})
	writeFile(t, filepath.Join(mirror, ReleasesFile), string(b))
	l, _ := releasesLibrary(t, mirror)
	if list := l.Releases(); len(list) != 1 || list[0].ID != "r1" {
		t.Fatalf("only the confined release: %+v", list)
	}
}

func TestPinnedProjectGetsItsReleasesEngine(t *testing.T) {
	mirror := releasesMirror(t,
		relEntry{ID: "legacy", Status: "stable", Engine: "legacy engine"},
		relEntry{ID: "r2", Status: "stable", Engine: "r2 engine"},
	)
	l, root := releasesLibrary(t, mirror)

	if w := libGet(l, "/a/p1/p/support.js"); w.Body.String() != "/* own engine */" {
		t.Fatalf("no meta/: the project's own engine, got %q", w.Body.String())
	}
	if _, err := l.SetProjectRelease("p1", "r2"); err == nil || !IsReleaseError(err) || !strings.Contains(err.Error(), "design migrate") {
		t.Fatalf("no meta/: migrate first, got %v", err)
	}
	writeFile(t, filepath.Join(root, "p1", MetaDirname, MetaProjectFile), `{"v":1,"title":"P1"}`)
	if w := libGet(l, "/a/p1/p/support.js"); w.Body.String() != "legacy engine" {
		t.Fatalf("meta/ naming no release: the mirror's legacy engine, got %q", w.Body.String())
	}
	if _, err := l.SetProjectRelease("p1", "r2"); err != nil {
		t.Fatal(err)
	}
	if w := libGet(l, "/a/p1/p/support.js"); w.Code != 200 || w.Body.String() != "r2 engine" {
		t.Fatalf("pinned: %d %q", w.Code, w.Body.String())
	}
	if got, _ := os.ReadFile(filepath.Join(root, "p1", "project", "support.js")); string(got) != "/* own engine */" {
		t.Fatal("the project's files are never changed")
	}
	// The board's address of support.js carries the engine's version, so a browser holding the old engine for good
	// asks again after the move.
	page := libGet(l, "/a/p1/p/Main.dc.html").Body.String()
	m := regexp.MustCompile(`support\.js\?tkv=([^"]+)`).FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no version on support.js: %s", page)
	}
	st, _ := os.Stat(filepath.Join(mirror, "upstream", "design", "r2", "dc-runtime.js"))
	if m[1] != FileVersion(st) {
		t.Fatalf("version %q is not the engine's %q", m[1], FileVersion(st))
	}
	if w := libGet(l, "/a/p1/p/support.js?tkv="+m[1]); w.Header().Get("Cache-Control") != immutable {
		t.Fatalf("the versioned engine is kept: %q", w.Header().Get("Cache-Control"))
	}
	// A project that keeps no copy of its own still gets the engine.
	_ = os.Remove(filepath.Join(root, "p1", "project", "support.js"))
	if w := libGet(l, "/a/p1/p/support.js"); w.Body.String() != "r2 engine" {
		t.Fatalf("no own copy: %q", w.Body.String())
	}
	// A release the mirror no longer has: the own file again (here none, so 404).
	writeFile(t, filepath.Join(root, "p1", MetaDirname, MetaProjectFile), `{"v":1,"release":"gone"}`)
	if w := libGet(l, "/a/p1/p/support.js"); w.Code != 404 {
		t.Fatalf("unknown release, no own copy: %d", w.Code)
	}
}

func TestSetProjectReleaseKeepsMetaAndRefusesBeta(t *testing.T) {
	mirror := releasesMirror(t,
		relEntry{ID: "legacy", Status: "stable", Engine: "a"},
		relEntry{ID: "r2", Status: "stable", Engine: "b"},
		relEntry{ID: "r3", Status: "beta", Engine: "c"},
	)
	l, root := releasesLibrary(t, mirror)
	p := l.ProjectReleaseOf("p1", l.Releases())
	if p.Current != LegacyRelease || p.Pinned || p.Upgrade == nil || *p.Upgrade != "r2" {
		t.Fatalf("no meta/: %+v", p)
	}
	writeFile(t, filepath.Join(root, "p1", MetaDirname, MetaProjectFile), `{"v":1,"title":"P","kits":[{"name":"ds","version":"1.2.0"}],"tailwind":true}`)
	p = l.ProjectReleaseOf("p1", l.Releases())
	if p.Current != LegacyRelease || !p.Pinned || p.Upgrade == nil || *p.Upgrade != "r2" {
		t.Fatalf("before: %+v", p)
	}
	if _, err := l.SetProjectRelease("p1", "r3"); err == nil || !IsReleaseError(err) {
		t.Fatalf("beta refused: %v", err)
	}
	if _, err := l.SetProjectRelease("p1", "nope"); err == nil || !IsReleaseError(err) {
		t.Fatalf("unknown refused: %v", err)
	}
	if _, err := l.SetProjectRelease("ghost", "r2"); err == nil || !IsReleaseError(err) {
		t.Fatalf("no project: %v", err)
	}
	p, err := l.SetProjectRelease("p1", "r2")
	if err != nil {
		t.Fatal(err)
	}
	if p.Current != "r2" || !p.Pinned || p.Upgrade != nil || p.Skill == nil || !strings.HasSuffix(*p.Skill, "skills/design-r2") {
		t.Fatalf("after: %+v", p)
	}
	m := l.ReadModel("p1")
	if m == nil || m.Meta.Release != "r2" || m.Meta.Title != "P" || len(m.Meta.Kits) != 1 || !m.Meta.Tailwind {
		t.Fatalf("the rest of meta/project.json is kept: %+v", m)
	}
}

func TestNewProjectStartsOnNewestStable(t *testing.T) {
	mirror := releasesMirror(t,
		relEntry{ID: "legacy", Status: "stable", Engine: "a"},
		relEntry{ID: "r2", Status: "stable", Engine: "b"},
		relEntry{ID: "r3", Status: "beta", Engine: "c"},
	)
	l, root := releasesLibrary(t, mirror)
	writeFile(t, filepath.Join(root, TemplateDirname, ArtifactFile), `{"v":1,"title":"Template"}`)
	writeFile(t, filepath.Join(root, TemplateDirname, "project", "support.js"), "own")
	writeFile(t, filepath.Join(root, TemplateDirname, MetaDirname, MetaProjectFile), `{"v":1,"tailwind":true,"release":"legacy"}`)

	p, err := l.CreateProject("Новый")
	if err != nil {
		t.Fatal(err)
	}
	if got := l.PinnedRelease(p.ID); got != "r2" {
		t.Fatalf("new project's release: %q", got)
	}
	if m := l.ReadModel(p.ID); m == nil || !m.Meta.Tailwind {
		t.Fatalf("the template's meta is kept: %+v", m)
	}
	if err := l.SetNewProjectsRelease("r3"); err == nil {
		t.Fatal("beta refused for new projects")
	}
	if err := l.SetNewProjectsRelease("legacy"); err != nil {
		t.Fatal(err)
	}
	p2, err := l.CreateProject("Второй")
	if err != nil {
		t.Fatal(err)
	}
	if got := l.PinnedRelease(p2.ID); got != "legacy" {
		t.Fatalf("chosen release: %q", got)
	}
	if err := l.SetNewProjectsRelease(""); err != nil || l.NewProjectsRelease() != "r2" {
		t.Fatalf("back to newest: %v %q", err, l.NewProjectsRelease())
	}

	// Without a mirror a new project keeps the template's release.
	plain := New(Config{LibraryDir: root})
	t.Cleanup(plain.StopWatching)
	p3, err := plain.CreateProject("Третий")
	if err != nil {
		t.Fatal(err)
	}
	if got := plain.PinnedRelease(p3.ID); got != LegacyRelease {
		t.Fatalf("no mirror: %q", got)
	}
}

func TestReleasesRoute(t *testing.T) {
	mirror := releasesMirror(t,
		relEntry{ID: "legacy", Status: "stable", Engine: "a"},
		relEntry{ID: "r2", Status: "stable", Engine: "b"},
	)
	l, _ := releasesLibrary(t, mirror)
	a := &API{L: l, Session: func(*http.Request) bool { return true }, CLIToken: func(*http.Request) bool { return false },
		APIRefusal: func(*http.Request) (int, any, http.Header, bool) { return 0, nil, nil, false }}
	call := func(method, url, body string) (int, map[string]any) {
		var rd io.Reader
		if body != "" {
			rd = strings.NewReader(body)
		}
		r := httptest.NewRequest(method, url, rd)
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		ans := a.Releases(r)
		var out map[string]any
		raw, _ := json.Marshal(ans.Body)
		if b, ok := ans.Body.([]byte); ok {
			raw = b
		}
		_ = json.Unmarshal(raw, &out)
		return ans.Status, out
	}
	status, out := call("GET", "/x?artifact=p1", "")
	if status != 200 || out["configured"] != true || out["newest"] != "r2" {
		t.Fatalf("GET: %d %v", status, out)
	}
	if proj := out["project"].(map[string]any); proj["current"] != "legacy" || proj["upgrade"] != "r2" {
		t.Fatalf("project: %v", proj)
	}
	if status, _ := call("POST", "/x", `{"artifact":"p1","release":"nope"}`); status != 422 {
		t.Fatalf("unknown release: %d", status)
	}
	if status, _ := call("POST", "/x", `{"artifact":"p1"}`); status != 400 {
		t.Fatalf("no release: %d", status)
	}
	writeFile(t, filepath.Join(l.ArtifactDir("p1"), MetaDirname, MetaProjectFile), `{"v":1}`)
	status, out = call("POST", "/x", `{"artifact":"p1","release":"r2"}`)
	if proj, _ := out["project"].(map[string]any); status != 200 || proj["current"] != "r2" || proj["upgrade"] != nil {
		t.Fatalf("upgrade: %d %v", status, out)
	}
	status, out = call("POST", "/x", `{"newProjects":"legacy"}`)
	if status != 200 || out["newProjects"] != "legacy" || out["chosen"] != "legacy" {
		t.Fatalf("new projects: %d %v", status, out)
	}
	status, out = call("POST", "/x", `{"newProjects":null}`)
	if status != 200 || out["newProjects"] != "r2" || out["chosen"] != nil {
		t.Fatalf("new projects follow newest: %d %v", status, out)
	}

	// The listing carries each project's release only when releases are used.
	arts := l.ListArtifacts()
	l.AttachReleases(arts)
	if arts[0].Release == nil || arts[0].Release.Current != "r2" {
		t.Fatalf("listing: %+v", arts[0].Release)
	}
	plain := New(Config{LibraryDir: t.TempDir()})
	b := &API{L: plain, Session: a.Session, CLIToken: a.CLIToken, APIRefusal: a.APIRefusal}
	ans := b.Releases(httptest.NewRequest("GET", "/x", nil))
	raw, _ := json.Marshal(ans.Body)
	if bb, ok := ans.Body.([]byte); ok {
		raw = bb
	}
	if !bytes.Contains(raw, []byte(`"configured":false`)) {
		t.Fatalf("no mirror: %s", raw)
	}
}
