package memstore

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/ext"
)

func writeNote(t *testing.T, parent, entry, name, text string) {
	t.Helper()
	dir := filepath.Join(parent, entry, "memory")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFileStoreReadsAsBefore(t *testing.T) {
	parent := t.TempDir()
	writeNote(t, parent, "p1", "deploy.md", "---\nname: deploy\ndescription: how we ship\n---\n# Deploy\nUse the blue-green flow.\n")
	writeNote(t, parent, "p1", "notes.md", "plain text about pizza\n")
	s := FileStore{Parents: []string{parent}}
	ctx := context.Background()

	list, err := s.List(ctx)
	if err != nil || len(list) != 2 || list[0].Name != "deploy.md" || list[1].Name != "notes.md" {
		t.Fatalf("list: %+v %v", list, err)
	}
	n, err := s.Get(ctx, "deploy.md")
	if err != nil || n.Title != "Deploy" || n.Description != "how we ship" || !strings.Contains(n.Body, "blue-green") || n.Version == "" {
		t.Fatalf("get: %+v %v", n, err)
	}
	if _, err := s.Get(ctx, "../etc/passwd"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("bad name: %v", err)
	}
	if _, err := s.Get(ctx, "none.md"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	hits, err := s.Search(ctx, "PIZZA", 0)
	if err != nil || len(hits) != 1 || hits[0].Name != "notes.md" {
		t.Fatalf("search: %+v %v", hits, err)
	}
}

func TestFileStoreIsReadOnlyWithoutWriteDir(t *testing.T) {
	s := FileStore{Parents: []string{t.TempDir()}}
	if _, err := s.Put(context.Background(), "a.md", "x", ""); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("put: %v", err)
	}
}

func TestFileStorePutAndConflict(t *testing.T) {
	dir := t.TempDir()
	s := FileStore{Parents: []string{t.TempDir()}, WriteDir: dir}
	ctx := context.Background()
	if _, err := s.Put(ctx, "bad name", "x", ""); !errors.Is(err, ErrBadName) {
		t.Fatalf("bad: %v", err)
	}
	e, err := s.Put(ctx, "a.md", "# A\nfirst", "")
	if err != nil || e.Version == "" {
		t.Fatalf("put: %+v %v", e, err)
	}
	if _, err := s.Put(ctx, "a.md", "# A\nsecond", e.Version); err != nil {
		t.Fatalf("put with current version: %v", err)
	}
	if _, err := s.Put(ctx, "a.md", "# A\nthird", "1"); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "a.md"))
	if strings.Contains(string(raw), "third") {
		t.Fatalf("stale write went through: %s", raw)
	}
}

// fakeAdapter is an adapter of the memory protocol that keeps notes in memory.
func fakeAdapter(t *testing.T, caps []string) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	notes := map[string]Note{}
	ver := 0
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/describe", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(ext.Description{Protocol: Protocol, Name: "example-wiki", Version: "1", Capabilities: caps})
	})
	post := func(path string, f func(in map[string]any) (int, any)) {
		mux.HandleFunc("POST "+path, func(w http.ResponseWriter, r *http.Request) {
			var in map[string]any
			_ = json.NewDecoder(r.Body).Decode(&in)
			mu.Lock()
			defer mu.Unlock()
			st, out := f(in)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(st)
			_ = json.NewEncoder(w).Encode(out)
		})
	}
	post("/v1/list", func(map[string]any) (int, any) {
		var l []Entry
		for _, n := range notes {
			l = append(l, n.Entry)
		}
		return 200, map[string]any{"notes": l}
	})
	post("/v1/get", func(in map[string]any) (int, any) {
		n, ok := notes[in["name"].(string)]
		if !ok {
			return 404, map[string]string{"code": "not_found"}
		}
		return 200, n
	})
	post("/v1/put", func(in map[string]any) (int, any) {
		name := in["name"].(string)
		if cur, ok := notes[name]; ok && in["ifVersion"] != "" && in["ifVersion"] != cur.Version {
			return 409, map[string]string{"code": "conflict"}
		}
		ver++
		e := Entry{Name: name, Title: name, Version: strconv.Itoa(ver)}
		notes[name] = Note{Entry: e, Body: in["text"].(string)}
		return 200, e
	})
	post("/v1/search", func(in map[string]any) (int, any) {
		var hits []Hit
		for _, n := range notes {
			if strings.Contains(n.Body, in["query"].(string)) {
				hits = append(hits, Hit{Entry: n.Entry, Snippet: "from the adapter"})
			}
		}
		return 200, map[string]any{"hits": hits}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func registerFake(t *testing.T, caps []string) *ExtStore {
	t.Helper()
	srv := fakeAdapter(t, caps)
	c, err := ext.NewClient(ext.Config{BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	st, err := Register(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestExtStoreRoundTrip(t *testing.T) {
	st := registerFake(t, []string{"read", "write", "search"})
	ctx := context.Background()
	e, err := st.Put(ctx, "wiki.md", "release notes for example.com", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Put(ctx, "wiki.md", "again", "stale"); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflict: %v", err)
	}
	n, err := st.Get(ctx, "wiki.md")
	if err != nil || n.Body != "release notes for example.com" || n.Version != e.Version {
		t.Fatalf("get: %+v %v", n, err)
	}
	if _, err := st.Get(ctx, "no.md"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	hits, err := st.Search(ctx, "release", 5)
	if err != nil || len(hits) != 1 || hits[0].Snippet != "from the adapter" {
		t.Fatalf("search: %+v %v", hits, err)
	}
}

func TestExtStoreSkipsCapabilitiesItDoesNotList(t *testing.T) {
	st := registerFake(t, []string{"read"})
	ctx := context.Background()
	if _, err := st.Put(ctx, "a.md", "x", ""); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("put: %v", err)
	}
}

func TestRegisterRefusesOtherProtocolAndNoRead(t *testing.T) {
	srv := fakeAdapter(t, []string{"write"})
	c, _ := ext.NewClient(ext.Config{BaseURL: srv.URL})
	if _, err := Register(context.Background(), c); err == nil {
		t.Fatal("an adapter without read was accepted")
	}
}

func TestLayeredFallsBackToFiles(t *testing.T) {
	parent := t.TempDir()
	writeNote(t, parent, "p1", "local.md", "# Local\nonly on disk\n")
	st := registerFake(t, []string{"read", "write"})
	ctx := context.Background()
	l := Layered{Top: st, Base: FileStore{Parents: []string{parent}}}
	if _, err := l.Put(ctx, "remote.md", "# Remote\nin the wiki", ""); err != nil {
		t.Fatal(err)
	}
	list, err := l.List(ctx)
	if err != nil || len(list) != 2 {
		t.Fatalf("list: %+v %v", list, err)
	}
	if n, err := l.Get(ctx, "local.md"); err != nil || n.Title != "Local" {
		t.Fatalf("local: %+v %v", n, err)
	}
	if n, err := l.Get(ctx, "remote.md"); err != nil || !strings.Contains(n.Body, "wiki") {
		t.Fatalf("remote: %+v %v", n, err)
	}
	hits, err := l.Search(ctx, "disk", 5)
	if err != nil || len(hits) != 1 || hits[0].Name != "local.md" {
		t.Fatalf("search: %+v %v", hits, err)
	}
}

func TestLayeredWithUnreachableAdapterKeepsFiles(t *testing.T) {
	parent := t.TempDir()
	writeNote(t, parent, "p1", "local.md", "# Local\nbody\n")
	srv := fakeAdapter(t, []string{"read"})
	c, _ := ext.NewClient(ext.Config{BaseURL: srv.URL})
	st, err := Register(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	srv.Close()
	l := Layered{Top: st, Base: FileStore{Parents: []string{parent}}}
	if n, err := l.Get(context.Background(), "local.md"); err != nil || n.Title != "Local" {
		t.Fatalf("get: %+v %v", n, err)
	}
}

func TestLoadAdaptersSkipsOtherProtocols(t *testing.T) {
	srv := fakeAdapter(t, []string{"read"})
	f := filepath.Join(t.TempDir(), "adapters.json")
	spec := `[{"name":"example-wiki","kind":"memory","url":"` + srv.URL + `","protocol":"` + Protocol + `"},
	{"name":"other","kind":"connection","url":"http://127.0.0.1:1","protocol":"harnessly.connection/v1"}]`
	if err := os.WriteFile(f, []byte(spec), 0o600); err != nil {
		t.Fatal(err)
	}
	got, errs := LoadAdapters(context.Background(), f)
	if len(errs) != 0 || len(got) != 1 || got[0].Name() != "example-wiki" {
		t.Fatalf("%v %v", got, errs)
	}
	if got, errs := LoadAdapters(context.Background(), ""); got != nil || errs != nil {
		t.Fatal("empty path must load nothing")
	}
}
