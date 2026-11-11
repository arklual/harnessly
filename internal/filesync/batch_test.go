package filesync

import (
	"context"
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

// counted is a store behind a server that counts what it was asked and how it answered.
type counted struct {
	mu   sync.Mutex
	hits map[string]int
	// old: the store has no batch look at the blobs.
	old bool
	// locked: writes under locked/ are refused.
	locked bool
	url    string
	store  *filestore.Store
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (c *counted) n(key string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hits[key]
}

func (c *counted) set(fn func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	fn()
}

func newCounted(t *testing.T) *counted {
	t.Helper()
	c := &counted{hits: map[string]int{}}
	srvStore, err := filestore.OpenFolder(filepath.Join(t.TempDir(), "hub"), func(r *http.Request, coll string) (filestore.Access, error) {
		acc, err := authorize(r, coll)
		acc.Writes = func(p string) bool {
			c.mu.Lock()
			defer c.mu.Unlock()
			return !c.locked || !strings.HasPrefix(p, "locked/")
		}
		return acc, err
	})
	must(t, err)
	c.store, err = srvStore.Room(context.Background(), filestore.KindDesign)
	must(t, err)
	inner := srvStore.Handler()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		what := r.Method + " " + r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		if strings.Contains(r.URL.Path, "/blobs/") && !strings.HasSuffix(r.URL.Path, "/blobs/missing") {
			what = r.Method + " blob"
		}
		c.mu.Lock()
		old := c.old
		c.mu.Unlock()
		sw := &statusWriter{ResponseWriter: w, status: 200}
		if old && what == "POST missing" {
			http.NotFound(sw, r)
		} else {
			inner.ServeHTTP(sw, r)
		}
		c.mu.Lock()
		c.hits[what]++
		if what == "POST commit" {
			c.hits["commit "+http.StatusText(sw.status)]++
		}
		c.mu.Unlock()
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(srvStore.Close)
	c.url = srv.URL
	return c
}

func (c *counted) agent(t *testing.T, group func(string) string) (*Agent, string) {
	t.Helper()
	return c.agentAs(t, tokenA, group)
}

func (c *counted) agentAs(t *testing.T, token string, group func(string) string) (*Agent, string) {
	t.Helper()
	dir := t.TempDir()
	root := filepath.Join(dir, "root")
	must(t, os.MkdirAll(root, 0o755))
	a, err := New(Config{Root: root, StateDir: filepath.Join(dir, "state"), Machine: machines[token], RefusalGroup: group,
		Store: NewClient(c.url, filestore.KindDesign, staticToken(token), nil)})
	must(t, err)
	a.now = func() time.Time { return time.Date(2026, 10, 18, 21, 41, 0, 0, time.UTC) }
	t.Cleanup(a.Close)
	return a, root
}

func firstFolder(rel string) string {
	id, _, ok := strings.Cut(rel, "/")
	if !ok {
		return ""
	}
	return id
}

func TestAFolderAsksOnceWhichContentsTheStoreLacks(t *testing.T) {
	c := newCounted(t)
	a, root := c.agent(t, nil)
	for _, name := range []string{"a", "b", "c", "d"} {
		write(t, root, "admin/project/"+name+".dc.html", "<x-dc>"+name+"</x-dc>")
	}
	write(t, root, "admin/project/copy.dc.html", "<x-dc>a</x-dc>")
	must(t, a.ScanAll(ctx))
	if c.n("POST missing") != 1 || c.n("HEAD blob") != 0 {
		t.Fatalf("looks at the store: %d batch, %d single; want 1 and 0", c.n("POST missing"), c.n("HEAD blob"))
	}
	// Two files with the same contents are one upload.
	if c.n("PUT blob") != 4 || c.n("commit OK") != 5 {
		t.Fatalf("%d uploads, %d commits; want 4 and 5", c.n("PUT blob"), c.n("commit OK"))
	}
	// Contents the store already holds are not uploaded again.
	write(t, root, "admin/project/e.dc.html", "<x-dc>b</x-dc>")
	write(t, root, "admin/project/f.dc.html", "<x-dc>f</x-dc>")
	must(t, a.ScanAll(ctx))
	if c.n("POST missing") != 2 || c.n("HEAD blob") != 0 || c.n("PUT blob") != 5 || c.n("commit OK") != 7 {
		t.Fatalf("second pass: %v", c.hits)
	}
	// One changed file is one look, as before.
	write(t, root, "admin/project/g.dc.html", "<x-dc>g</x-dc>")
	must(t, a.ScanAll(ctx))
	if c.n("POST missing") != 2 || c.n("HEAD blob") != 1 || c.n("PUT blob") != 6 {
		t.Fatalf("one file: %v", c.hits)
	}
}

func TestAStoreWithoutTheBatchLookIsAskedAboutEachBlob(t *testing.T) {
	c := newCounted(t)
	c.old = true
	a, root := c.agent(t, nil)
	for _, name := range []string{"a", "b", "c"} {
		write(t, root, "admin/project/"+name+".dc.html", "<x-dc>"+name+"</x-dc>")
	}
	must(t, a.ScanAll(ctx))
	if c.n("POST missing") != 1 || c.n("HEAD blob") != 3 || c.n("PUT blob") != 3 || c.n("commit OK") != 3 {
		t.Fatalf("first pass: %v", c.hits)
	}
	// The batch look is not tried again.
	write(t, root, "admin/project/d.dc.html", "<x-dc>d</x-dc>")
	write(t, root, "admin/project/e.dc.html", "<x-dc>e</x-dc>")
	must(t, a.ScanAll(ctx))
	if c.n("POST missing") != 1 || c.n("HEAD blob") != 5 || c.n("commit OK") != 5 {
		t.Fatalf("second pass: %v", c.hits)
	}
}

func TestARefusedProjectIsAskedAboutOnce(t *testing.T) {
	c := newCounted(t)
	c.locked = true
	a, root := c.agent(t, firstFolder)
	for _, name := range []string{"a", "b", "c", "d"} {
		write(t, root, "locked/project/"+name+".dc.html", "<x-dc>"+name+"</x-dc>")
	}
	write(t, root, "open/project/a.dc.html", "<x-dc>open</x-dc>")
	must(t, a.ScanAll(ctx))
	if c.n("commit Forbidden") != 1 || c.n("commit OK") != 1 {
		t.Fatalf("first pass: %v; want one refusal for the project and the open file sent", c.hits)
	}
	// Nothing of the refused project was uploaded after the refusal, and an edit in it asks nobody.
	puts := c.n("PUT blob")
	write(t, root, "locked/project/b.dc.html", "<x-dc>b, edited</x-dc>")
	write(t, root, "locked/project/new.dc.html", "<x-dc>new</x-dc>")
	must(t, a.ScanAll(ctx))
	must(t, os.Remove(filepath.Join(root, "locked", "project", "c.dc.html")))
	must(t, a.ScanAll(ctx))
	if c.n("commit Forbidden") != 1 || c.n("PUT blob") != puts {
		t.Fatalf("after edits in the refused project: %v", c.hits)
	}
	// The files stay on the machine.
	if got := read(t, root, "locked/project/b.dc.html"); got != "<x-dc>b, edited</x-dc>" {
		t.Fatalf("the refused file: %q", got)
	}
	// What the store shows the machine changed and the role is still missing: one more refusal, not one per file.
	must(t, a.rescope(ctx, "before", "a scope the store does not name"))
	must(t, a.ScanAll(ctx))
	if c.n("commit Forbidden") != 2 {
		t.Fatalf("after a change of scope: %v", c.hits)
	}
	// The role is given: the files of the project reach the store.
	c.set(func() { c.locked = false })
	must(t, a.rescope(ctx, "before", "a scope the store does not name"))
	must(t, a.ScanAll(ctx))
	for _, name := range []string{"a", "b", "d", "new"} {
		if e, err := c.store.Current(ctx, "locked/project/"+name+".dc.html"); err != nil || e == nil || e.Deleted {
			t.Fatalf("%s did not reach the store after the role was given: %+v %v", name, e, err)
		}
	}
	if c.n("commit Forbidden") != 2 {
		t.Fatalf("after the role was given: %v", c.hits)
	}
}

func TestARefusedFileThatOnlyChangedItsTimeIsNotSentAgain(t *testing.T) {
	c := newCounted(t)
	c.locked = true
	a, root := c.agent(t, nil)
	write(t, root, "locked/project/a.dc.html", "<x-dc>a</x-dc>")
	must(t, a.ScanAll(ctx))
	if c.n("commit Forbidden") != 1 {
		t.Fatalf("first pass: %v", c.hits)
	}
	later := time.Now().Add(time.Hour)
	must(t, os.Chtimes(filepath.Join(root, "locked", "project", "a.dc.html"), later, later))
	must(t, a.ScanAll(ctx))
	if c.n("commit Forbidden") != 1 || c.n("POST commit") != 1 {
		t.Fatalf("the same contents were offered again: %v", c.hits)
	}
	// Without a group every refused file is asked about on its own.
	write(t, root, "locked/project/b.dc.html", "<x-dc>b</x-dc>")
	must(t, a.ScanAll(ctx))
	if c.n("commit Forbidden") != 2 {
		t.Fatalf("another file of the project: %v", c.hits)
	}
}

func TestPutStartsFromTheRevisionItLastSaw(t *testing.T) {
	c := newCounted(t)
	cl := NewClient(c.url, filestore.KindDesign, staticToken(tokenA), nil)
	const p = "admin/project/state.json"
	first, err := cl.Put(ctx, p, []byte(`{"v":1}`), "test")
	must(t, err)
	second, err := cl.Put(ctx, p, []byte(`{"v":2}`), "test")
	must(t, err)
	if second.Rev <= first.Rev || c.n("commit Conflict") != 0 {
		t.Fatalf("writing a path again: rev %d then %d, %d conflicts; want none", first.Rev, second.Rev, c.n("commit Conflict"))
	}
	// A process that never saw the path learns its revision from one conflict and writes on top of it.
	heads.set(c.url+"\x00"+filestore.KindDesign+"\x00"+p, 0)
	third, err := cl.Put(ctx, p, []byte(`{"v":3}`), "test")
	must(t, err)
	if third.Rev <= second.Rev || c.n("commit Conflict") != 1 {
		t.Fatalf("an unknown path: rev %d, %d conflicts; want one", third.Rev, c.n("commit Conflict"))
	}
}

func TestContentsTheFolderHoldsAreNotDownloadedAndAQuietFolderAsksNothing(t *testing.T) {
	c := newCounted(t)
	a, ra := c.agent(t, nil)
	b, rb := c.agentAs(t, tokenB, nil)
	// Two versions of a kit share most of their files.
	files := map[string]string{
		"kit/v1/tokens.css": "tokens", "kit/v1/button.js": "button", "kit/v1/readme.md": "one",
		"kit/v2/tokens.css": "tokens", "kit/v2/button.js": "button", "kit/v2/readme.md": "two",
		"kit/v3/tokens.css": "tokens", "kit/v3/button.js": "button", "kit/v3/readme.md": "two",
	}
	for rel, body := range files {
		write(t, ra, rel, body)
	}
	must(t, a.SyncOnce(ctx))
	if c.n("commit OK") != len(files) || c.n("PUT blob") != 4 {
		t.Fatalf("the sender: %v; want %d commits and 4 uploads", c.hits, len(files))
	}
	must(t, b.SyncOnce(ctx))
	for rel, body := range files {
		if got := read(t, rb, rel); got != body {
			t.Fatalf("%s on the other machine: %q, want %q", rel, got, body)
		}
	}
	// Four different contents are four downloads, however many files hold them.
	if c.n("GET blob") != 4 {
		t.Fatalf("%d downloads for 4 different contents", c.n("GET blob"))
	}
	// A copy edited on the machine is not taken for the contents it once had.
	write(t, rb, "kit/v1/readme.md", "one, edited here and not sent")
	write(t, ra, "kit/v4/readme.md", "one")
	must(t, a.ScanAll(ctx))
	_, err := b.Pull(ctx, 0)
	must(t, err)
	if got := read(t, rb, "kit/v4/readme.md"); got != "one" || c.n("GET blob") != 5 {
		t.Fatalf("after the local copy was edited: %q, %d downloads; want the contents downloaded once more", got, c.n("GET blob"))
	}

	// At rest: full scans and pulls on both machines write nothing, upload nothing and download nothing.
	// (The edit made above is sent and reaches the first machine before the count starts.)
	must(t, b.ScanAll(ctx))
	must(t, a.SyncOnce(ctx))
	quiet := map[string]int{}
	for _, k := range []string{"POST commit", "PUT blob", "GET blob", "HEAD blob", "POST missing"} {
		quiet[k] = c.n(k)
	}
	for range 3 {
		must(t, a.SyncOnce(ctx))
		must(t, b.SyncOnce(ctx))
		must(t, a.ScanAll(ctx))
		must(t, b.ScanAll(ctx))
	}
	for k, was := range quiet {
		if got := c.n(k); got != was {
			t.Errorf("at rest %s went from %d to %d", k, was, got)
		}
	}
}

func TestAStoppedScanLeavesTheFolderToTheNextOne(t *testing.T) {
	c := newCounted(t)
	a, root := c.agent(t, nil)
	for _, name := range []string{"a", "b", "c"} {
		write(t, root, "admin/project/"+name+".dc.html", "<x-dc>"+name+"</x-dc>")
	}
	stopped, cancel := context.WithCancel(ctx)
	cancel()
	_ = a.ScanAll(stopped)
	if c.n("POST commit") != 0 || c.n("HEAD blob") != 0 || c.n("POST missing") != 0 {
		t.Fatalf("a stopped scan asked the store: %v", c.hits)
	}
	must(t, a.ScanAll(ctx))
	if c.n("commit OK") != 3 {
		t.Fatalf("the next scan: %v", c.hits)
	}
}
