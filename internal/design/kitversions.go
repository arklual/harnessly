package design

import (
	"errors"
	"os"
	"path/filepath"
	"sort"

	"github.com/ly-solutions/harnessly/internal/designkit"
	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/locale"
)

// The versions of a kit as the library works them: one draft per kit (a project of the library, so that people and
// agents edit it like any design, its edits are in the history and its changes stream to the pages that show it),
// published as new immutable versions. A project goes on the draft for the time of an edit ("Править в ките") and
// comes back on the version the draft is published as; every other project stays on its version until the person
// takes a newer one.

const kitDraftKey = "kitDraft"

// KitError is a refusal of a kit operation worded for the person.
type KitError struct {
	Msg    string
	Status int
}

func (e *KitError) Error() string { return e.Msg }

func kitErr(status int, en, ru string) error {
	return &KitError{Msg: locale.Pick(en, ru), Status: status}
}

// KitDraftState is a kit's draft as the screen shows it.
type KitDraftState struct {
	Project      string             `json:"project"`
	Base         string             `json:"base"`
	CreatedAt    int64              `json:"createdAt"`
	Changes      []designkit.Change `json:"changes"`
	ChangedCount int                `json:"changedCount"`
}

// KitUse is a project on a kit.
type KitUse struct {
	Project string `json:"project"`
	Title   string `json:"title"`
	Version string `json:"version"`
	Draft   bool   `json:"draft"`
}

// KitState is a kit with its versions, its draft and the projects on it.
type KitState struct {
	ID string `json:"id"`
	// Settings: whether the kit is for viewing only, and whether the person asking made it (KitVersionsState).
	Settings KitSettingsState `json:"settings"`
	// MayEdit: the person may start or edit the draft now (KitVersionsState).
	MayEdit  bool                  `json:"mayEdit"`
	Latest   string                `json:"latest"`
	Versions []designkit.Published `json:"versions"`
	Draft    *KitDraftState        `json:"draft"`
	Projects []KitUse              `json:"projects"`
}

func (l *Library) kitVersions(kit string) []designkit.Published {
	out := []designkit.Published{}
	for _, v := range designkit.Versions(l.KitsDir(), kit) {
		if p, ok := designkit.ReadPublished(l.KitsDir(), kit, v.Version); ok {
			out = append(out, p)
		}
	}
	return out
}

// liveDraft is the kit's draft while its folder is there.
func (l *Library) liveDraft(kit string) (designkit.Draft, bool) {
	d, ok := l.KitDraft(kit)
	if !ok || !isRealDir(l.ProjectDirOf(d.Project)) {
		return designkit.Draft{}, false
	}
	// A draft started before the kit screen read it records no folder: it gets it once.
	if dir := l.ProjectDir(d.Project); d.Dir != dir {
		d.Dir = dir
		if err := designkit.WriteDraft(l.KitsDir(), kit, d); err != nil {
			l.warn("design kit draft: record its folder", err)
		}
	}
	return d, true
}

func (l *Library) draftChanges(kit string, d designkit.Draft) ([]designkit.Change, error) {
	base := designkit.Root(l.KitsDir(), kit, d.Base)
	if base == "" {
		return nil, kitErr(409, "The version the draft started from is gone", "Версии, от которой начат черновик, больше нет")
	}
	return designkit.Changes(base, l.ProjectDir(d.Project), d.Skip)
}

// KitState is the kit's versions, draft and projects; with changes, the draft's changed files are compared.
func (l *Library) KitState(kit string, changes bool) (KitState, error) {
	if !designkit.ValidID(kit) {
		return KitState{}, kitErr(400, "Unknown kit", "Неизвестный кит")
	}
	st := KitState{ID: kit, Versions: l.kitVersions(kit), Projects: []KitUse{}}
	if len(st.Versions) == 0 {
		return KitState{}, kitErr(404, "The kit is not installed", "Кит не установлен")
	}
	st.Latest = st.Versions[0].Version.Version
	if d, ok := l.liveDraft(kit); ok {
		ds := &KitDraftState{Project: d.Project, Base: d.Base, CreatedAt: d.CreatedAt, Changes: []designkit.Change{}}
		if changes {
			list, err := l.draftChanges(kit, d)
			if err != nil {
				return KitState{}, err
			}
			ds.ChangedCount = len(list)
			if len(list) > designkit.MaxChangesListed {
				list = list[:designkit.MaxChangesListed]
			}
			if list != nil {
				ds.Changes = list
			}
		}
		st.Draft = ds
	}
	for id, p := range l.ProjectsOnKit(kit) {
		title := id
		if a := l.ReadArtifact(id); a != nil {
			title = a.Title
		}
		st.Projects = append(st.Projects, KitUse{Project: id, Title: title, Version: p.Version, Draft: p.Draft})
	}
	sort.Slice(st.Projects, func(i, j int) bool { return st.Projects[i].Title < st.Projects[j].Title })
	return st, nil
}

// StartKitDraft is the kit's draft, made from its newest version when it has none: a library project whose folder
// is a copy of the version.
func (l *Library) StartKitDraft(kit string) (designkit.Draft, error) {
	if !designkit.ValidID(kit) {
		return designkit.Draft{}, kitErr(400, "Unknown kit", "Неизвестный кит")
	}
	if l.KitReadOnly(kit) {
		return designkit.Draft{}, errKitReadOnly()
	}
	m := l.lockOf("kitdraft\x00" + kit)
	m.Lock()
	defer m.Unlock()
	if d, ok := l.liveDraft(kit); ok {
		return d, nil
	}
	latest, ok := designkit.Latest(l.KitsDir(), kit)
	if !ok {
		return designkit.Draft{}, kitErr(404, "The kit is not installed", "Кит не установлен")
	}
	created, err := l.CreateProject(locale.Pick("Kit draft: ", "Черновик кита ") + kit)
	if err != nil {
		return designkit.Draft{}, err
	}
	dir := l.ProjectDir(created.ID)
	own, _ := readNames(dir)
	if _, _, err := designkit.CopyTree(designkit.Root(l.KitsDir(), kit, latest.Version), dir, nil); err != nil {
		_ = l.DeleteProject(created.ID)
		return designkit.Draft{}, err
	}
	l.watchTree(dir)
	if err := designkit.SeedDraftPrep(l.KitsDir(), kit, latest.Version); err != nil {
		l.warn("design kit draft: its atlases", err)
	}
	d := designkit.Draft{Project: created.ID, Base: latest.Version, CreatedAt: l.clock().UnixMilli(), Skip: own, Dir: dir}
	if err := l.setMetaKey(created.ID, kitDraftKey, kit); err != nil {
		l.warn("design kit draft: mark the project", err)
	}
	if err := designkit.WriteDraft(l.KitsDir(), kit, d); err != nil {
		_ = l.DeleteProject(created.ID)
		return designkit.Draft{}, err
	}
	return d, nil
}

// PublishKitDraft makes the draft the kit's next version: part is patch, minor (the default) or major of the newest
// version; notes say what changed. The projects that were on the draft go onto the new version, and the draft goes on
// from it.
func (l *Library) PublishKitDraft(kit, part, notes string) (designkit.Published, error) {
	if l.KitReadOnly(kit) {
		return designkit.Published{}, errKitReadOnly()
	}
	m := l.lockOf("kitdraft\x00" + kit)
	m.Lock()
	defer m.Unlock()
	d, ok := l.liveDraft(kit)
	if !ok {
		return designkit.Published{}, kitErr(404, "The kit has no draft", "У кита нет черновика")
	}
	changes, err := l.draftChanges(kit, d)
	if err != nil {
		return designkit.Published{}, err
	}
	if len(changes) == 0 {
		return designkit.Published{}, kitErr(409, "The draft has no edits to publish", "В черновике нет правок для публикации")
	}
	latest, ok := designkit.Latest(l.KitsDir(), kit)
	if !ok {
		return designkit.Published{}, kitErr(404, "The kit is not installed", "Кит не установлен")
	}
	version, err := designkit.Bump(latest.Version, part)
	if err != nil {
		return designkit.Published{}, kitErr(400, err.Error(), "Номер версии: patch, minor или major")
	}
	text, _ := CleanText(jscompat.Trim(notes), 500)
	pub, err := designkit.Publish(l.KitsDir(), kit, version, l.ProjectDir(d.Project), d.Skip, designkit.PublishOptions{Notes: text, Changes: changes, From: d.Base, Now: l.clock})
	if errors.Is(err, designkit.ErrExists) {
		return designkit.Published{}, kitErr(409, "Version "+version+" is already published", "Версия "+version+" уже опубликована")
	}
	if err != nil {
		return designkit.Published{}, err
	}
	designkit.SeedVersionPrep(l.KitsDir(), kit, version)
	d.Base = version
	if err := designkit.StampVersion(l.ProjectDir(d.Project), kit, version); err != nil {
		return pub, err
	}
	if err := designkit.WriteDraft(l.KitsDir(), kit, d); err != nil {
		return pub, err
	}
	for id, p := range l.ProjectsOnKit(kit) {
		if !p.Draft {
			continue
		}
		p.Version, p.Draft, p.Dismissed = version, false, ""
		if err := l.SetKitPin(id, &p); err != nil {
			l.warn("design kit publish: move "+id+" to the new version", err)
		}
	}
	return pub, nil
}

// DiscardKitDraft puts the kit back to its last version: the projects on the draft go back to their versions and the
// draft's project goes to the library's trash, whole.
func (l *Library) DiscardKitDraft(kit string) error {
	if l.KitReadOnly(kit) {
		return errKitReadOnly()
	}
	m := l.lockOf("kitdraft\x00" + kit)
	m.Lock()
	defer m.Unlock()
	d, ok := l.KitDraft(kit)
	if !ok {
		return kitErr(404, "The kit has no draft", "У кита нет черновика")
	}
	for id, p := range l.ProjectsOnKit(kit) {
		if p.Draft {
			p.Draft = false
			if err := l.SetKitPin(id, &p); err != nil {
				return err
			}
		}
	}
	if l.ReadArtifact(d.Project) != nil {
		if err := l.DeleteProject(d.Project); err != nil {
			return err
		}
	}
	return designkit.ClearDraft(l.KitsDir(), kit)
}

// ProjectKitState is a project's kit as its header shows it.
type ProjectKitState struct {
	Pin *KitPin `json:"pin"`
	// Latest is the kit's newest version.
	Latest string `json:"latest,omitempty"`
	// Update is the newest version past the pin, unless the person chose to stay away from it.
	Update *designkit.Published `json:"update"`
	// DraftProject is the kit's draft, when it has one.
	DraftProject string `json:"draftProject,omitempty"`
	// KitDraftOf: the project is itself the draft of this kit.
	KitDraftOf string `json:"kitDraftOf,omitempty"`
}

// ProjectKit is the project's kit, its newer version and its draft.
func (l *Library) ProjectKit(project string) ProjectKitState {
	st := ProjectKitState{}
	if model := l.ReadModel(project); model != nil && model.Meta.raw != nil {
		if kit := model.Meta.raw.String(kitDraftKey); designkit.ValidID(kit) {
			if d, ok := l.KitDraft(kit); ok && d.Project == project {
				st.KitDraftOf = kit
			}
		}
	}
	pin, ok := l.KitPinOf(project)
	if !ok {
		return st
	}
	st.Pin = &pin
	if latest, ok := designkit.Latest(l.KitsDir(), pin.ID); ok {
		st.Latest = latest.Version
		if designkit.Compare(latest.Version, pin.Version) > 0 && latest.Version != pin.Dismissed {
			if p, ok := designkit.ReadPublished(l.KitsDir(), pin.ID, latest.Version); ok {
				st.Update = &p
			}
		}
	}
	if d, ok := l.liveDraft(pin.ID); ok {
		st.DraftProject = d.Project
	}
	return st
}

// EditKitFrom is "Править в ките": the kit's draft (made when there is none) with the project switched onto it.
func (l *Library) EditKitFrom(project string) (designkit.Draft, error) {
	pin, ok := l.KitPinOf(project)
	if !ok {
		return designkit.Draft{}, kitErr(409, "The project is not on a kit", "Проект не на ките")
	}
	if l.KitReadOnly(pin.ID) {
		return designkit.Draft{}, errKitReadOnly()
	}
	d, err := l.StartKitDraft(pin.ID)
	if err != nil {
		return designkit.Draft{}, err
	}
	if !pin.Draft {
		pin.Draft = true
		if err := l.SetKitPin(project, &pin); err != nil {
			return designkit.Draft{}, err
		}
	}
	return d, nil
}

// UseKitVersion puts the project on an installed version of its kit (off the draft); stay instead records that the
// person keeps the current one and the newer version is not offered again.
func (l *Library) UseKitVersion(project, version string, stay bool) (KitPin, error) {
	pin, ok := l.KitPinOf(project)
	if !ok {
		return KitPin{}, kitErr(409, "The project is not on a kit", "Проект не на ките")
	}
	if version == "" {
		version = pin.Version
	}
	if designkit.Root(l.KitsDir(), pin.ID, version) == "" {
		return KitPin{}, kitErr(404, "No such version of the kit", "Такой версии кита нет")
	}
	if stay {
		pin.Dismissed = version
	} else {
		pin.Version, pin.Draft, pin.Dismissed = version, false, ""
	}
	return pin, l.SetKitPin(project, &pin)
}

// setMetaKey writes one key of meta/project.json, keeping the rest.
func (l *Library) setMetaKey(project, key string, value any) error {
	m := l.lockOf("kitpin\x00" + project)
	m.Lock()
	defer m.Unlock()
	model := l.ReadModel(project)
	if model == nil {
		return errors.New("the project has no model folder")
	}
	meta := model.Meta
	if meta.raw == nil {
		meta.raw = jsvalue.NewObject()
	} else {
		meta.raw = meta.raw.Copy()
	}
	meta.raw.Set(key, value)
	_, err := l.writeProjectMeta(project, meta)
	return err
}

// kitFolderOf is where the agent finds the files of the project's kit (the version's folder or the draft's).
func (l *Library) kitFolderOf(project string) (KitPin, string, bool) {
	pin, ok := l.KitPinOf(project)
	if !ok {
		return KitPin{}, "", false
	}
	root := l.kitRootOf(pin)
	if root == "" {
		return KitPin{}, "", false
	}
	if real, err := filepath.EvalSymlinks(root); err == nil {
		root = real
	}
	if _, err := os.Stat(root); err != nil {
		return KitPin{}, "", false
	}
	return pin, root, true
}
