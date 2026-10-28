package design

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func ownerLibrary(t *testing.T, root, user, email string) *Library {
	t.Helper()
	l := New(Config{LibraryDir: root, DataDir: t.TempDir(), OwnerEmail: email,
		Texts: Texts{People: map[string]string{"p.smirnov": "Пётр", "o.orlov": "Олег"}}})
	l.osUser = func() string { return user }
	// A route may start the history; it must stop before the temporary folders go.
	t.Cleanup(l.StopBackground)
	return l
}

// A project made by a runtime remembers its person; another runtime of the library reads it and is not the owner.
func TestOwnerIsRecordedAndReadByOtherRuntimes(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "site", ProjectDirname), 0o777); err != nil {
		t.Fatal(err)
	}
	mine := ownerLibrary(t, root, "se", "p.smirnov@example.com")
	other := ownerLibrary(t, root, "ki", "o.orlov@example.com")

	if _, ok := mine.ReadOwner("site"); ok {
		t.Fatal("an owner before anyone made it")
	}
	if err := mine.SetOwner("site"); err != nil {
		t.Fatal(err)
	}
	// A second record never replaces the first.
	if err := other.SetOwner("site"); err != nil {
		t.Fatal(err)
	}
	owner, ok := other.ReadOwner("site")
	if !ok || owner.Name != "Пётр" || owner.User != "se" {
		t.Fatalf("owner = %+v, %v", owner, ok)
	}
	idMine, _ := mine.Identity()
	idOther, _ := other.Identity()
	if !owner.Mine(idMine) || owner.Mine(idOther) {
		t.Fatal("whose it is")
	}
	if (Owner{}).Mine(Identity{}) {
		t.Fatal("nobody owns what has no owner")
	}
}

// What the shared folder holds is untrusted: a link, a blank or a wrong-typed field is no owner.
func TestOwnerFileIsReadWithDistrust(t *testing.T) {
	root := t.TempDir()
	l := ownerLibrary(t, root, "se", "p.smirnov@example.com")
	write := func(id, body string) {
		writeFile(t, filepath.Join(root, id, OwnerFile), body)
	}
	write("blank", `{"v":1,"user":"se","name":"  "}`)
	write("typed", `{"v":1,"user":7,"name":"Пётр"}`)
	write("broken", `{"user":`)
	writeFile(t, filepath.Join(root, "outside.json"), `{"user":"x","name":"Чужой"}`)
	if err := os.MkdirAll(filepath.Join(root, "linked"), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "outside.json"), filepath.Join(root, "linked", OwnerFile)); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"blank", "typed", "broken", "linked", "missing", "../escape"} {
		if o, ok := l.ReadOwner(id); ok {
			t.Fatalf("%s: owner %+v", id, o)
		}
	}
	if err := l.SetOwner("../escape"); err != nil {
		t.Fatal(err)
	}
}

// The owner is a person, not a Unix user: the same person under another Unix user on another machine
// owns the project; another person under the same Unix user (self-hosted machines all run as one) does not; a file
// without a person keeps comparing the Unix user.
func TestOwnerIsMatchedByPersonOverUnixUser(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "site", ProjectDirname), 0o777); err != nil {
		t.Fatal(err)
	}
	maker := ownerLibrary(t, root, "se", "p.smirnov@example.com")
	if err := maker.SetOwner("site"); err != nil {
		t.Fatal(err)
	}
	owner, ok := maker.ReadOwner("site")
	if !ok || owner.Person != "p.smirnov@example.com" {
		t.Fatalf("owner = %+v, %v", owner, ok)
	}
	samePersonOtherUser, _ := ownerLibrary(t, root, "coder", "P.Smirnov@example.com").Identity()
	otherPersonSameUser, _ := ownerLibrary(t, root, "se", "o.orlov@example.com").Identity()
	sameLocalOtherDomain, _ := ownerLibrary(t, root, "se", "p.smirnov@other.org").Identity()
	noAddressSameUser, _ := ownerLibrary(t, root, "se", "").Identity()
	if !owner.Mine(samePersonOtherUser) {
		t.Fatal("the same person under another Unix user is the owner")
	}
	if owner.Mine(otherPersonSameUser) || owner.Mine(sameLocalOtherDomain) || owner.Mine(noAddressSameUser) {
		t.Fatal("another person under the same Unix user is not the owner")
	}

	legacy := Owner{User: "se", Name: "Пётр", V: 2, Private: true}
	if !legacy.Mine(Identity{Runtime: "se"}) || legacy.Mine(samePersonOtherUser) {
		t.Fatal("a file without a person compares the Unix user")
	}

	// A runtime that knows no address records no person, like before.
	if err := os.MkdirAll(filepath.Join(root, "bare", ProjectDirname), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := ownerLibrary(t, root, "se", "").SetOwner("bare"); err != nil {
		t.Fatal(err)
	}
	if o, ok := ownerLibrary(t, root, "se", "").ReadOwner("bare"); !ok || o.Person != "" {
		t.Fatalf("bare owner = %+v, %v", o, ok)
	}
}

// An owner file made before the owner file had a person gets its person only on the gateway's word: two people share one
// Unix user, and reading a file never writes it.
func TestOwnerFileIsBackfilledOnlyOnGatewayWord(t *testing.T) {
	const body = `{"v":2,"user":"harnessly","name":"harnessly","private":true}`
	setup := func() (string, *Library, *Library) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, "p1", OwnerFile), body)
		return root, ownerLibrary(t, root, "harnessly", "a@example.com"), ownerLibrary(t, root, "harnessly", "b@example.com")
	}
	read := func(root string) string {
		b, _ := os.ReadFile(filepath.Join(root, "p1", OwnerFile))
		return string(b)
	}
	snap := func(enforce bool, owner string) AccessSnapshot {
		s := AccessSnapshot{Enforce: enforce, At: time.Now(),
			Projects: map[string]Grant{"p1": {Role: "owner", Owner: owner, OwnerName: "x"}}}
		if owner == "" {
			s.Projects = nil
		}
		return s
	}

	// Reads and lists never write; neither does a runtime that has no snapshot.
	root, a, b := setup()
	for _, l := range []*Library{a, b} {
		l.ReadOwner("p1")
		l.RoleNow("p1")
	}
	if read(root) != body {
		t.Fatalf("a read wrote: %s", read(root))
	}

	// The gateway says A owns it: only A's runtime writes, with A's whole address.
	if err := b.SetAccess(snap(true, "A@Example.com")); err != nil {
		t.Fatal(err)
	}
	if read(root) != body {
		t.Fatalf("B wrote over A's project: %s", read(root))
	}
	if err := a.SetAccess(snap(true, "A@Example.com")); err != nil {
		t.Fatal(err)
	}
	if o, ok := b.ReadOwner("p1"); !ok || o.Person != "a@example.com" || o.User != "harnessly" || !o.Private {
		t.Fatalf("after the gateway's word: %+v, %v", o, ok)
	}
	// B no longer reads it as its own, A does.
	idA, _ := a.Identity()
	idB, _ := b.Identity()
	o, _ := a.ReadOwner("p1")
	if !o.Mine(idA) || o.Mine(idB) {
		t.Fatal("whose it is")
	}

	// No row, no enforcement, another owner, another Unix user: nothing is written.
	for name, tc := range map[string]struct {
		s    AccessSnapshot
		user string
	}{
		"not enforced": {snap(false, "a@example.com"), "harnessly"},
		"no row":       {snap(true, ""), "harnessly"},
		"owner is B":   {snap(true, "b@example.com"), "harnessly"},
		"other user":   {snap(true, "a@example.com"), "se"},
	} {
		root, _, _ := setup()
		l := ownerLibrary(t, root, tc.user, "a@example.com")
		if err := l.SetAccess(tc.s); err != nil {
			t.Fatal(err)
		}
		if read(root) != body {
			t.Fatalf("%s: wrote %s", name, read(root))
		}
	}
}
