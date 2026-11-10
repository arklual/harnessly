package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/datamove"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

type memStore map[string]bool

func (s memStore) HasBlob(_ context.Context, kind, sha string) (bool, error) {
	return s[kind+"/"+sha], nil
}
func (s memStore) PutBlob(_ context.Context, kind, sha string, _ []byte) error {
	s[kind+"/"+sha] = true
	return nil
}

// HRN-883: `harnessly migrate` and the page read and steer the move through the runtime; without a hub there is none.
func TestMoveRoutes(t *testing.T) {
	d := deps{SessionOK: func(r *http.Request) bool { return r.Header.Get("X-Session") == "1" }}
	routes := moveRoutes(d)
	call := func(method, path, body string, session bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if session {
			r.Header.Set("X-Session", "1")
		}
		w := httptest.NewRecorder()
		for _, rt := range routes {
			if rt.Match(r) {
				rt.Handler.ServeHTTP(w, r)
				return w
			}
		}
		t.Fatalf("no route for %s", path)
		return nil
	}
	page := platformAPI + "/machine/migration"
	moving.Store(nil)
	if w := call("GET", page, "", false); w.Code != http.StatusUnauthorized {
		t.Fatalf("no session: %d", w.Code)
	}
	if w := call("GET", "/api/cli/migrate", "", false); w.Code != http.StatusForbidden {
		t.Fatalf("no CLI token: %d", w.Code)
	}
	w := call("GET", page, "", true)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "no move on this machine") {
		t.Fatalf("no move: %d %s", w.Code, w.Body)
	}

	data := t.TempDir()
	up := filepath.Join(data, "v2", "uploads")
	if err := os.MkdirAll(up, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(up, "up_1"), []byte("png"), 0o644)
	_ = os.WriteFile(filepath.Join(up, "up_1.json"), []byte(`{"id":"up_1","name":"a.png","mime":"image/png"}`), 0o644)
	_ = os.WriteFile(filepath.Join(up, "up_2.json"), []byte(`{"id":"up_2","name":"b.png","mime":"image/png"}`), 0o644)
	m := datamove.New(datamove.Options{Version: "test", StateFile: filepath.Join(data, "migrations", "m.json"),
		Sources: []datamove.Source{datamove.Uploads{Dir: up}}, Target: memStore{}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	moving.Store(m)
	t.Cleanup(func() { moving.Store(nil) })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); m.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done })

	var st struct {
		Total, Verified, Failed int64
		Failures                []hubproto.MigrationObject
	}
	for until := time.Now().Add(10 * time.Second); st.Failed == 0 && time.Now().Before(until); time.Sleep(10 * time.Millisecond) {
		w = call("GET", "/agents"+page, "", true)
		_ = json.Unmarshal(w.Body.Bytes(), &st)
	}
	if st.Total != 2 || st.Verified != 1 || st.Failed != 1 || st.Failures[0].Code != datamove.CodeGone {
		t.Fatalf("status = %+v (%s)", st, w.Body)
	}
	if w := call("POST", page+"/verify", "", true); w.Code != http.StatusNotFound {
		t.Fatalf("the page verified: %d", w.Code)
	}
	if w := call("POST", page+"/skip", "{}", true); w.Code != http.StatusBadRequest {
		t.Fatalf("a skip of nothing named: %d", w.Code)
	}
	w = call("POST", page+"/skip", `{"objects":[{"kind":"uploads","object":"up_2"}]}`, true)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"marked":1`) {
		t.Fatalf("skip: %d %s", w.Code, w.Body)
	}
}
