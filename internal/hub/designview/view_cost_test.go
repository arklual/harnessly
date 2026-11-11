package designview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/filestore"
)

// dirRoom is a store room over a folder on disk: the revisions it serves are the folder's files, each revision with the
// files it was told changed (a stand-in of the hub's store for the cost of a first look at a project).
type dirRoom struct {
	mu      sync.Mutex
	prefix  string
	files   map[string]filestore.Entry // path under the project -> entry at the head
	blobs   map[string]string          // sha -> file on disk
	head    int64
	history map[int64]map[string]filestore.Entry
	opened  int
}

func newDirRoom(t testing.TB, project, dir string) *dirRoom {
	t.Helper()
	r := &dirRoom{prefix: project + "/", files: map[string]filestore.Entry{}, blobs: map[string]string{}, history: map[int64]map[string]filestore.Entry{}}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		r.put(t, filepath.ToSlash(rel), p)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	r.commit()
	return r
}

func (r *dirRoom) put(t testing.TB, rel, file string) {
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	sha := hex.EncodeToString(sum[:])
	r.blobs[sha] = file
	r.files[rel] = filestore.Entry{Path: r.prefix + rel, Sha: sha, Size: int64(len(b))}
}

// commit makes the files as they are a revision.
func (r *dirRoom) commit() int64 {
	r.head++
	snap := map[string]filestore.Entry{}
	for k, e := range r.files {
		e.Rev = r.head
		snap[k] = e
	}
	r.history[r.head] = snap
	return r.head
}

func (r *dirRoom) HistoryUnder(_ context.Context, _ string, _ int) ([]filestore.Entry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return []filestore.Entry{{Rev: r.head}}, nil
}

func (r *dirRoom) Snapshot(_ context.Context, _ string, at int64) (filestore.Snapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := filestore.Snapshot{At: at}
	for _, e := range r.history[at] {
		s.Entries = append(s.Entries, e)
	}
	return s, nil
}

func (r *dirRoom) OpenBlob(_ context.Context, sha string) (io.ReadCloser, error) {
	r.mu.Lock()
	r.opened++
	file := r.blobs[sha]
	r.mu.Unlock()
	return os.Open(file)
}

func costView(t testing.TB, room Room, live int) *View {
	t.Helper()
	return New(Options{Rooms: func(context.Context, string) (Room, error) { return room, nil }, Dir: t.TempDir(), Poll: time.Millisecond, Live: live})
}

// serve is one guest read of a path under the project: the status and how long it took.
func serve(t testing.TB, v *View, project string, at int64, segs ...string) (int, time.Duration) {
	t.Helper()
	esc := make([]string, len(segs))
	for i, s := range segs {
		esc[i] = url.PathEscape(s)
	}
	req := httptest.NewRequest("GET", "/"+strings.Join(esc, "/"), nil)
	req.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	start := time.Now()
	if err := v.Serve(w, req, "space", project, at, segs, nil, false); err != nil {
		t.Fatal(err)
	}
	return w.Code, time.Since(start)
}

// TestRevisionCost measures what the first look at a slide costs after a new revision of a project: the slide's page and
// the three biggest files a board of it draws with (the engine, the kit's bundles), once as laid out anew for each
// revision (Live < 0, how it was) and once brought up to date (Live > 0). It reads a project folder named by
// DESIGNVIEW_COST_DIR (a copy, named as the project: nothing in it is written) and prints the times; no assertion.
func TestRevisionCost(t *testing.T) {
	dir := os.Getenv("DESIGNVIEW_COST_DIR")
	if dir == "" {
		t.Skip("DESIGNVIEW_COST_DIR names a project folder to measure")
	}
	project := filepath.Base(dir)
	for _, mode := range []struct {
		name string
		live int
	}{{"laid out anew each revision", -1}, {"brought up to date", 12}} {
		room := newDirRoom(t, project, dir)
		v := costView(t, room, mode.live)
		// The slide and the biggest files that are not slides.
		page := []string{"01 Обложка.dc.html"}
		big := biggest(room, 3)
		look := func(at int64) time.Duration {
			start := time.Now()
			for _, rel := range append([][]string{page}, big...) {
				if code, _ := serve(t, v, project, at, rel...); code != 200 {
					t.Fatalf("%v: HTTP %d", rel, code)
				}
			}
			return time.Since(start)
		}
		t.Logf("[%s] %d files, first revision: first look %v, again %v", mode.name, len(room.files), look(1), look(1))
		changed := "project/02 Содержание.dc.html"
		for i := 0; i < 4; i++ {
			b, _ := os.ReadFile(room.blobs[room.files[changed].Sha])
			f := filepath.Join(t.TempDir(), "changed")
			if err := os.WriteFile(f, append(b, []byte("\n<!-- edit "+string(rune('a'+i))+" -->\n")...), 0o600); err != nil {
				t.Fatal(err)
			}
			room.put(t, changed, f)
			rev := room.commit()
			t.Logf("[%s] revision %d: first look %v, again %v", mode.name, rev, look(rev), look(rev))
		}
		t.Logf("[%s] blobs read from the store: %d", mode.name, room.opened)
	}
}

// biggest is the n biggest files of the room that are no slide, as paths for serve.
func biggest(r *dirRoom, n int) [][]string {
	var out [][]string
	used := map[string]bool{}
	for len(out) < n {
		best, size := "", int64(-1)
		for rel, e := range r.files {
			if !used[rel] && strings.HasPrefix(rel, "project/") && !strings.HasSuffix(rel, ".dc.html") && strings.HasSuffix(rel, ".js") && e.Size > size {
				best, size = rel, e.Size
			}
		}
		if best == "" {
			break
		}
		used[best] = true
		out = append(out, strings.Split(strings.TrimPrefix(best, "project/"), "/"))
	}
	return out
}
