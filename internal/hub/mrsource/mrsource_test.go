package mrsource

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

func open(t *testing.T) store.Store {
	t.Helper()
	db, err := store.Open(context.Background(), "sqlite://"+filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db.S()
}

func mr(iid, project, created, merged string) string {
	return `{"iid":` + iid + `,"title":"T","web_url":"https://Git.Example.com/` + project + `/-/merge_requests/` + iid +
		`","created_at":"` + created + `","merged_at":"` + merged + `","author":{"username":"bob"},"references":{"full":"` + project + `!` + iid + `"}}`
}

func TestPollPagesAndStores(t *testing.T) {
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.RequestURI())
		if r.Header.Get("PRIVATE-TOKEN") != "sekret" {
			w.WriteHeader(401)
			return
		}
		if r.URL.Path != "/api/v4/groups/team%2Fsub/merge_requests" && r.URL.EscapedPath() != "/api/v4/groups/team%2Fsub/merge_requests" {
			t.Errorf("path %s", r.URL.EscapedPath())
		}
		switch r.URL.Query().Get("page") {
		case "1":
			w.Header().Set("X-Next-Page", "2")
			_, _ = io.WriteString(w, "["+mr("1", "team/sub/app", "2026-10-30T10:00:00Z", "2026-10-30T12:00:00Z")+"]")
		default:
			_, _ = io.WriteString(w, "["+mr("2", "team/sub/app", "2026-10-31T10:00:00Z", "2026-10-31T11:00:00Z")+",{\"iid\":3,\"web_url\":\"\"}]")
		}
	}))
	defer srv.Close()
	st := open(t)
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	p := New(srv.URL, []string{"team/sub"}, "sekret", st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	p.Now = func() time.Time { return now }
	if err := p.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(asked) != 2 || !strings.Contains(asked[0], "state=merged") || !strings.Contains(asked[0], "updated_after=2026-09-04T12%3A00%3A00Z") {
		t.Errorf("first run asks for the last 60 days: %v", asked)
	}
	newest, err := st.NewestMergedAt(context.Background())
	if err != nil || newest != time.Date(2026, 10, 31, 11, 0, 0, 0, time.UTC).UnixMilli() {
		t.Fatalf("newest %d %v", newest, err)
	}
	// The next run starts a day before the newest merge.
	asked = nil
	if err := p.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(asked) == 0 || !strings.Contains(asked[0], "updated_after=2026-10-30T11%3A00%3A00Z") {
		t.Errorf("later run: %v", asked)
	}
}

func TestPollKeepsTokenOutOfLogs(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(403)
	}))
	defer srv.Close()
	var buf bytes.Buffer
	p := New(srv.URL, []string{"g"}, "sekret", open(t), slog.New(slog.NewTextHandler(&buf, nil)))
	err := p.Poll(context.Background())
	if err == nil || calls.Load() != 1 {
		t.Fatalf("a refusal is not retried: %v after %d calls", err, calls.Load())
	}
	if !strings.Contains(buf.String(), "level=WARN") || strings.Contains(buf.String(), "sekret") || strings.Contains(err.Error(), "sekret") {
		t.Errorf("log %q", buf.String())
	}
	srv.Close()
	err = p.Poll(context.Background())
	if err == nil || strings.Contains(err.Error(), "sekret") {
		t.Errorf("connection error: %v", err)
	}
}

func TestOffWithoutSource(t *testing.T) {
	if New("", []string{"g"}, "t", open(t), nil) != nil || New("http://x", nil, "t", open(t), nil) != nil || New("http://x", []string{"g"}, "", open(t), nil) != nil {
		t.Error("a poller without a full source")
	}
}
