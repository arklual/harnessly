package design

import (
	"path/filepath"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// Who made a project. The library is a folder several teammates' runtimes write into, and nothing in a project says
// who started it, so a new project gets <project>/.owner.json: the Unix user of the runtime that made it (what tells
// runtimes apart, as in a claim) and the name of the person behind that runtime. The file sits beside meta/, not in
// project/ or meta/, so the board format and its history never carry it. A project without the file, made before
// this, has no known owner and none is guessed. The file is written by whoever has access to the folder, so it is
// read like everything else in the library: no links, small, a clean one-line text.

const OwnerFile = ".owner.json"

// Owner is the runtime and the person that made a project.
// V is the version of the file (1 before projects were personal, 2 with "private"); Private: the project is personal
// until the gateway says otherwise.
type Owner struct {
	User, Name string
	V          int
	Private    bool
}

// Mine: the project was made by the runtime asking.
func (o Owner) Mine(id Identity) bool { return o.User != "" && o.User == id.Runtime }

// SetOwner records the runtime's person as the owner of a project that has none yet; an existing record stays. A new
// project is personal: the file says so (v 2, "private").
func (l *Library) SetOwner(id string) error {
	file := l.ownerPath(id)
	if file == "" {
		return nil
	}
	if _, ok := l.ReadOwner(id); ok {
		return nil
	}
	who, err := l.Identity()
	if err != nil {
		return err
	}
	_, err = writeIfChanged(file, append(jsvalue.Stringify(jsvalue.NewObject("v", 2, "user", who.Runtime, "name", who.Person, "private", true), "  "), '\n'))
	return err
}

func (l *Library) ownerPath(id string) string {
	if !IsLibraryID(id) {
		return ""
	}
	return filepath.Join(l.ArtifactDir(id), OwnerFile)
}

// ReadOwner is who made a project; false when the file is missing or says nothing usable.
func (l *Library) ReadOwner(id string) (Owner, bool) {
	file := l.ownerPath(id)
	if file == "" {
		return Owner{}, false
	}
	o, ok := readSmallJSON(file)
	if !ok {
		return Owner{}, false
	}
	user, okUser := CleanText(get(o, "user"), 64)
	name, okName := CleanText(get(o, "name"), 64)
	if !okUser || !okName {
		return Owner{}, false
	}
	out := Owner{User: user, Name: name, V: 1}
	if v, ok := get(o, "v").(float64); ok && v >= 2 {
		out.V = 2
		out.Private = get(o, "private") == true
	}
	return out, true
}
