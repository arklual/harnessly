package design

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/locale"
)

// The versions of a project, on the library's history (history.go): every snapshot that touched the project is a
// point of its history, and a named version is a point the person gave a name ("Для показа клиенту"). The names live
// next to the history repository (versions/<project>.json) with a ref per version, so the point is never collected.
// A version can be opened read only (the pinned library of pinned.go serves it, with the kit it was on), compared with
// the project as it is, and restored: what is there now is saved first, so a restore can be undone the same way.

const maxNamedVersions = 200

// NamedVersion is a point of the project's history with a name.
type NamedVersion struct {
	ID   string  `json:"id"`
	Name string  `json:"name"`
	At   int64   `json:"at"`
	Who  string  `json:"who"`
	Kit  *KitPin `json:"kit"`
}

// ProjectPoint is one point of a project's history.
type ProjectPoint struct {
	ID      string `json:"id"`
	At      int64  `json:"at"`
	Who     string `json:"who"`
	Agent   bool   `json:"agent"`
	Subject string `json:"subject"`
	// Name is the version's name when the person named this point.
	Name string `json:"name,omitempty"`
}

// ProjectVersions is the project's history for the "Версии" panel.
type ProjectVersions struct {
	Points []ProjectPoint `json:"points"`
	Named  []NamedVersion `json:"named"`
	// Unsaved: the project has changes the history has not taken yet.
	Unsaved bool `json:"unsaved"`
}

func (l *Library) namedFile(project string) (string, error) {
	repo, err := l.historyRepo()
	if err != nil {
		return "", err
	}
	return filepath.Join(repo.gitDir, "versions", project+".json"), nil
}

func (l *Library) readNamed(project string) []NamedVersion {
	file, err := l.namedFile(project)
	if err != nil {
		return []NamedVersion{}
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return []NamedVersion{}
	}
	var out []NamedVersion
	if json.Unmarshal(raw, &out) != nil {
		return []NamedVersion{}
	}
	keep := out[:0]
	for _, v := range out {
		if fullSHA.MatchString(v.ID) && v.Name != "" {
			keep = append(keep, v)
		}
	}
	return keep
}

func (l *Library) writeNamed(project string, list []NamedVersion) error {
	file, err := l.namedFile(project)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	raw, _ := json.MarshalIndent(list, "", "  ")
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, file)
}

// isAgentEditor: a snapshot's editor line names a chat (OwnerLine), a person's names only them.
func isAgentEditor(editors []string) bool {
	for _, e := range editors {
		if strings.Contains(e, " · chat ") || strings.Contains(e, " · чат ") {
			return true
		}
	}
	return false
}

// ProjectHistory is the project's points (the newest first, at most limit) and its named versions.
func (l *Library) ProjectHistory(project string, limit float64) (ProjectVersions, error) {
	if l.ReadArtifact(project) == nil {
		return ProjectVersions{}, historyErr(locale.Pick("No such project", "Нет такого проекта"))
	}
	h, err := l.readHistory(project, nil, limit)
	if err != nil {
		return ProjectVersions{}, err
	}
	named := l.readNamed(project)
	names := map[string]string{}
	for _, n := range named {
		names[n.ID] = n.Name
	}
	out := ProjectVersions{Points: []ProjectPoint{}, Named: named, Unsaved: h.unsaved}
	for _, e := range h.entries {
		out.Points = append(out.Points, ProjectPoint{ID: e.ID, At: e.At, Who: WhoOf(e), Agent: isAgentEditor(e.Editors), Subject: e.Subject, Name: names[e.ID]})
	}
	sort.SliceStable(out.Named, func(i, j int) bool { return out.Named[i].At > out.Named[j].At })
	return out, nil
}

// snapshotProject takes what the project has now into the history and answers the revision holding it.
func (l *Library) snapshotProject(who, subject string, now time.Time) (string, error) {
	h := &l.hist
	h.mu.Lock()
	runtime := h.runtime
	h.mu.Unlock()
	var editors []string
	if who != "" {
		editors = []string{who}
	}
	out, err := l.snapshotNow(runtime, editors, subject, now)
	if err != nil {
		return "", err
	}
	if out.kind == SnapshotBusy {
		return "", historyErr(locale.Pick("The history is busy with another save: try again in a few seconds", "История сейчас занята другим сохранением: повторите через несколько секунд"))
	}
	repo, err := l.ensureRepo()
	if err != nil {
		return "", err
	}
	sha, err := l.gitOK(repo, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return "", historyErr(locale.Pick("The history has nothing saved yet", "В истории пока ничего не сохранено"))
	}
	return strings.TrimSpace(sha), nil
}

// SaveProjectVersion is "Сохранить версию": the project as it is now, under a name.
func (l *Library) SaveProjectVersion(project, name, who string) (NamedVersion, error) {
	if l.ReadArtifact(project) == nil {
		return NamedVersion{}, historyErr(locale.Pick("No such project", "Нет такого проекта"))
	}
	clean, ok := CleanText(jscompat.Trim(name), 80)
	if !ok || clean == "" {
		return NamedVersion{}, historyErr(locale.Pick("Name the version", "Назовите версию"))
	}
	l.hist.chain.Lock()
	defer l.hist.chain.Unlock()
	now := l.clock()
	rev, err := l.snapshotProject(who, locale.Pick("Version \""+clean+"\"", "Версия «"+clean+"»"), now)
	if err != nil {
		return NamedVersion{}, err
	}
	repo, err := l.ensureRepo()
	if err != nil {
		return NamedVersion{}, err
	}
	v := NamedVersion{ID: rev, Name: clean, At: now.UnixMilli(), Who: who}
	if pin, ok := l.KitPinOf(project); ok {
		v.Kit = &pin
	}
	list := l.readNamed(project)
	kept := []NamedVersion{v}
	for _, old := range list {
		if old.ID != rev {
			kept = append(kept, old)
		}
	}
	if len(kept) > maxNamedVersions {
		return NamedVersion{}, historyErr(locale.Pick("The project has too many named versions", "У проекта слишком много именованных версий"))
	}
	if _, err := l.gitOK(repo, "update-ref", "refs/versions/"+project+"/"+rev, rev); err != nil {
		return NamedVersion{}, err
	}
	return v, l.writeNamed(project, kept)
}

// RenameProjectVersion renames a named version; an empty name takes the name away (the point stays in the history).
func (l *Library) RenameProjectVersion(project, rev, name string) error {
	l.hist.chain.Lock()
	defer l.hist.chain.Unlock()
	list := l.readNamed(project)
	clean, _ := CleanText(jscompat.Trim(name), 80)
	out := list[:0]
	found := false
	for _, v := range list {
		if v.ID == rev {
			found = true
			if clean == "" {
				continue
			}
			v.Name = clean
		}
		out = append(out, v)
	}
	if !found {
		return historyErr(locale.Pick("No such version", "Такой версии нет"))
	}
	if clean == "" {
		if repo, err := l.ensureRepo(); err == nil {
			_, _ = l.git(repo, "update-ref", "-d", "refs/versions/"+project+"/"+rev)
		}
	}
	return l.writeNamed(project, out)
}

// ProjectChange is one board (or other file) of the project changed between a version and now.
type ProjectChange struct {
	Path string `json:"path"`
	// Kind is "added", "changed" or "removed" (from the version to now).
	Kind  string `json:"kind"`
	Board string `json:"board,omitempty"`
}

// ProjectDiff is a version compared with the project as it is.
type ProjectDiff struct {
	Revision string          `json:"revision"`
	Boards   []ProjectChange `json:"boards"`
	// Other counts the changed files that are not boards (pictures, styles, the model).
	Other int `json:"other"`
	// KitThen and KitNow are the kit the project was on at the version and is on now.
	KitThen *KitPin `json:"kitThen"`
	KitNow  *KitPin `json:"kitNow"`
}

func (l *Library) resolveRevision(repo historyRepo, rev, project string) (string, error) {
	if !revisionRe.MatchString(rev) {
		return "", historyErr(locale.Pick("Could not tell which version", "Не удалось понять, какая версия"))
	}
	res, err := l.git(repo, "rev-parse", "--verify", "--quiet", "--end-of-options", rev+"^{commit}")
	if err != nil {
		return "", err
	}
	if res.code != 0 {
		return "", historyErr(locale.Pick("No such version in the history", "Такой версии нет в истории"))
	}
	full := strings.TrimSpace(res.stdout)
	there, err := l.git(repo, "cat-file", "-e", full+":"+project)
	if err != nil {
		return "", err
	}
	if there.code != 0 {
		return "", historyErr(locale.Pick("The project did not exist in that version yet", "В этой версии проекта ещё не было"))
	}
	return full, nil
}

func (l *Library) pinAt(repo historyRepo, rev, project string) *KitPin {
	res, err := l.git(repo, "show", rev+":"+project+"/"+MetaDirname+"/"+MetaProjectFile)
	if err != nil || res.code != 0 {
		return nil
	}
	raw, err := jsvalue.Parse([]byte(res.stdout))
	if err != nil {
		return nil
	}
	o, ok := raw.(*jsvalue.Object)
	if !ok {
		return nil
	}
	return parseKitPin(get(o, kitPinKey))
}

// DiffProject compares a version of the project with the project as it is now (saved first).
func (l *Library) DiffProject(project, rev string) (ProjectDiff, error) {
	if l.ReadArtifact(project) == nil {
		return ProjectDiff{}, historyErr(locale.Pick("No such project", "Нет такого проекта"))
	}
	l.hist.chain.Lock()
	defer l.hist.chain.Unlock()
	repo, err := l.ensureRepo()
	if err != nil {
		return ProjectDiff{}, err
	}
	full, err := l.resolveRevision(repo, rev, project)
	if err != nil {
		return ProjectDiff{}, err
	}
	head, err := l.snapshotProject("", "", l.clock())
	if err != nil {
		return ProjectDiff{}, err
	}
	out, err := l.gitOK(repo, "diff", "--name-status", "-z", "--no-renames", full, head, "--", project)
	if err != nil {
		return ProjectDiff{}, err
	}
	d := ProjectDiff{Revision: full, Boards: []ProjectChange{}, KitThen: l.pinAt(repo, full, project), KitNow: l.pinAt(repo, head, project)}
	kinds := map[string]string{"A": "added", "M": "changed", "D": "removed", "T": "changed"}
	for _, s := range parseNameStatus(out) {
		if _, board, ok := BoardOfPath(s.path); ok {
			d.Boards = append(d.Boards, ProjectChange{Path: strings.TrimPrefix(s.path, project+"/"+ProjectDirname+"/"), Kind: kinds[s.status[:1]], Board: board})
			continue
		}
		d.Other++
	}
	return d, nil
}

// RestoreProjectVersion puts the whole project back as it was at a version: what is there now is saved first, the version's
// files are checked out over the folder, files the version did not have are taken away, and the result is a snapshot
// of its own. A project some other live chat holds a board of is not touched.
func (l *Library) RestoreProjectVersion(project, rev, who string) (string, error) {
	if l.ReadArtifact(project) == nil {
		return "", historyErr(locale.Pick("No such project", "Нет такого проекта"))
	}
	now := l.clock()
	for _, c := range l.LiveClaims(project, now.UnixMilli()) {
		return "", historyErr(locale.Pick("The board \""+c.Board+"\" is being edited by "+c.Owner.User+" (chat \""+c.Owner.ChatTitle+"\"): a version can be restored when they finish",
			"Доску «"+c.Board+"» сейчас правит "+c.Owner.User+" (чат «"+c.Owner.ChatTitle+"»): вернуть версию можно, когда он закончит"))
	}
	l.hist.chain.Lock()
	defer l.hist.chain.Unlock()
	repo, err := l.ensureRepo()
	if err != nil {
		return "", err
	}
	if !isPlainFolder(repo.root, project) {
		return "", historyErr(locale.Pick("The project folder is not a plain folder: a version cannot be restored", "Папка проекта не обычная папка: вернуть версию нельзя"))
	}
	full, err := l.resolveRevision(repo, rev, project)
	if err != nil {
		return "", err
	}
	short := full[:7]
	head, err := l.snapshotProject(who, locale.Pick("Before restoring the project to "+short, "Перед возвратом проекта к версии "+short), now)
	if err != nil {
		return "", err
	}
	added, err := l.gitOK(repo, "diff", "--name-only", "-z", "--no-renames", "--diff-filter=A", full, head, "--", project)
	if err != nil {
		return "", err
	}
	if _, err := l.gitOK(repo, "restore", "--source="+full, "--worktree", "--", project); err != nil {
		return "", err
	}
	for _, rel := range strings.Split(added, "\x00") {
		if rel == "" || !strings.HasPrefix(rel, project+"/") || strings.Contains(rel, "..") {
			continue
		}
		_ = os.Remove(filepath.Join(repo.root, filepath.FromSlash(rel)))
	}
	l.listing.invalidatePath(project)
	snap, err := l.snapshotProject(who, locale.Pick("Restored the project to version ", "Вернул проект к версии ")+short, now)
	if err != nil {
		return "", err
	}
	return snap, nil
}

// VersionLink is the address (from the runtime's root) of a project's point of history opened read only, on its first
// page, as the "Версии" panel opens it; "" for a project with no page.
func (l *Library) VersionLink(project, rev string) string {
	a := l.ReadArtifact(project)
	if a == nil || len(a.Boards) == 0 || rev == "" {
		return ""
	}
	parts := strings.Split(pageFileOf(a.Boards[0].Path), "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return "/a/" + url.PathEscape(project) + "/r/" + url.PathEscape(rev) + "/" + strings.Join(parts, "/")
}
