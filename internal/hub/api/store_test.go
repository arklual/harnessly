package api_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/filestore"
	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
	"github.com/ly-solutions/harnessly/internal/hubproto"
	"github.com/ly-solutions/harnessly/internal/storelink"
)

// storeMachine is a runtime of a hub's machine with its Sinopia library, syncing through the hub's store.
type storeMachine struct {
	lib  string
	link *storelink.Link
	stop context.CancelFunc
	done chan struct{}
}

func dialURL(s *hubtest.Stack) string { return "ws" + strings.TrimPrefix(s.URL, "http") + "/agent/v1" }

func linkOf(t *testing.T, s *hubtest.Stack, a *hubtest.Agent, data string) *storelink.Link {
	t.Helper()
	l, err := storelink.Open(storelink.Options{DataDir: data, Machine: &storelink.Machine{ID: a.MachineID, DialURL: dialURL(s), Key: a.Priv},
		Name: "vm-" + a.MachineID[:4], Now: s.Clock.Now, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	if l == nil || l.Kind() != storelink.KindHub {
		t.Fatalf("a hub's machine must link to the hub's store: %+v", l)
	}
	return l
}

func startMachine(t *testing.T, s *hubtest.Stack, a *hubtest.Agent, dir string) *storeMachine {
	t.Helper()
	m := &storeMachine{lib: filepath.Join(dir, "library"), link: linkOf(t, s, a, filepath.Join(dir, "data")), done: make(chan struct{})}
	ctx, stop := context.WithCancel(context.Background())
	m.stop = stop
	go func() {
		defer close(m.done)
		m.link.SyncFolder(ctx, filestore.KindDesign, func() string { return m.lib })
	}()
	t.Cleanup(m.halt)
	return m
}

func (m *storeMachine) halt() {
	m.stop()
	<-m.done
}

func writeFile(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// eventually waits for a file to hold what is wanted.
func eventually(t *testing.T, within time.Duration, root, rel, want string) {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		b, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if string(b) == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: %q after %s, want %q", rel, b, within, want)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Two machines of a space keep one Sinopia library through the hub's store: an edit on one is on the other in seconds;
// with the first one off, the second goes on editing, and the first catches up when it is back; a write made on a
// stale copy is kept, not lost; a machine of another space sees none of it.
func TestStoreSyncsTheLibraryOfASpaceAcrossMachines(t *testing.T) {
	s := hubtest.New(t)
	owner := s.SignIn("owner@example.com")
	space, _ := owner.NewSpace("Acme")
	other, _ := owner.NewSpace("Beta")
	a, b, c := owner.Machine(space, nil), owner.Machine(space, nil), owner.Machine(other, nil)
	dir := t.TempDir()

	ma := startMachine(t, s, a, filepath.Join(dir, "a"))
	mb := startMachine(t, s, b, filepath.Join(dir, "b"))
	mc := startMachine(t, s, c, filepath.Join(dir, "c"))

	started := time.Now()
	writeFile(t, ma.lib, "p1/project/Главная.dc.html", "<h1>v1</h1>")
	eventually(t, 15*time.Second, mb.lib, "p1/project/Главная.dc.html", "<h1>v1</h1>")
	t.Logf("an edit on one machine reached the other in %s", time.Since(started).Round(10*time.Millisecond))

	// The first machine goes away; the second edits; the first comes back and catches up.
	ma.halt()
	writeFile(t, mb.lib, "p1/project/Главная.dc.html", "<h1>v2 while a was off</h1>")
	writeFile(t, mb.lib, "p2/artifact.json", `{"title":"new"}`)
	time.Sleep(1500 * time.Millisecond) // the second machine's edit reaches the hub
	ma = startMachine(t, s, a, filepath.Join(dir, "a"))
	eventually(t, 15*time.Second, ma.lib, "p1/project/Главная.dc.html", "<h1>v2 while a was off</h1>")
	eventually(t, 15*time.Second, ma.lib, "p2/artifact.json", `{"title":"new"}`)

	// A conflict: the first machine is off, both edit the same file; the one that comes second keeps its edit aside.
	ma.halt()
	writeFile(t, mb.lib, "p1/notes.txt", "b wrote this")
	writeFile(t, ma.lib, "p1/notes.txt", "a wrote this offline")
	time.Sleep(1500 * time.Millisecond)
	ma = startMachine(t, s, a, filepath.Join(dir, "a"))
	eventually(t, 15*time.Second, ma.lib, "p1/notes.txt", "b wrote this")
	kept := filepath.Join(ma.lib, ".conflicts", "p1")
	deadline := time.Now().Add(5 * time.Second)
	for {
		entries, _ := os.ReadDir(kept)
		var found bool
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), "notes.txt.") && !strings.HasSuffix(e.Name(), ".json") {
				body, _ := os.ReadFile(filepath.Join(kept, e.Name()))
				found = string(body) == "a wrote this offline"
			}
		}
		if found {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the losing edit is not kept in %s: %v", kept, entries)
		}
		time.Sleep(50 * time.Millisecond)
	}

	// The other space's machine has its own library.
	if _, err := os.Stat(filepath.Join(mc.lib, "p1")); !os.IsNotExist(err) {
		t.Fatalf("another space's machine got the library: %v", err)
	}
	writeFile(t, mc.lib, "own/index.html", "beta")
	time.Sleep(1500 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(ma.lib, "own")); !os.IsNotExist(err) {
		t.Fatalf("a machine got another space's project: %v", err)
	}
}

// A blob of the uploads collection is put with its record and read back by its hash, by another machine of the space;
// a machine of another space cannot read it by guessing the hash.
func TestStoreUploadsBlobByHash(t *testing.T) {
	s := hubtest.New(t)
	owner := s.SignIn("owner@example.com")
	space, _ := owner.NewSpace("Acme")
	other, _ := owner.NewSpace("Beta")
	a, b, c := owner.Machine(space, nil), owner.Machine(space, nil), owner.Machine(other, nil)
	dir := t.TempDir()
	ctx := context.Background()

	up := linkOf(t, s, a, filepath.Join(dir, "a")).Client(filestore.KindUploads)
	if err := linkOf(t, s, a, filepath.Join(dir, "a")).Ready(ctx, time.Second); err != nil {
		t.Fatal(err)
	}
	body := []byte("\x89PNG a picture from the chat")
	e, err := up.Put(ctx, "chat-1/shot.png", body, "owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	var got bytes.Buffer
	if err := linkOf(t, s, b, filepath.Join(dir, "b")).Client(filestore.KindUploads).GetBlob(ctx, e.Sha, &got); err != nil || got.String() != string(body) {
		t.Fatalf("read by hash on another machine: %q %v", got.String(), err)
	}
	got.Reset()
	if err := linkOf(t, s, c, filepath.Join(dir, "c")).Client(filestore.KindUploads).GetBlob(ctx, e.Sha, &got); err == nil {
		t.Fatal("a machine of another space read the blob")
	}
}

// The store is a capability of the hub; the API takes only a live session, which only the machine's own key gets.
func TestStoreSessionAndCapability(t *testing.T) {
	s := hubtest.New(t)
	var doc struct {
		Capabilities []struct{ ID, Status string } `json:"capabilities"`
	}
	s.Browser().Do("GET", "/api/capabilities", nil).JSON(&doc)
	on := false
	for _, c := range doc.Capabilities {
		on = on || c.ID == hubproto.StoreCapability && c.Status == "on"
	}
	if !on {
		t.Fatalf("the store is not among the capabilities: %+v", doc)
	}
	if r := s.Browser().Do("GET", "/store/v1/collections/design/changes", nil); r.Code != http.StatusUnauthorized {
		t.Fatalf("no session: %s", r)
	}
	owner := s.SignIn("owner@example.com")
	space, _ := owner.NewSpace("Acme")
	a := owner.Machine(space, nil)
	host, _ := hubproto.DialHost(s.Cfg.PublicURL)
	dial, _ := hubproto.SignDial(a.Priv, a.MachineID, s.Clock.Now(), host)
	if r := s.Browser().Do("POST", hubproto.StorePath, nil, "Authorization", dial); r.Code != http.StatusUnauthorized {
		t.Fatalf("a dial signature opened a store session: %s", r)
	}
	sess, err := storelink.RequestSession(context.Background(), http.DefaultClient,
		storelink.Machine{ID: a.MachineID, DialURL: dialURL(s), Key: a.Priv}, s.Clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	if r := s.Browser().Do("GET", "/store/v1/collections/design/changes", nil, "Authorization", "Bearer "+sess.Token); r.Code != 200 {
		t.Fatalf("with a session: %s", r)
	}
	if r := s.Browser().Do("GET", "/store/v1/collections/design/changes", nil, "Authorization", "Bearer "+sess.Token+"x"); r.Code != http.StatusUnauthorized {
		t.Fatalf("a forged session: %s", r)
	}
	s.Clock.Advance(2 * time.Hour)
	if r := s.Browser().Do("GET", "/store/v1/collections/design/changes", nil, "Authorization", "Bearer "+sess.Token); r.Code != http.StatusUnauthorized {
		t.Fatalf("an expired session: %s", r)
	}
}

// HUB_STORE=off: no capability, no routes; a runtime then keeps its data as before.
func TestStoreOff(t *testing.T) {
	s := hubtest.New(t, hubtest.Env("HUB_STORE", "off"))
	if s.Hub.Store != nil {
		t.Fatal("the store is open with HUB_STORE=off")
	}
	if r := s.Browser().Do("POST", hubproto.StorePath, nil); r.Code != http.StatusNotFound {
		t.Fatalf("the session route: %s", r)
	}
	if err := storelink.HubHasStore(context.Background(), http.DefaultClient, s.URL); err == nil {
		t.Fatal("a hub without a store says it has one")
	}
}
