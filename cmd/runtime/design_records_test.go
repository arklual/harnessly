package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/filestore"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/storelink"
)

// a runtime without a hub writes its entities into the embedded store and the check says they are all there.
func TestRecordsReachTheEmbeddedStore(t *testing.T) {
	l, err := storelink.Open(storelink.Options{DataDir: t.TempDir(), Name: "box"})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	recs := []filestore.Record{{ID: "shop", Title: "Shop", Kind: "site", At: 1}, {ID: "kit-a", Title: "Kit", Kind: "ds", At: 2}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); startRecords(ctx, l, func() []filestore.Record { return recs }, nil, nil) }()
	deadline := time.Now().Add(20 * time.Second)
	var body string
	for time.Now().Before(deadline) {
		if recordsLive.Load() != nil {
			rr := httptest.NewRecorder()
			h := designRecordsRoutes()[0]
			req := httptest.NewRequest(http.MethodGet, platformAPI+"/design/records-check", nil)
			if h.Match(req) {
				h.Handler.ServeHTTP(rr, req)
				body = rr.Body.String()
				if strings.Contains(body, `"OK":true`) || strings.Contains(body, `"ok":true`) {
					break
				}
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	cancel()
	<-done
	if !strings.Contains(strings.ToLower(body), `"ok":true`) {
		t.Fatalf("the store lacks the entities: %s", body)
	}
}

// a document of a type an app adds is recorded with that type, so the hub lists it among the app's documents;
// the library's own projects keep the library's kind.
func TestLibraryRecordsCarryTheAppDocumentType(t *testing.T) {
	dir := t.TempDir()
	d := deps{DataDir: dir, Home: t.TempDir(), CLI: &gate.CLIToken{DataDir: dir, Headers: []string{"x-cli-token"}}}
	lib := designAPI(d).L
	t.Cleanup(lib.StopWatching)
	site, err := lib.CreateProject("Лендинг")
	if err != nil {
		t.Fatal(err)
	}
	note, err := lib.CreateProject("Мои заметки")
	if err != nil {
		t.Fatal(err)
	}
	if err := apps.Open(dir).SetDocKind(note.ID, "notes.doc"); err != nil {
		t.Fatal(err)
	}
	kinds := map[string]string{}
	for _, r := range libraryRecords(d) {
		kinds[r.ID] = r.Kind
	}
	if kinds[note.ID] != "notes.doc" {
		t.Fatalf("the app's document: %q (%v)", kinds[note.ID], kinds)
	}
	if k := kinds[site.ID]; k == "" || k == "notes.doc" {
		t.Fatalf("the site: %q", k)
	}
}
