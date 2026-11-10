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

	"github.com/ly-solutions/harnessly/internal/filestore"
)

// a design library in the store shows each machine only the projects its person has a role in. The owner's
// machine goes away; a teammate with a role edits the project from their own machine; the edit reaches the owner when
// the owner's machine is back; a machine whose person has no role never gets the files and cannot write them.

const tokenOwner, tokenMate, tokenStranger = "owner-token-owner-token-owner-tok", "mate-token-mate-token-mate-token-m", "stranger-token-stranger-token-str"

// scopeRig is a store whose authorizer behaves like the hub's design filter: per machine, which projects it sees and
// which it edits; a project not listed is open.
type scopeRig struct {
	t   *testing.T
	url string
	mu  sync.Mutex
	// grants[machine][project] = "edit" | "view" | "none"
	grants map[string]map[string]string
}

var scopeMachines = map[string]string{tokenOwner: "vm-owner", tokenMate: "vm-mate", tokenStranger: "vm-stranger"}

func (r *scopeRig) set(machine, project, role string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.grants[machine] == nil {
		r.grants[machine] = map[string]string{}
	}
	r.grants[machine][project] = role
}

func (r *scopeRig) authorize(req *http.Request, coll string) (filestore.Access, error) {
	m := scopeMachines[strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer ")]
	if m == "" {
		return filestore.Access{}, filestore.ErrUnauthorized
	}
	r.mu.Lock()
	g := map[string]string{}
	scope := ""
	for p, role := range r.grants[m] {
		g[p] = role
	}
	for _, p := range []string{"secret", "open"} {
		scope += p + "=" + g[p] + ";"
	}
	r.mu.Unlock()
	role := func(p string) (string, bool) {
		id, _, ok := strings.Cut(p, "/")
		if !ok {
			return "", false
		}
		v, known := g[id]
		return v, known
	}
	return filestore.Access{Member: m, Room: coll, Scope: scope,
		Sees: func(p string) bool {
			v, known := role(p)
			return !known || v != "none"
		},
		Writes: func(p string) bool {
			v, known := role(p)
			return !known || v == "edit"
		},
	}, nil
}

func newScopeRig(t *testing.T) *scopeRig {
	t.Helper()
	r := &scopeRig{t: t, grants: map[string]map[string]string{}}
	srv, err := filestore.OpenFolder(filepath.Join(t.TempDir(), "hub"), r.authorize)
	if err != nil {
		t.Fatal(err)
	}
	h := httptest.NewServer(srv.Handler())
	t.Cleanup(h.Close)
	t.Cleanup(srv.Close)
	r.url = h.URL
	return r
}

func (r *scopeRig) machine(dir, token, name string) (*Agent, string) {
	root := filepath.Join(dir, name)
	rg := &rig{t: r.t, url: r.url}
	return rg.agent(root, filepath.Join(dir, name+"-state"), token, name), root
}

func syncNow(t *testing.T, a *Agent) {
	t.Helper()
	ctx := context.Background()
	for range 4 {
		more, err := a.Pull(ctx, 0)
		must(t, err)
		if !more {
			break
		}
	}
	must(t, a.ScanAll(ctx))
}

func TestScopeOwnerAwayMateEditsStrangerNeverSees(t *testing.T) {
	r := newScopeRig(t)
	dir := t.TempDir()
	owner, ownerRoot := r.machine(dir, tokenOwner, "vm-owner")
	mate, mateRoot := r.machine(dir, tokenMate, "vm-mate")
	stranger, strangerRoot := r.machine(dir, tokenStranger, "vm-stranger")

	// The owner's person owns «secret»; the mate edits it; the stranger has no role. «open» is nobody's: open to all.
	r.set("vm-owner", "secret", "edit")
	r.set("vm-mate", "secret", "edit")
	r.set("vm-stranger", "secret", "none")

	write(t, ownerRoot, "secret/project/Main.dc.html", "v1")
	write(t, ownerRoot, "open/project/Board.dc.html", "open v1")
	syncNow(t, owner)
	syncNow(t, mate)
	syncNow(t, stranger)

	if got := read(t, mateRoot, "secret/project/Main.dc.html"); got != "v1" {
		t.Fatalf("the mate's machine has %q, want the owner's v1", got)
	}
	if got := read(t, strangerRoot, "secret/project/Main.dc.html"); got != "<missing>" {
		t.Fatalf("the stranger's machine got a project its person has no role in: %q", got)
	}
	if got := read(t, strangerRoot, "open/project/Board.dc.html"); got != "open v1" {
		t.Fatalf("an open project did not reach the stranger: %q", got)
	}

	// The owner's machine is off (its agent does not run); the mate edits from their own machine.
	write(t, mateRoot, "secret/project/Main.dc.html", "v2 by the mate")
	syncNow(t, mate)

	// The stranger writes a file of their own into the project: the store refuses it, and the agent does not try again.
	write(t, strangerRoot, "secret/project/Sneak.dc.html", "x")
	syncNow(t, stranger)
	syncNow(t, stranger)

	// The owner's machine is back.
	syncNow(t, owner)
	if got := read(t, ownerRoot, "secret/project/Main.dc.html"); got != "v2 by the mate" {
		t.Fatalf("the owner's machine has %q after it came back, want the mate's edit", got)
	}
	if got := read(t, ownerRoot, "secret/project/Sneak.dc.html"); got != "<missing>" {
		t.Fatalf("a write of a machine without a role reached the owner: %q", got)
	}
}

func TestScopeRoleTakenRemovesTheCopyAndGivenBringsItWhole(t *testing.T) {
	r := newScopeRig(t)
	dir := t.TempDir()
	owner, ownerRoot := r.machine(dir, tokenOwner, "vm-owner")
	mate, mateRoot := r.machine(dir, tokenMate, "vm-mate")
	r.set("vm-owner", "secret", "edit")
	r.set("vm-mate", "secret", "none")

	write(t, ownerRoot, "secret/project/Main.dc.html", "v1")
	write(t, ownerRoot, "secret/project/Second.dc.html", "s1")
	syncNow(t, owner)
	syncNow(t, mate)
	if got := read(t, mateRoot, "secret/project/Main.dc.html"); got != "<missing>" {
		t.Fatalf("no role yet, but the file is there: %q", got)
	}

	// A role is given: the scope changes and the project arrives whole, though its revisions are behind the cursor.
	r.set("vm-mate", "secret", "view")
	syncNow(t, mate)
	syncNow(t, mate)
	if got := read(t, mateRoot, "secret/project/Second.dc.html"); got != "s1" {
		t.Fatalf("after the role was given the project did not arrive whole: %q", got)
	}

	// A viewer's edit is not taken by the store.
	write(t, mateRoot, "secret/project/Main.dc.html", "viewer edit")
	syncNow(t, mate)
	syncNow(t, owner)
	if got := read(t, ownerRoot, "secret/project/Main.dc.html"); got != "v1" {
		t.Fatalf("a viewer's edit reached the owner: %q", got)
	}

	// The role is taken: an unedited copy goes away, the edited one stays (nothing local is lost).
	r.set("vm-mate", "secret", "none")
	syncNow(t, mate)
	if got := read(t, mateRoot, "secret/project/Second.dc.html"); got != "<missing>" {
		t.Fatalf("the role was taken, the unedited copy stayed: %q", got)
	}
	if got := read(t, mateRoot, "secret/project/Main.dc.html"); got != "viewer edit" {
		t.Fatalf("a local edit was lost when the role was taken: %q", got)
	}
}

// A team machine's library is shared by the runtimes of several people: each syncs it with its own person's projects
// and removes nothing, so the folder holds what any of them has a role in.
func TestSharedFolderHoldsWhatAnyOfItsPeopleSees(t *testing.T) {
	r := newScopeRig(t)
	dir := t.TempDir()
	owner, ownerRoot := r.machine(dir, tokenOwner, "vm-owner")
	r.set("vm-owner", "secret", "edit")
	r.set("vm-owner", "theirs", "none")
	r.set("vm-mate", "secret", "none")
	r.set("vm-mate", "theirs", "edit")
	r.set("vm-stranger", "secret", "none")
	r.set("vm-stranger", "theirs", "none")
	write(t, ownerRoot, "secret/project/Main.dc.html", "s")
	syncNow(t, owner)
	mateHome, mateRoot := r.machine(dir, tokenMate, "vm-mate-home")
	write(t, mateRoot, "theirs/project/Main.dc.html", "t")
	syncNow(t, mateHome)

	shared := filepath.Join(dir, "srv")
	must(t, os.MkdirAll(shared, 0o755))
	team := func(token, name string) *Agent {
		a, err := New(Config{Root: shared, StateDir: filepath.Join(dir, name+"-state"),
			Store: NewClient(r.url, filestore.KindDesign, staticToken(token), nil), Machine: name, KeepUnseen: true})
		must(t, err)
		t.Cleanup(a.Close)
		return a
	}
	a, b := team(tokenOwner, "team-a"), team(tokenMate, "team-b")
	for range 2 {
		syncNow(t, a)
		syncNow(t, b)
	}
	if got := read(t, shared, "secret/project/Main.dc.html"); got != "s" {
		t.Fatalf("the owner's project on the team machine: %q", got)
	}
	if got := read(t, shared, "theirs/project/Main.dc.html"); got != "t" {
		t.Fatalf("the mate's project on the team machine: %q", got)
	}
	// Each runtime meets the other's project in the folder and leaves it be: nothing is pushed in its name.
	stranger, strangerRoot := r.machine(dir, tokenStranger, "vm-stranger")
	syncNow(t, stranger)
	for _, p := range []string{"secret/project/Main.dc.html", "theirs/project/Main.dc.html"} {
		if got := read(t, strangerRoot, p); got != "<missing>" {
			t.Fatalf("%s reached a machine without a role: %q", p, got)
		}
	}
}
