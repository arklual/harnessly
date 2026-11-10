package apps

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/appstore"
	"github.com/ly-solutions/harnessly/internal/filestore"
	"github.com/ly-solutions/harnessly/internal/filesync"
	"github.com/ly-solutions/harnessly/internal/storelink"
)

// centerFor is a store like the hub's, as the machines of one space speak to it: each machine's client writes under
// its own name, and sees only its own person's user areas when the room is filtered.
func centerFor(t *testing.T) func(machine, person string) *filesync.Client {
	t.Helper()
	srv, err := filestore.OpenFolder(t.TempDir(), func(r *http.Request, coll string) (filestore.Access, error) {
		return filestore.Access{Member: r.Header.Get("X-Test-Machine"), Room: coll}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })
	return func(machine, person string) *filesync.Client {
		hc := &http.Client{Transport: asMachine{machine, storelink.Handler(srv.Handler())}}
		return filesync.NewClient("http://store.invalid", filestore.KindApps, nil, hc)
	}
}

type asMachine struct {
	name string
	rt   http.RoundTripper
}

func (n asMachine) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("X-Test-Machine", n.name)
	return n.rt.RoundTrip(r)
}

func onCenter(t *testing.T, c *filesync.Client, person string) *Storage {
	t.Helper()
	s := &Storage{Root: t.TempDir()}
	s.UseCenter(&Center{Files: appstore.ClientFiles(c, "test"), Person: person})
	return s
}

// HRN-898: two machines of a space read and write one space's area in the hub's store, ifVersion holds across them,
// and the install's area stays on each machine.
func TestStorageAreasAreTheSpacesUnderAHub(t *testing.T) {
	center := centerFor(t)
	a := onCenter(t, center("a", "p-1"), "p-1")
	b := onCenter(t, center("b", "p-1"), "p-1")
	const app = "acme.board"

	v, err := a.SetIf(app, ScopeSpace, "", "cards", json.RawMessage(`[1]`), nil)
	if err != nil || v != 1 {
		t.Fatalf("a writes: v=%d err=%v", v, err)
	}
	got, ver, ok, err := b.GetVersioned(app, ScopeSpace, "", "cards")
	if err != nil || !ok || string(got) != `[1]` || ver != 1 {
		t.Fatalf("b reads %s v%d ok=%v err=%v", got, ver, ok, err)
	}
	if _, err := b.SetIf(app, ScopeSpace, "", "cards", json.RawMessage(`[1,2]`), &ver); err != nil {
		t.Fatal(err)
	}
	stale := int64(1)
	_, err = a.SetIf(app, ScopeSpace, "", "cards", json.RawMessage(`[1,3]`), &stale)
	var conflict *ConflictError
	if !errors.As(err, &conflict) || conflict.Current != 2 {
		t.Fatalf("a's stale write: %v", err)
	}
	if err := a.Set(app, ScopeDoc, "doc-1", "note", json.RawMessage(`"hi"`)); err != nil {
		t.Fatal(err)
	}
	if keys, err := b.Keys(app, ScopeDoc, "doc-1"); err != nil || len(keys) != 1 {
		t.Fatalf("b's keys of the doc: %v %v", keys, err)
	}
	if err := a.Set(app, ScopeInstall, "", "token", json.RawMessage(`"a"`)); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := b.Get(app, ScopeInstall, "", "token"); ok {
		t.Fatal("the install's area of a reached b")
	}
	if _, err := os.Stat(filepath.Join(a.Root, app, "install", "area.json")); err != nil {
		t.Fatalf("the install's area is not on a's disk: %v", err)
	}
	if _, err := os.Stat(filepath.Join(a.Root, app, "space")); !os.IsNotExist(err) {
		t.Fatalf("the space's area is on a's disk: %v", err)
	}
	if err := b.Delete(app, ScopeSpace, "", "cards"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := a.Get(app, ScopeSpace, "", "cards"); ok {
		t.Fatal("a still reads the key b deleted")
	}
}

// Each person's user area is their own; with nobody named it stays on the machine.
func TestStorageUserAreaIsThePersons(t *testing.T) {
	center := centerFor(t)
	a := onCenter(t, center("a", "p-1"), "p-1")
	b := onCenter(t, center("b", "p-2"), "p-2")
	none := onCenter(t, center("c", ""), "")
	const app = "acme.board"
	if err := a.Set(app, ScopeUser, "", "theme", json.RawMessage(`"dark"`)); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := b.Get(app, ScopeUser, "", "theme"); ok {
		t.Fatal("another person reads the user area")
	}
	if err := none.Set(app, ScopeUser, "", "theme", json.RawMessage(`"light"`)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(none.Root, app, "user", "area.json")); err != nil {
		t.Fatalf("a user area with nobody named is not on the machine: %v", err)
	}
	if v, _, _ := a.Get(app, ScopeUser, "", "theme"); string(v) != `"dark"` {
		t.Fatalf("a's own user area: %s", v)
	}
}

// The limits are the same as on a machine: one area, and the areas of a scope per document together, in the space.
func TestStorageQuotaUnderAHub(t *testing.T) {
	center := centerFor(t)
	a := onCenter(t, center("a", "p-1"), "p-1")
	b := onCenter(t, center("b", "p-1"), "p-1")
	const app = "acme.board"
	big := json.RawMessage(`"` + strings.Repeat("x", scopeLimit[ScopeDoc]) + `"`)
	if err := a.Set(app, ScopeDoc, "d1", "k", big); !errors.Is(err, ErrQuota) {
		t.Fatalf("an area past its limit: %v", err)
	}
	chunk := json.RawMessage(`"` + strings.Repeat("x", scopeLimit[ScopeDoc]-100) + `"`)
	n := int(scopeTotal[ScopeDoc] / int64(scopeLimit[ScopeDoc]))
	var refused bool
	for i := 0; i <= n+1; i++ {
		s := a
		if i%2 == 1 {
			s = b
		}
		if err := s.Set(app, ScopeDoc, "d"+strconv.Itoa(i), "k", chunk); errors.Is(err, ErrQuota) {
			refused = true
			break
		} else if err != nil {
			t.Fatal(err)
		}
	}
	if !refused {
		t.Fatal("the documents of the space went past the scope's total")
	}
}

// The move puts the keys of the local areas into the store: two machines' space areas come out as one, a key both have
// keeps the store's value, the local files stay, and a second run moves nothing.
func TestStorageMovesLocalAreasToTheCenter(t *testing.T) {
	center := centerFor(t)
	const app = "acme.board"
	local := func(name string) *Storage {
		s := &Storage{Root: t.TempDir()}
		if err := s.Set(app, ScopeSpace, "", "from-"+name, json.RawMessage(`1`)); err != nil {
			t.Fatal(err)
		}
		if err := s.Set(app, ScopeSpace, "", "both", json.RawMessage(`"`+name+`"`)); err != nil {
			t.Fatal(err)
		}
		if err := s.Set(app, ScopeUser, "", "theme", json.RawMessage(`"`+name+`"`)); err != nil {
			t.Fatal(err)
		}
		if err := s.Set(app, ScopeInstall, "", "secret", json.RawMessage(`1`)); err != nil {
			t.Fatal(err)
		}
		return s
	}
	a, b := local("a"), local("b")
	ctx := context.Background()
	fa := appstore.ClientFiles(center("a", "p-1"), "a")
	fb := appstore.ClientFiles(center("b", "p-2"), "b")
	if moved, failed, err := a.MoveToCenter(ctx, fa, "p-1"); err != nil || failed != 0 || moved != 2 {
		t.Fatalf("a's move: moved=%d failed=%d err=%v", moved, failed, err)
	}
	if moved, failed, err := b.MoveToCenter(ctx, fb, "p-2"); err != nil || failed != 0 || moved != 2 {
		t.Fatalf("b's move: moved=%d failed=%d err=%v", moved, failed, err)
	}
	if moved, _, err := a.MoveToCenter(ctx, fa, "p-1"); err != nil || moved != 0 {
		t.Fatalf("a's second move: moved=%d err=%v", moved, err)
	}
	a.UseCenter(&Center{Files: fa, Person: "p-1"})
	b.UseCenter(&Center{Files: fb, Person: "p-2"})
	keys, err := b.Keys(app, ScopeSpace, "")
	if err != nil || len(keys) != 3 {
		t.Fatalf("the space's keys after both moves: %v %v", keys, err)
	}
	if v, _, _ := b.Get(app, ScopeSpace, "", "both"); string(v) != `"a"` {
		t.Fatalf("a key both had: %s, want the store's (a's, moved first)", v)
	}
	if v, _, _ := b.Get(app, ScopeUser, "", "theme"); string(v) != `"b"` {
		t.Fatalf("b's own user area: %s", v)
	}
	if v, _, _ := a.Get(app, ScopeInstall, "", "secret"); string(v) != `1` {
		t.Fatalf("the install's area after the move: %s", v)
	}
	if _, err := os.Stat(filepath.Join(a.Root, app, "space", "area.json")); err != nil {
		t.Fatalf("the local area is gone after the move: %v", err)
	}
	var journal map[string]Moved
	raw, _ := os.ReadFile(filepath.Join(a.Root, MovedFile))
	if err := json.Unmarshal(raw, &journal); err != nil || journal[app+"/space/area.json"].Status != "moved" {
		t.Fatalf("a's journal: %s", raw)
	}
}

// Another machine's write reaches this machine's views as a change of the key, and only once.
func TestStorageHearsTheOtherMachinesWrites(t *testing.T) {
	center := centerFor(t)
	const app = "acme.board"
	a := onCenter(t, center("a", "p-1"), "p-1")
	b := &Storage{Root: t.TempDir()}
	if err := a.Set(app, ScopeSpace, "", "old", json.RawMessage(`1`)); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var heard []Change
	b.OnChange(func(c Change) {
		mu.Lock()
		heard = append(heard, c)
		mu.Unlock()
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		b.RunCenter(ctx, HubLink{Client: center("b", "p-1"), Person: "p-1"})
	}()
	deadline := time.Now().Add(5 * time.Second)
	for b.centerOf() == nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond)
	if err := a.Set(app, ScopeChat, "chat-1", "draft", json.RawMessage(`"x"`)); err != nil {
		t.Fatal(err)
	}
	want := Change{App: app, Scope: ScopeChat, Owner: "chat-1", Key: "draft"}
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(heard)
		mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-done
	mu.Lock()
	defer mu.Unlock()
	if len(heard) != 1 || heard[0] != want {
		t.Fatalf("b heard %+v, want only %+v", heard, want)
	}
}
