package design

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/ly-solutions/harnessly/internal/locale"
)

// Folders of the library. A folder is a way to lay projects out, not a right: who sees a project is the access
// snapshot's, and a folder changes none of it. The layout is personal: it is kept per person in <library>/.folders/, so
// a project the team shares sits in another folder (or none) for each teammate, and the same person's other runtimes on
// this library read the same layout. It is not the hub's: the hub knows who may open a project, not how each person
// arranges their own shelf, and the layout must work for a runtime that has no hub at all.

const (
	foldersDir = ".folders"
	// FolderMax bounds a person's folders; FolderNameMax a name.
	FolderMax     = 200
	FolderNameMax = 60
	// SystemFile is where a project records the design system or kit it was started on (beside meta/, like the owner file).
	SystemFile = ".system.json"
)

// Folder is a folder of the library. Parent is "" for a top one; a folder inside another cannot hold a folder (one level).
type Folder struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Parent string `json:"parent"`
}

// System is the design system or kit a project was built on.
type System struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type layoutFile struct {
	Folders []Folder          `json:"folders"`
	Placed  map[string]string `json:"placed"`
	// Examples: the demo projects were laid in the demo folder once; what the person does with them after is theirs.
	Examples bool `json:"examples,omitempty"`
	// Named: the folder the first release called "Examples" was renamed to "Demo" once (or never was that).
	Named bool `json:"named,omitempty"`
}

// DemoProjects are the onboarding projects every library starts with (cmd/runtime/demo). Each person finds them in
// one folder, "Demo", made the first time their layout is read with any of them in the library (and again, when the
// person asks for the demo back, for the projects that come back). A person who moves them out, renames the folder or
// deletes it does not get it back on its own: what they do with it after is theirs.
var DemoProjects = []string{"relay-site", "relay-deck", "relay-video", "relay-ds"}

// DemoFolderID is the demo folder's id, the same for everyone (it was the "Examples" folder first: same id, so the
// libraries that already have it keep one folder, not two).
const DemoFolderID = "f-0000e0a1"

// demoFolderName is the demo folder's default name.
func demoFolderName() string { return locale.Pick("Demo", "Демо") }

func isOldDemoFolderName(n string) bool {
	return sameName(n, "Examples") || sameName(n, "Примеры")
}

// demoFolder finds the person's demo folder: by its id (a rename does not lose it), else a top folder of the default
// name; it makes one when there is none and the person has room for it, and says whether it did.
func demoFolder(f *layoutFile) (id string, made, ok bool) {
	name := demoFolderName()
	for _, fo := range f.Folders {
		if fo.ID == DemoFolderID {
			return fo.ID, false, true
		}
	}
	for _, fo := range f.Folders {
		if fo.Parent == "" && sameName(fo.Name, name) {
			return fo.ID, false, true
		}
	}
	if len(f.Folders) >= FolderMax {
		return "", false, false
	}
	f.Folders = append(f.Folders, Folder{ID: DemoFolderID, Name: name})
	return DemoFolderID, true, true
}

// placeDemo lays the given demo projects that are not placed yet in the demo folder; true when it changed f.
func placeDemo(f *layoutFile, ids []string) bool {
	if len(ids) == 0 {
		return false
	}
	folder, made, ok := demoFolder(f)
	if !ok {
		return false
	}
	changed := made
	for _, id := range ids {
		if _, placed := f.Placed[id]; !placed {
			f.Placed[id] = folder
			changed = true
		}
	}
	return changed
}

// withExamples (the layout's first read) lays the demo projects present in the library in the demo folder, once, and
// renames the folder the first release called "Examples" ("Примеры") to "Demo" once, unless the person renamed it
// themselves or has a folder of that name already; true when it changed f.
func (l *Library) withExamples(f *layoutFile) bool {
	changed := false
	if f.Examples && !f.Named {
		f.Named = true
		changed = true
		for i, fo := range f.Folders {
			if fo.ID != DemoFolderID || !isOldDemoFolderName(fo.Name) {
				continue
			}
			taken := false
			for _, o := range f.Folders {
				taken = taken || o.ID != fo.ID && o.Parent == fo.Parent && sameName(o.Name, demoFolderName())
			}
			if !taken {
				f.Folders[i].Name = demoFolderName()
			}
		}
	}
	if f.Examples {
		return changed
	}
	var present []string
	for _, id := range DemoProjects {
		if st, err := os.Stat(l.ArtifactDir(id)); err == nil && st.IsDir() {
			present = append(present, id)
		}
	}
	if len(present) == 0 {
		return changed
	}
	placeDemo(f, present)
	f.Examples, f.Named = true, true
	return true
}

// PlaceDemo lays the demo projects that came back (the person asked for them) in the demo folder: the one they have
// (found by id, so a rename does not lose it) or a new one when they deleted it. A project they already placed stays
// where it is.
func (l *Library) PlaceDemo(person string, ids []string) error {
	key, err := l.layoutKey(person)
	if err != nil {
		return err
	}
	m := l.lockOf("folders\x00" + key)
	m.Lock()
	defer m.Unlock()
	f := l.readLayout(key)
	f.Examples, f.Named = true, true
	if !placeDemo(&f, ids) {
		return nil
	}
	return l.writeLayout(key, f)
}

// Layout is a person's folders and where their projects lie (a project not named lies at the top).
type Layout struct {
	Folders []Folder          `json:"folders"`
	Placed  map[string]string `json:"placed"`
	Systems map[string]System `json:"systems"`
}

// FolderError is a refusal to lay out, with the status to answer.
type FolderError struct {
	Msg    string
	Status int
}

func (e *FolderError) Error() string { return e.Msg }

func folderErr(status int, en, ru string) error {
	return &FolderError{Msg: locale.Pick(en, ru), Status: status}
}

var folderIDRe = regexp.MustCompile(`^f-[0-9a-f]{8}$`)

// layoutKey names the person's file: their address, else the runtime's Unix user.
func (l *Library) layoutKey(person string) (string, error) {
	who, err := l.IdentityOf(person)
	if err != nil {
		return "", err
	}
	// Injective: the hash of the exact address (or the Unix user, told apart by a prefix), so two people never share a file.
	src := "person:" + who.PersonID
	if who.PersonID == "" {
		src = "runtime:" + who.Runtime
	}
	sum := sha256.Sum256([]byte(src))
	key := hex.EncodeToString(sum[:16])
	return key, nil
}

func (l *Library) layoutPath(key string) string {
	return filepath.Join(l.Root(), foldersDir, key+".json")
}

func (l *Library) readLayout(key string) layoutFile {
	out := layoutFile{Folders: []Folder{}, Placed: map[string]string{}}
	raw, err := os.ReadFile(l.layoutPath(key))
	if err != nil || len(raw) > 1<<20 {
		return out
	}
	var f layoutFile
	if json.Unmarshal(raw, &f) != nil {
		return out
	}
	out.Examples, out.Named = f.Examples, f.Named
	known := map[string]Folder{}
	for _, fo := range f.Folders {
		if folderIDRe.MatchString(fo.ID) && fo.Name != "" {
			known[fo.ID] = fo
			out.Folders = append(out.Folders, fo)
		}
	}
	for p, fo := range f.Placed {
		if _, ok := known[fo]; ok && IsLibraryID(p) {
			out.Placed[p] = fo
		}
	}
	return out
}

func (l *Library) writeLayout(key string, f layoutFile) error {
	if err := os.MkdirAll(filepath.Join(l.Root(), foldersDir), 0o777); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	_, err = writeIfChanged(l.layoutPath(key), append(raw, '\n'))
	return err
}

// ReadSystem is the design system a project was built on; false when it records none.
func (l *Library) ReadSystem(id string) (System, bool) {
	if !IsLibraryID(id) {
		return System{}, false
	}
	o, ok := readSmallJSON(filepath.Join(l.ArtifactDir(id), SystemFile))
	if !ok {
		return System{}, false
	}
	sid, ok1 := CleanText(get(o, "id"), 128)
	label, ok2 := CleanText(get(o, "label"), 128)
	if !ok1 || !ok2 {
		return System{}, false
	}
	return System{ID: sid, Label: label}, true
}

// SetSystem records the design system a new project is built on, once.
func (l *Library) SetSystem(id string, s System) error {
	if !IsLibraryID(id) || s.ID == "" {
		return nil
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	_, err = writeIfChanged(filepath.Join(l.ArtifactDir(id), SystemFile), append(raw, '\n'))
	return err
}

// LayoutFor is the person's layout with the design systems of the projects they can see.
func (l *Library) LayoutFor(person string) (Layout, error) {
	key, err := l.layoutKey(person)
	if err != nil {
		return Layout{}, err
	}
	f := l.readLayout(key)
	if !f.Examples || !f.Named {
		m := l.lockOf("folders\x00" + key)
		m.Lock()
		f = l.readLayout(key)
		if l.withExamples(&f) {
			if err := l.writeLayout(key, f); err != nil {
				m.Unlock()
				return Layout{}, err
			}
		}
		m.Unlock()
	}
	out := Layout{Folders: f.Folders, Placed: f.Placed, Systems: map[string]System{}}
	for _, a := range l.VisibleToFor(l.ListArtifacts(), person) {
		if s, ok := l.ReadSystem(a.ID); ok {
			out.Systems[a.ID] = s
		}
	}
	// A project the person cannot see (or that is gone) is not placed for them.
	seen := map[string]bool{}
	for _, a := range l.ListArtifacts() {
		seen[a.ID] = true
	}
	for p := range out.Placed {
		if !seen[p] {
			delete(out.Placed, p)
		}
	}
	return out, nil
}

func newFolderID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return "f-" + hex.EncodeToString(b)
}

func sameName(a, b string) bool { return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b)) }

// SaveFolder makes a folder (id "") or renames one (and moves it under another top folder when parent is given).
func (l *Library) SaveFolder(person, id, name, parent string, hasParent bool) (Folder, error) {
	clean, ok := CleanText(name, FolderNameMax)
	clean = strings.TrimSpace(clean)
	if !ok || clean == "" {
		return Folder{}, folderErr(http.StatusBadRequest, "Name the folder", "Назовите папку")
	}
	key, err := l.layoutKey(person)
	if err != nil {
		return Folder{}, err
	}
	m := l.lockOf("folders\x00" + key)
	m.Lock()
	defer m.Unlock()
	f := l.readLayout(key)
	byID := map[string]int{}
	for i, fo := range f.Folders {
		byID[fo.ID] = i
	}
	idx, existing := byID[id]
	if id != "" && !existing {
		return Folder{}, folderErr(http.StatusNotFound, "The folder is gone", "Папки больше нет")
	}
	if !existing {
		if !hasParent {
			parent = ""
		}
		if len(f.Folders) >= FolderMax {
			return Folder{}, folderErr(http.StatusBadRequest, "Too many folders", "Слишком много папок")
		}
	} else if !hasParent {
		parent = f.Folders[idx].Parent
	}
	if parent != "" {
		pi, ok := byID[parent]
		if !ok {
			return Folder{}, folderErr(http.StatusNotFound, "The folder is gone", "Папки больше нет")
		}
		if f.Folders[pi].Parent != "" || parent == id {
			return Folder{}, folderErr(http.StatusBadRequest, "A folder holds folders one level deep only", "Вложенность — только на один уровень")
		}
		if existing {
			for _, fo := range f.Folders {
				if fo.Parent == id {
					return Folder{}, folderErr(http.StatusBadRequest, "A folder with folders in it stays at the top", "Папка с папками внутри остаётся наверху")
				}
			}
		}
	}
	for _, fo := range f.Folders {
		if fo.ID != id && fo.Parent == parent && sameName(fo.Name, clean) {
			return Folder{}, folderErr(http.StatusConflict, "There is a folder with this name already", "Папка с таким названием уже есть")
		}
	}
	if existing {
		f.Folders[idx].Name, f.Folders[idx].Parent = clean, parent
		return f.Folders[idx], l.writeLayout(key, f)
	}
	fo := Folder{ID: newFolderID(), Name: clean, Parent: parent}
	f.Folders = append(f.Folders, fo)
	return fo, l.writeLayout(key, f)
}

// FolderProjects are the projects lying in the folder and the folders under it.
func (l *Library) FolderProjects(person, id string) ([]string, error) {
	key, err := l.layoutKey(person)
	if err != nil {
		return nil, err
	}
	f := l.readLayout(key)
	in := map[string]bool{id: true}
	for _, fo := range f.Folders {
		if fo.Parent == id {
			in[fo.ID] = true
		}
	}
	var out []string
	for p, fo := range f.Placed {
		if in[fo] {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out, nil
}

// RemoveFolder takes the folder and its subfolders away; projects that lay in them go to the top. Nothing of a project
// is touched.
func (l *Library) RemoveFolder(person, id string) error {
	key, err := l.layoutKey(person)
	if err != nil {
		return err
	}
	m := l.lockOf("folders\x00" + key)
	m.Lock()
	defer m.Unlock()
	f := l.readLayout(key)
	gone := map[string]bool{id: true}
	found := false
	for _, fo := range f.Folders {
		if fo.ID == id {
			found = true
		}
		if fo.Parent == id {
			gone[fo.ID] = true
		}
	}
	if !found {
		return folderErr(http.StatusNotFound, "The folder is gone", "Папки больше нет")
	}
	kept := f.Folders[:0:0]
	for _, fo := range f.Folders {
		if !gone[fo.ID] {
			kept = append(kept, fo)
		}
	}
	f.Folders = kept
	for p, fo := range f.Placed {
		if gone[fo] {
			delete(f.Placed, p)
		}
	}
	return l.writeLayout(key, f)
}

// MoveProjects lays projects in a folder ("" is the top). Only projects the person can see are laid.
func (l *Library) MoveProjects(person string, ids []string, folder string) error {
	key, err := l.layoutKey(person)
	if err != nil {
		return err
	}
	m := l.lockOf("folders\x00" + key)
	m.Lock()
	defer m.Unlock()
	f := l.readLayout(key)
	if folder != "" {
		ok := false
		for _, fo := range f.Folders {
			ok = ok || fo.ID == folder
		}
		if !ok {
			return folderErr(http.StatusNotFound, "The folder is gone", "Папки больше нет")
		}
	}
	for _, p := range ids {
		if !IsLibraryID(p) || !l.CanFor(p, RoleView, person) {
			continue
		}
		if folder == "" {
			delete(f.Placed, p)
		} else {
			f.Placed[p] = folder
		}
	}
	return l.writeLayout(key, f)
}
