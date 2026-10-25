package wire_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

var png = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00")

type body struct{ *bytes.Reader }

func (body) Close() error { return nil }

func newBody(b []byte) body { return body{bytes.NewReader(b)} }

func shotServer() *wire.Server {
	return &wire.Server{
		Limits: wire.DefaultLimits,
		Shot: func(_ context.Context, conv, id string) (wire.ShotFile, error) {
			if conv != "tab-a" || id != "listed" {
				return wire.ShotFile{}, wire.ErrNoShot
			}
			return wire.ShotFile{Body: newBody(png), Mime: "image/png", ModTime: time.Unix(1_800_000_000, 0)}, nil
		},
	}
}

// A picture is asked for by the id the conversation lists: the listed one comes as its bytes, any other id is not
// found, and a path in its place is no more than an id nobody lists.
func TestShotServesOnlyWhatTheConversationLists(t *testing.T) {
	srv := shotServer()
	get := func(url string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
		return rec
	}
	rec := get("/api/v2/conversations/tab-a/shot?id=listed")
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/png" || rec.Header().Get("X-Content-Type-Options") != "nosniff" ||
		rec.Body.Len() != len(png) {
		t.Fatalf("listed: %d %v %d", rec.Code, rec.Header(), rec.Body.Len())
	}
	for _, url := range []string{
		"/api/v2/conversations/tab-a/shot?id=other",
		"/api/v2/conversations/tab-a/shot?id=/etc/passwd",
		"/api/v2/conversations/tab-a/shot?id=../../etc/passwd",
		"/api/v2/conversations/tab-b/shot?id=listed",
	} {
		if rec := get(url); rec.Code != http.StatusNotFound {
			t.Fatalf("%s: %d", url, rec.Code)
		}
	}
	if rec := get("/api/v2/conversations/tab-a/shot"); rec.Code != http.StatusBadRequest {
		t.Fatalf("no id: %d", rec.Code)
	}
	srv.Shot = nil
	if rec := get("/api/v2/conversations/tab-a/shot?id=listed"); rec.Code != http.StatusNotFound {
		t.Fatalf("not served: %d", rec.Code)
	}
}
