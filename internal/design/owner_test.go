package design

import (
	"os"
	"path/filepath"
	"testing"
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
