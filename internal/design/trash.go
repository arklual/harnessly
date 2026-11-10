package design

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/ly-solutions/harnessly/internal/locale"
)

// The trash of the library: what DeleteProject moved to .trash/ is listed, put back or erased here. Only the
// project's owner sees and does any of it, by the owner file that went to the trash with the project (the access rows
// went at the delete, so the file is all that is left to say whose it was).

// TrashItem is a project in the trash.
type TrashItem struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	DeletedAt int64  `json:"deletedAt"`
	// Kind is site, slides or video, told by the folder's own files.
	Kind string `json:"kind"`
}

type trashEntry struct {
	id, dir string
	at      int64
}

func (l *Library) trashEntries(id string) []trashEntry {
	root := filepath.Join(l.Root(), ".trash")
	entries, _ := os.ReadDir(root)
	var out []trashEntry
	for _, e := range entries {
		m := trashedProject.FindStringSubmatch(e.Name())
		if !e.IsDir() || m == nil || !IsLibraryID(m[1]) || (id != "" && m[1] != id) {
			continue
		}
		stamp := e.Name()[len(m[1])+1:]
		at, _ := strconv.ParseInt(stamp, 10, 64)
		out = append(out, trashEntry{id: m[1], dir: filepath.Join(root, e.Name()), at: at})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].at > out[j].at })
	return out
}

// ownedTrash: the trashed folder's owner file names the person: by the same rule as gateDelete (the person when the file has one, else the runtime's Unix user), so restore and purge need no more than the delete did.
func (l *Library) ownedTrash(e trashEntry, person string) bool {
	who, err := l.IdentityOf(person)
	if err != nil {
		return false
	}
	o, ok := readSmallJSON(filepath.Join(e.dir, OwnerFile))
	if !ok {
		return false
	}
	name, _ := CleanText(get(o, "user"), 64)
	pid, _ := CleanText(get(o, "person"), 128)
	return Owner{User: name, Person: lowerTrim(pid)}.Mine(who)
}

func lowerTrim(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

func trashTitle(e trashEntry) string {
	if o, ok := readSmallJSON(filepath.Join(e.dir, MetaDirname, MetaProjectFile)); ok {
		if t, ok := CleanText(get(o, "title"), 200); ok && t != "" {
			return t
		}
	}
	if o, ok := readSmallJSON(filepath.Join(e.dir, ArtifactFile)); ok {
		if t, ok := CleanText(get(o, "title"), 200); ok && t != "" {
			return t
		}
	}
	return e.id
}

func trashKind(e trashEntry) string {
	switch {
	case isRealFile(filepath.Join(e.dir, MetaDirname, VideoSettingsFile)) || isRealFile(filepath.Join(e.dir, VideoSettingsFile)):
		return "video"
	case isRealFile(filepath.Join(e.dir, MetaDirname, DeckSettingsFile)) || isRealFile(filepath.Join(e.dir, DeckSettingsFile)):
		return "slides"
	}
	return "site"
}

// TrashFor are the person's trashed projects, the latest deleted first; one per id.
func (l *Library) TrashFor(person string) []TrashItem {
	out := []TrashItem{}
	seen := map[string]bool{}
	for _, e := range l.trashEntries("") {
		if seen[e.id] || !l.ownedTrash(e, person) {
			continue
		}
		seen[e.id] = true
		out = append(out, TrashItem{ID: e.id, Title: trashTitle(e), DeletedAt: e.at, Kind: trashKind(e)})
	}
	return out
}

// RestoreProject puts the project's latest trashed folder back. The gateway hears of it as of a new project of the person.
func (l *Library) RestoreProject(id, person string) error {
	if !IsLibraryID(id) {
		return &ProjectDeleteError{Msg: locale.Pick("Unknown design", "Неизвестный макет"), Status: http.StatusBadRequest}
	}
	m := l.lockOf("project\x00" + id)
	m.Lock()
	defer m.Unlock()
	defer l.listing.invalidatePath(id)
	var found *trashEntry
	for _, e := range l.trashEntries(id) {
		if l.ownedTrash(e, person) {
			found = &e
			break
		}
	}
	if found == nil {
		return &ProjectDeleteError{Msg: locale.Pick("The project is not in the trash", "Проекта нет в корзине"), Status: http.StatusNotFound}
	}
	if _, err := os.Lstat(l.ArtifactDir(id)); err == nil {
		return &ProjectDeleteError{Msg: locale.Pick("A project with this name is in the library already", "В библиотеке уже есть проект с таким именем"), Status: http.StatusConflict}
	}
	if err := os.Rename(found.dir, l.ArtifactDir(id)); err != nil {
		return err
	}
	l.AnnounceNewBy(id, person)
	return nil
}

// PurgeProject erases the project's trashed folders for good.
func (l *Library) PurgeProject(id, person string) error {
	if !IsLibraryID(id) {
		return &ProjectDeleteError{Msg: locale.Pick("Unknown design", "Неизвестный макет"), Status: http.StatusBadRequest}
	}
	m := l.lockOf("project\x00" + id)
	m.Lock()
	defer m.Unlock()
	n := 0
	for _, e := range l.trashEntries(id) {
		if !l.ownedTrash(e, person) {
			continue
		}
		if err := os.RemoveAll(e.dir); err != nil {
			return err
		}
		n++
	}
	if n == 0 {
		return &ProjectDeleteError{Msg: locale.Pick("The project is not in the trash", "Проекта нет в корзине"), Status: http.StatusNotFound}
	}
	return nil
}
