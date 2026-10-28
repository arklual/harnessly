package design

import (
	"path/filepath"
	"strings"

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
//
// Person is the stable key of the person (the owner's whole address, lower case) and beats User: one
// person has a runtime, so a different Unix user, on every machine, and several machines share one Unix user name
// (a self-hosted profile runs everything as "harnessly"). A file without Person (made before the owner file had a person, or by a
// runtime that does not know its person's address) is compared by the Unix user as it always was.
type Owner struct {
	User, Name, Person string
	V                  int
	Private            bool
}

// Mine: the project was made by the person asking; for a file that names no person, by the runtime asking.
func (o Owner) Mine(id Identity) bool {
	if o.Person != "" {
		return o.Person == id.PersonID
	}
	return o.User != "" && o.User == id.Runtime
}

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
	obj := jsvalue.NewObject("v", 2, "user", who.Runtime, "name", who.Person, "private", true)
	if who.PersonID != "" {
		obj.Set("person", who.PersonID)
	}
	_, err = writeIfChanged(file, append(jsvalue.Stringify(obj, "  "), '\n'))
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
	if person, ok := CleanText(get(o, "person"), 128); ok {
		out.Person = strings.ToLower(person)
	}
	if v, ok := get(o, "v").(float64); ok && v >= 2 {
		out.V = 2
		out.Private = get(o, "private") == true
	}
	return out, true
}

// BackfillOwners adds the person to the owner files made before the owner file had a person, on the word of the gateway only: the
// snapshot enforces access and lists the project with this runtime's person (the whole address) as its owner, the
// file names no person yet, and its user is this runtime's Unix user. Reading never writes: on a self-hosted library
// every person shares one Unix user, so a file alone cannot say whose it is. A file of another Unix user is never
// touched; the write is the library's usual one (no links, a temporary file renamed over).
func (l *Library) BackfillOwners(s AccessSnapshot) {
	if !s.Enforce {
		return
	}
	who, err := l.Identity()
	if err != nil || who.PersonID == "" {
		return
	}
	for id, g := range s.Projects {
		if g.Role != "owner" || strings.ToLower(strings.TrimSpace(g.Owner)) != who.PersonID {
			continue
		}
		file := l.ownerPath(id)
		if file == "" {
			continue
		}
		o, ok := readSmallJSON(file)
		if !ok || get(o, "person") != nil {
			continue
		}
		if user, ok := CleanText(get(o, "user"), 64); !ok || user != who.Runtime {
			continue
		}
		o.Set("person", who.PersonID)
		if _, err := writeIfChanged(file, append(jsvalue.Stringify(o, "  "), '\n')); err != nil && l.cfg.Log != nil {
			l.cfg.Log("warn", "design: the person was not added to an owner file", err)
		}
	}
}
