package webpkg

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeRegistry struct {
	archive   []byte
	sums      string
	files     []pkgFile
	tagCommit string
	branch    bool
	gets      int
}

func newRegistry(archive []byte) *fakeRegistry {
	sum := sha256.Sum256(archive)
	hexSum := hex.EncodeToString(sum[:])
	by := []pipeline{{ID: 7, Ref: "v1.2.3", SHA: "abc"}}
	return &fakeRegistry{
		archive:   archive,
		sums:      hexSum + "  " + Archive + "\n",
		tagCommit: "abc",
		files: []pkgFile{
			{ID: 1, Name: Archive, Size: int64(len(archive)), SHA256: hexSum, Pipelines: by},
			{ID: 2, Name: Sums, Pipelines: by},
		},
	}
}

func (f *fakeRegistry) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("PRIVATE-TOKEN") != "t" {
		http.Error(w, "no", http.StatusUnauthorized)
		return
	}
	f.gets++
	switch p := r.URL.Path; {
	case p == "/api/packages":
		_ = json.NewEncoder(w).Encode([]pkg{{ID: 9, Name: "web-old", Version: "9.9.9"}, {ID: 5, Name: PackageName, Version: "1.2.3"}})
	case p == "/api/packages/5/package_files":
		_ = json.NewEncoder(w).Encode(f.files)
	case strings.HasPrefix(p, "/api/pipelines/"):
		_ = json.NewEncoder(w).Encode(map[string]any{"tag": !f.branch, "ref": "v1.2.3", "sha": "abc"})
	case p == "/api/repository/tags/v1.2.3":
		_, _ = w.Write([]byte(`{"commit":{"id":"` + f.tagCommit + `"}}`))
	case p == "/api/packages/generic/web/1.2.3/"+Sums:
		_, _ = w.Write([]byte(f.sums))
	case p == "/api/packages/generic/web/1.2.3/"+Archive:
		_, _ = w.Write(f.archive)
	default:
		http.NotFound(w, r)
	}
}

func source(t *testing.T, f *fakeRegistry) Source {
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return Source{API: srv.URL + "/api", Token: "t"}
}

func TestSyncTakesAVerifiedPage(t *testing.T) {
	dir := t.TempDir()
	f := newRegistry([]byte("page"))
	s := source(t, f)
	p, changed, err := s.Sync(context.Background(), dir)
	if err != nil || !changed {
		t.Fatalf("Sync = %+v %v %v", p, changed, err)
	}
	if p.Version != "1.2.3" || p.Pipeline != 7 || p.Size != 4 || p.File != "web-1.2.3.tar.gz" {
		t.Fatalf("page %+v", p)
	}
	if body, _ := os.ReadFile(filepath.Join(dir, p.File)); string(body) != "page" {
		t.Fatalf("archive %q", body)
	}
	cur, err := Current(dir)
	if err != nil || cur != p {
		t.Fatalf("Current = %+v %v", cur, err)
	}
	// The same version again: nothing is downloaded.
	before := f.gets
	if _, changed, err := s.Sync(context.Background(), dir); err != nil || changed {
		t.Fatalf("second Sync changed=%v err=%v", changed, err)
	}
	if f.gets != before+1 { // the listing only
		t.Fatalf("second Sync made %d requests, want only the listing", f.gets-before)
	}
}

func TestSyncRefuses(t *testing.T) {
	cases := map[string]func(f *fakeRegistry){
		"a file written by a branch pipeline": func(f *fakeRegistry) {
			f.files[0].Pipelines = append(f.files[0].Pipelines, pipeline{ID: 8, Ref: "feature", SHA: "abc"})
		},
		"a pipeline at another commit": func(f *fakeRegistry) { f.tagCommit = "other" },
		"a branch named like the tag":  func(f *fakeRegistry) { f.branch = true },
		"a file without a pipeline":    func(f *fakeRegistry) { f.files[1].Pipelines = nil },
		"no sums":                      func(f *fakeRegistry) { f.files = f.files[:1] },
		"sums that disagree":           func(f *fakeRegistry) { f.sums = strings.Repeat("0", 64) + "  " + Archive + "\n" },
		"an archive that is not the released one": func(f *fakeRegistry) {
			f.archive = []byte("evil")
		},
	}
	for name, spoil := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			f := newRegistry([]byte("page"))
			spoil(f)
			if _, changed, err := source(t, f).Sync(context.Background(), dir); err == nil || changed {
				t.Fatalf("Sync took it: changed=%v err=%v", changed, err)
			}
			if p, _ := Current(dir); p.Version != "" {
				t.Fatalf("serves %+v", p)
			}
			if entries, _ := os.ReadDir(dir); len(entries) != 0 {
				t.Fatalf("left %d files", len(entries))
			}
		})
	}
}

func TestCurrentWithoutAPage(t *testing.T) {
	if p, err := Current(t.TempDir()); err != nil || p.Version != "" {
		t.Fatalf("Current = %+v %v", p, err)
	}
}

func TestTokenStaysOnTheRegistryHost(t *testing.T) {
	var leaked string
	storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked = r.Header.Get("PRIVATE-TOKEN")
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(storage.Close)
	reg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, storage.URL+"/blob", http.StatusFound)
	}))
	t.Cleanup(reg.Close)
	res, err := Source{API: reg.URL, Token: "t"}.get(context.Background(), "/x")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if leaked != "" {
		t.Fatalf("the token reached another host: %q", leaked)
	}
}
