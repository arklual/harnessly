package notices

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func open(t *testing.T, file string) (*sql.DB, *Service) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+file+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := EnsureSchema(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return db, New(NewSQL(Wrap(db, false)))
}

func item(key, chat string, at int64) Item {
	return Item{Key: key, Chat: chat, At: at, Doc: json.RawMessage(`{"key":"` + key + `","chatId":"` + chat + `","text":"hi"}`)}
}

// A notice and its settings live through a restart: the same file opened again by a new process.
func TestSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	file := filepath.Join(t.TempDir(), "n.db")
	db, svc := open(t, file)
	if fresh, err := svc.Add(ctx, "p1", item("k1", "c1", 10), ""); err != nil || !fresh {
		t.Fatalf("add: %v %v", fresh, err)
	}
	if fresh, _ := svc.Add(ctx, "p1", item("k1", "c1", 10), ""); fresh {
		t.Fatal("the same key twice is no news")
	}
	if _, err := svc.Add(ctx, "p1", item("k2", "c2", 20), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Read(ctx, "p1", ReadReq{Keys: []string{"k2"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveSettings(ctx, "p1", json.RawMessage(`{"sound":false}`), 0, ""); err != nil {
		t.Fatal(err)
	}
	db.Close()

	_, again := open(t, file)
	st, err := again.State(ctx, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Items) != 1 || st.Items[0].Key != "k1" {
		t.Fatalf("items after restart: %+v", st.Items)
	}
	if len(st.Reads) != 1 || st.Reads[0].Key != "k2" {
		t.Fatalf("reads after restart: %+v", st.Reads)
	}
	if st.Settings.Rev != 1 || string(st.Settings.Doc) != `{"sound":false}` {
		t.Fatalf("settings: %+v", st.Settings)
	}
	other, _ := again.State(ctx, "p2")
	if len(other.Items) != 0 {
		t.Fatal("another person sees nothing")
	}
}

func TestSettingsConflictAndReadBeforeItem(t *testing.T) {
	ctx := context.Background()
	_, svc := open(t, filepath.Join(t.TempDir(), "n.db"))
	if _, err := svc.SaveSettings(ctx, "p", json.RawMessage(`{}`), 0, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveSettings(ctx, "p", json.RawMessage(`{}`), 0, ""); err != ErrConflict {
		t.Fatalf("stale revision: %v", err)
	}
	// Device B reads a key before device A's item arrives: the item then never shows.
	if _, err := svc.Read(ctx, "p", ReadReq{Keys: []string{"late"}}); err != nil {
		t.Fatal(err)
	}
	if fresh, _ := svc.Add(ctx, "p", item("late", "c", 1), ""); fresh {
		t.Fatal("an item read before it came is not new")
	}
	if st, _ := svc.State(ctx, "p"); len(st.Items) != 0 {
		t.Fatalf("shown: %+v", st.Items)
	}
}

// A read on device A reaches device B's stream, and B's list no longer has it.
func TestReadOnOneDeviceReachesTheOther(t *testing.T) {
	ctx := context.Background()
	_, svc := open(t, filepath.Join(t.TempDir(), "n.db"))
	h := svc.Handler(func(r *http.Request) (string, bool) { return r.URL.Query().Get("person"), true })
	srv := httptest.NewServer(h)
	defer srv.Close()

	if _, err := svc.Add(ctx, "p", item("k1", "c1", 5), ""); err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/stream?person=p", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatal(ct)
	}
	lines := make(chan string, 16)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	waitFor := func(prefix string) string {
		t.Helper()
		for {
			select {
			case l, ok := <-lines:
				if !ok {
					t.Fatal("stream closed")
				}
				if strings.HasPrefix(l, prefix) {
					return l
				}
			case <-time.After(3 * time.Second):
				t.Fatalf("no %q on the stream", prefix)
			}
		}
	}
	waitFor("event: hello")

	// Device A reads.
	post, err := http.Post(srv.URL+"/read?person=p", "application/json", strings.NewReader(`{"keys":["k1"],"from":"A"}`))
	if err != nil {
		t.Fatal(err)
	}
	post.Body.Close()
	waitFor("event: read")
	line := waitFor("data: ")
	if !strings.Contains(line, `"k1"`) || !strings.Contains(line, `"from":"A"`) {
		t.Fatal(line)
	}

	// Device B opens: nothing unread, and the read is listed.
	get, err := http.Get(srv.URL + "/?person=p")
	if err != nil {
		t.Fatal(err)
	}
	defer get.Body.Close()
	var st State
	if err = json.NewDecoder(get.Body).Decode(&st); err != nil {
		t.Fatal(err)
	}
	if len(st.Items) != 0 || len(st.Reads) != 1 {
		t.Fatalf("B sees %+v", st)
	}
}
