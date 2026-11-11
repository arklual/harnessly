package designview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/filestore"
)

// memRoom is a room of several revisions of one project held in memory.
type memRoom struct {
	mu    sync.Mutex
	revs  map[int64]map[string]string // rev -> path under the project -> contents
	head  int64
	blobs map[string]string // sha -> contents
	reads int
}

func newMemRoom() *memRoom {
	return &memRoom{revs: map[int64]map[string]string{}, blobs: map[string]string{}}
}

// commit makes the files a new revision.
func (r *memRoom) commit(files map[string]string) int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.head++
	r.revs[r.head] = files
	return r.head
}

func shaOf(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func (r *memRoom) HistoryUnder(context.Context, string, int) ([]filestore.Entry, error) {
	return []filestore.Entry{{Rev: r.head}}, nil
}

func (r *memRoom) Snapshot(_ context.Context, prefix string, at int64) (filestore.Snapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := filestore.Snapshot{At: at}
	for p, c := range r.revs[at] {
		r.blobs[shaOf(c)] = c
		s.Entries = append(s.Entries, filestore.Entry{Path: prefix + p, Sha: shaOf(c), Size: int64(len(c)), Rev: at})
	}
	return s, nil
}

func (r *memRoom) OpenBlob(_ context.Context, sha string) (io.ReadCloser, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reads++
	return io.NopCloser(strings.NewReader(r.blobs[sha])), nil
}

func deckFiles(slide2 string) map[string]string {
	return map[string]string{
		"artifact.json":            `{"v":1,"title":"Deck","boards":[{"id":"01KTDL00000000000000000000","title":"One","path":"one.dc.html"},{"id":"01KTDL00100000000000000000","title":"Two","path":"two.dc.html"}]}`,
		"project/one.dc.html":      "<main class=\"board\">one</main>",
		"project/two.dc.html":      slide2,
		"project/ds/kit/theme.css": "body{margin:0}",
	}
}

func liveView(t *testing.T, rooms map[string]*memRoom, live int) *View {
	t.Helper()
	return New(Options{
		Rooms: func(_ context.Context, space string) (Room, error) { return rooms[space], nil },
		Dir:   t.TempDir(), Poll: time.Millisecond, Live: live,
	})
}

func read(t *testing.T, v *View, space, project string, at int64, pinned bool, segs ...string) (int, string) {
	t.Helper()
	req := httptest.NewRequest("GET", "/"+strings.Join(segs, "/"), nil)
	w := httptest.NewRecorder()
	if err := v.Serve(w, req, space, project, at, segs, nil, pinned); err != nil {
		t.Fatal(err)
	}
	return w.Code, w.Body.String()
}

const proj = "kit-live-test"

// A new revision reaches the head read by the files that changed: the one changed shows, the added one shows, the
// removed one is gone, and the contents that did not change are not fetched again.
func TestLiveBringsAProjectUpToARevision(t *testing.T) {
	room := newMemRoom()
	r1 := room.commit(deckFiles("<main class=\"board\">two</main>"))
	v := liveView(t, map[string]*memRoom{"s": room}, 12)

	if code, body := read(t, v, "s", proj, r1, false, "two.dc.html"); code != 200 || !strings.Contains(body, ">two<") {
		t.Fatalf("rev 1: %d %q", code, body)
	}
	first := v.lives["s/"+proj]
	if first == nil {
		t.Fatal("no live entry after the first read")
	}
	reads := room.reads

	files := deckFiles("<main class=\"board\">two, edited</main>")
	files["project/three.dc.html"] = "<main class=\"board\">three</main>"
	delete(files, "project/ds/kit/theme.css")
	r2 := room.commit(files)
	if code, body := read(t, v, "s", proj, r2, false, "two.dc.html"); code != 200 || !strings.Contains(body, "edited") {
		t.Fatalf("rev 2 changed file: %d %q", code, body)
	}
	if code, _ := read(t, v, "s", proj, r2, false, "three.dc.html"); code != 200 {
		t.Fatalf("rev 2 added file: %d", code)
	}
	if code, _ := read(t, v, "s", proj, r2, false, "ds", "kit", "theme.css"); code != 404 {
		t.Fatalf("rev 2 removed file: %d", code)
	}
	if code, body := read(t, v, "s", proj, r2, false, "one.dc.html"); code != 200 || !strings.Contains(body, ">one<") {
		t.Fatalf("rev 2 unchanged file: %d %q", code, body)
	}
	if v.lives["s/"+proj] != first || first.api == nil {
		t.Fatal("the library was made anew for the new revision")
	}
	if got := room.reads - reads; got != 2 {
		t.Fatalf("%d contents fetched for a revision that changed 2 files", got)
	}
	// A read of the old version (a pinned link) still sees that version.
	if code, body := read(t, v, "s", proj, r1, true, "two.dc.html"); code != 200 || strings.Contains(body, "edited") {
		t.Fatalf("pinned rev 1: %d %q", code, body)
	}
	// And a head read that asks for an older revision than the folder's does not drag the folder back.
	if _, body := read(t, v, "s", proj, r1, false, "two.dc.html"); strings.Contains(body, "edited") {
		t.Fatalf("an old head read: %q", body)
	}
	if _, body := read(t, v, "s", proj, r2, false, "two.dc.html"); !strings.Contains(body, "edited") {
		t.Fatalf("the folder went back: %q", body)
	}
}

// Two spaces with a project of one id keep two folders: nothing of one is answered to the other.
func TestLiveIsKeptPerSpace(t *testing.T) {
	a, b := newMemRoom(), newMemRoom()
	ra := a.commit(deckFiles("<main class=\"board\">space A</main>"))
	rb := b.commit(deckFiles("<main class=\"board\">space B</main>"))
	v := liveView(t, map[string]*memRoom{"a": a, "b": b}, 12)
	for i := 0; i < 2; i++ {
		if _, body := read(t, v, "a", proj, ra, false, "two.dc.html"); !strings.Contains(body, "space A") {
			t.Fatalf("a: %q", body)
		}
		if _, body := read(t, v, "b", proj, rb, false, "two.dc.html"); !strings.Contains(body, "space B") {
			t.Fatalf("b: %q", body)
		}
	}
	if len(v.lives) != 2 {
		t.Fatalf("%d live entries, want 2", len(v.lives))
	}
}

// The projects kept are bounded: the least recently used goes with its folder.
func TestLiveEvictsTheLeastRecentlyUsed(t *testing.T) {
	rooms := map[string]*memRoom{}
	revs := map[string]int64{}
	for _, s := range []string{"a", "b", "c"} {
		rooms[s] = newMemRoom()
		revs[s] = rooms[s].commit(deckFiles("<main class=\"board\">" + s + "</main>"))
	}
	v := liveView(t, rooms, 2)
	for _, s := range []string{"a", "b", "a", "c"} {
		if code, _ := read(t, v, s, proj, revs[s], false, "two.dc.html"); code != 200 {
			t.Fatalf("%s: %d", s, code)
		}
	}
	// b was the least recently used when c came.
	v.mu.Lock()
	kept := len(v.lives)
	_, hasA := v.lives["a/"+proj]
	_, hasB := v.lives["b/"+proj]
	v.mu.Unlock()
	if kept != 2 || !hasA || hasB {
		t.Fatalf("kept %d, a=%v b=%v", kept, hasA, hasB)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(v.o.Dir, "live", "b", proj)); os.IsNotExist(err) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the folder of the project let go is still there")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Readers going on while revisions come in see a whole revision each time, never a mixture or an error.
func TestLiveReadersAcrossRevisions(t *testing.T) {
	room := newMemRoom()
	rev := room.commit(deckFiles("<main class=\"board\">v1</main>"))
	v := liveView(t, map[string]*memRoom{"s": room}, 12)
	read(t, v, "s", proj, rev, false, "two.dc.html")
	var wg sync.WaitGroup
	stop := make(chan struct{})
	var mu sync.Mutex
	head := rev
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				mu.Lock()
				at := head
				mu.Unlock()
				req := httptest.NewRequest("GET", "/two.dc.html", nil)
				w := httptest.NewRecorder()
				if err := v.Serve(w, req, "s", proj, at, []string{"two.dc.html"}, nil, false); err != nil || w.Code != 200 {
					t.Errorf("read at %d: %v %d", at, err, w.Code)
					return
				}
			}
		}()
	}
	for i := 2; i <= 6; i++ {
		next := room.commit(deckFiles("<main class=\"board\">v" + string(rune('0'+i)) + "</main>"))
		mu.Lock()
		head = next
		mu.Unlock()
		time.Sleep(15 * time.Millisecond)
	}
	close(stop)
	wg.Wait()
}

// stuckWriter is a guest that does not take its answer: the write waits until released.
type stuckWriter struct {
	h       http.Header
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (w *stuckWriter) Header() http.Header { return w.h }
func (w *stuckWriter) WriteHeader(int)     {}
func (w *stuckWriter) Write(b []byte) (int, error) {
	w.once.Do(func() { close(w.started) })
	<-w.release
	return len(b), nil
}

// A reader stuck in its answer does not keep the next revision from the others: they get it in bounded time, from the
// revision's own folder, and the live folder catches up once the reader is gone.
func TestLiveSlowReaderDoesNotHoldBackTheProject(t *testing.T) {
	room := newMemRoom()
	r1 := room.commit(deckFiles("<main class=\"board\">v1</main>"))
	v := New(Options{
		Rooms: func(context.Context, string) (Room, error) { return room, nil },
		Dir:   t.TempDir(), Poll: time.Millisecond, Live: 12, BusyWait: 100 * time.Millisecond,
	})
	read(t, v, "s", proj, r1, false, "two.dc.html")

	stuck := &stuckWriter{h: http.Header{}, started: make(chan struct{}), release: make(chan struct{})}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = v.Serve(stuck, httptest.NewRequest("GET", "/two.dc.html", nil), "s", proj, r1, []string{"two.dc.html"}, nil, false)
	}()
	select {
	case <-stuck.started:
	case <-time.After(5 * time.Second):
		t.Fatal("the slow reader never started")
	}

	r2 := room.commit(deckFiles("<main class=\"board\">v2</main>"))
	for i := 0; i < 3; i++ {
		start := time.Now()
		// Another reader, and one on the old revision, both go on while the first is stuck.
		if _, body := read(t, v, "s", proj, r2, false, "two.dc.html"); !strings.Contains(body, "v2") {
			t.Fatalf("new revision not served: %q", body)
		}
		if _, body := read(t, v, "s", proj, r1, false, "two.dc.html"); !strings.Contains(body, "v1") {
			t.Fatalf("old revision not served: %q", body)
		}
		if took := time.Since(start); took > 3*time.Second {
			t.Fatalf("readers waited %v behind a stuck one", took)
		}
	}
	close(stuck.release)
	<-done
	if _, body := read(t, v, "s", proj, r2, false, "two.dc.html"); !strings.Contains(body, "v2") {
		t.Fatalf("after the slow reader: %q", body)
	}
	if l := v.lives["s/"+proj]; l == nil || l.rev != r2 {
		t.Fatal("the live folder did not catch up")
	}
}
