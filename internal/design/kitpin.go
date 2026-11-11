package design

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ly-solutions/harnessly/internal/designkit"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/locale"
)

// A project is pinned to a version of a UI kit: meta/project.json says {"kit": {"id", "version", "mounts"}} and the
// project's boards keep addressing the kit as ds/<mount>/... (a theme of the kit, or the whole kit under its id). The
// server answers those paths from the pinned version, /kit/<id>/<version>/themes/<mount>/..., so the project carries no
// copy and every project on a version sees the same files. A project switched to the kit's draft ("draft": true) is
// answered from the draft's folder instead, and sees an edit of the kit the moment it is saved. A project that still
// has a folder ds/<mount>/ of its own keeps it: the copy wins until it is taken away.

// KitPin is a project's kit.
type KitPin struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	// Mounts are the names under ds/ the project takes from the kit; empty is every mount the kit offers.
	Mounts []string `json:"mounts,omitempty"`
	// Draft: the project is shown on the kit's draft for now, Version is where it goes back to.
	Draft bool `json:"draft,omitempty"`
	// Dismissed is the newer version the person chose to stay away from; it is not offered again.
	Dismissed string `json:"dismissed,omitempty"`
}

const kitPinKey = "kit"

type kitPinEntry struct {
	mod  int64
	size int64
	pin  *KitPin
}

type kitDraftEntry struct {
	mod   int64
	draft *designkit.Draft
}

type kitPinState struct {
	mu     sync.Mutex
	pins   map[string]kitPinEntry
	drafts map[string]kitDraftEntry
}

func parseKitPin(v any) *KitPin {
	o, ok := v.(*jsvalue.Object)
	if !ok {
		return nil
	}
	p := KitPin{ID: o.String("id"), Version: o.String("version"), Dismissed: o.String("dismissed")}
	if !designkit.ValidID(p.ID) || !designkit.ValidVersion(p.Version) {
		return nil
	}
	if p.Dismissed != "" && !designkit.ValidVersion(p.Dismissed) {
		p.Dismissed = ""
	}
	p.Draft, _ = get(o, "draft").(bool)
	for _, m := range jsvalue.Strings(o.Array("mounts")) {
		if designkit.ValidID(m) {
			p.Mounts = append(p.Mounts, m)
		}
	}
	return &p
}

func (p KitPin) value() *jsvalue.Object {
	o := jsvalue.NewObject("id", p.ID, "version", p.Version)
	if len(p.Mounts) > 0 {
		o.Set("mounts", jsvalue.StringArray(p.Mounts))
	}
	if p.Draft {
		o.Set("draft", true)
	}
	if p.Dismissed != "" {
		o.Set("dismissed", p.Dismissed)
	}
	return o
}

// KitPinOf is the project's kit, read from meta/project.json; cached by the file's time and size.
func (l *Library) KitPinOf(project string) (KitPin, bool) {
	if !IsLibraryID(project) {
		return KitPin{}, false
	}
	file := filepath.Join(l.MetaDir(project), MetaProjectFile)
	st, err := os.Stat(file)
	if err != nil || !st.Mode().IsRegular() {
		return KitPin{}, false
	}
	key := l.Root() + "\x00" + project
	c := &l.kitPins
	c.mu.Lock()
	e, ok := c.pins[key]
	c.mu.Unlock()
	if !ok || e.mod != st.ModTime().UnixNano() || e.size != st.Size() {
		e = kitPinEntry{mod: st.ModTime().UnixNano(), size: st.Size()}
		if o, ok := readSmallJSON(file); ok {
			e.pin = parseKitPin(get(o, kitPinKey))
		}
		c.mu.Lock()
		if c.pins == nil {
			c.pins = map[string]kitPinEntry{}
		}
		c.pins[key] = e
		c.mu.Unlock()
	}
	if e.pin == nil {
		return KitPin{}, false
	}
	return *e.pin, true
}

// KitDraft is the kit's draft, cached by its record's time.
func (l *Library) KitDraft(kit string) (designkit.Draft, bool) {
	if !designkit.ValidID(kit) {
		return designkit.Draft{}, false
	}
	file := filepath.Join(l.KitsDir(), kit, ".draft.json")
	st, err := os.Stat(file)
	if err != nil {
		return designkit.Draft{}, false
	}
	c := &l.kitPins
	c.mu.Lock()
	e, ok := c.drafts[kit]
	c.mu.Unlock()
	if !ok || e.mod != st.ModTime().UnixNano() {
		e = kitDraftEntry{mod: st.ModTime().UnixNano()}
		if d, ok := designkit.ReadDraft(l.KitsDir(), kit); ok {
			e.draft = &d
		}
		c.mu.Lock()
		if c.drafts == nil {
			c.drafts = map[string]kitDraftEntry{}
		}
		c.drafts[kit] = e
		c.mu.Unlock()
	}
	if e.draft == nil || !IsLibraryID(e.draft.Project) {
		return designkit.Draft{}, false
	}
	return *e.draft, true
}

// kitRootOf is the folder the pin's files come from: the draft's when the project is on it (and the draft is still
// there), else the pinned version's; "" when neither is.
func (l *Library) kitRootOf(p KitPin) string {
	if p.Draft {
		if d, ok := l.KitDraft(p.ID); ok {
			if dir := l.ProjectDirOf(d.Project); isRealDir(dir) {
				return dir
			}
		}
	}
	return designkit.Root(l.KitsDir(), p.ID, p.Version)
}

func (p KitPin) mounts(mount string) bool {
	if len(p.Mounts) == 0 {
		return true
	}
	for _, m := range p.Mounts {
		if m == mount {
			return true
		}
	}
	return false
}

// kitFile is a file a project's ds/<mount>/... names, from the project's kit; "" when the project has no kit, keeps
// its own copy of that mount, or the kit has no such file. The file is confined to the mount's folder.
func (l *Library) kitFile(project string, segments []string) string {
	pin, ok := l.KitPinOf(project)
	if !ok {
		return ""
	}
	if file := l.mountFile(project, pin, l.kitRootOf(pin), segments); file != "" {
		return file
	}
	return l.baseFile(project, pin, segments)
}

// baseFile is a file of a kit the project's kit is built on, the nearest base first: a board on the kit loads
// the base's ds/<mount>/ too. "" when the kit extends nothing or the file is in none of them.
func (l *Library) baseFile(project string, pin KitPin, segments []string) string {
	if pin.Draft || len(segments) < 3 || segments[0] != "ds" {
		return ""
	}
	c, err := designkit.ReadCanvas(l.KitsDir(), pin.ID, pin.Version, "")
	if err != nil {
		return ""
	}
	for i := len(c.Bases) - 1; i >= 0; i-- {
		b := c.Bases[i]
		if f := l.mountFile(project, KitPin{ID: b.Kit, Version: b.Version}, designkit.Root(l.KitsDir(), b.Kit, b.Version), segments); f != "" {
			return f
		}
	}
	return ""
}

// kitFileAt is kitFile on another installed version of the project's kit (a look before taking it).
func (l *Library) kitFileAt(project, version string, segments []string) string {
	pin, ok := l.KitPinOf(project)
	if !ok {
		return ""
	}
	return l.mountFile(project, pin, designkit.Root(l.KitsDir(), pin.ID, version), segments)
}

func (l *Library) mountFile(project string, pin KitPin, root string, segments []string) string {
	if root == "" || len(segments) < 3 || segments[0] != "ds" {
		return ""
	}
	for _, s := range segments {
		if s == "" || s == "." || s == ".." || strings.ContainsAny(s, "\\\x00") {
			return ""
		}
	}
	mount := segments[1]
	if !pin.mounts(mount) {
		return ""
	}
	realBase := mountInside(root, designkit.MountOf(root, pin.ID, mount))
	if realBase == "" {
		return ""
	}
	real, err := filepath.EvalSymlinks(filepath.Join(append([]string{realBase}, segments[2:]...)...))
	if err != nil || !strings.HasPrefix(real, realBase+string(filepath.Separator)) {
		return ""
	}
	if st, err := os.Stat(real); err != nil || !st.Mode().IsRegular() {
		return ""
	}
	return real
}

// kitMissing maps a path under the project's folder that is not there to the kit's file, for the asset versions of a
// board; "" when it is not a kit's.
func (l *Library) kitMissing(project, projectRoot string) func(abs string) string {
	if _, ok := l.KitPinOf(project); !ok {
		return nil
	}
	return func(abs string) string {
		rel, err := filepath.Rel(projectRoot, filepath.Clean(abs))
		if err != nil || strings.HasPrefix(rel, "..") {
			return ""
		}
		return l.kitFile(project, strings.Split(filepath.ToSlash(rel), "/"))
	}
}

// KitMountDirs are the folders the project's ds/<mount>/ come from, by mount: what an export or a published site adds
// to the project's own files. A file the project has itself wins over the kit's one at the same path.
func (l *Library) KitMountDirs(project string) map[string]string {
	out := map[string]string{}
	pin, ok := l.KitPinOf(project)
	if !ok {
		return out
	}
	root := l.kitRootOf(pin)
	if root == "" {
		return out
	}
	names := pin.Mounts
	if len(names) == 0 {
		names = designkit.Mounts(root, pin.ID)
	}
	for _, m := range names {
		if dir := mountInside(root, designkit.MountOf(root, pin.ID, m)); dir != "" {
			out[m] = dir
		}
	}
	return out
}

// mountInside is a theme folder of a kit by its real path, when that path is inside the kit's own: a folder that is a
// link out of the kit is not served, exported or published.
func mountInside(root, dir string) string {
	if root == "" || dir == "" {
		return ""
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return ""
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil || !strings.HasPrefix(real, realRoot+string(filepath.Separator)) {
		return ""
	}
	return real
}

// KitPinError is a refusal of a pin change worded for the person.
type KitPinError struct{ Msg string }

func (e *KitPinError) Error() string { return e.Msg }

// SetKitPin writes the project's kit into meta/project.json; nil takes it away. Other keys of the file stay.
func (l *Library) SetKitPin(project string, pin *KitPin) error {
	m := l.lockOf("kitpin\x00" + project)
	m.Lock()
	defer m.Unlock()
	model := l.ReadModel(project)
	if model == nil {
		return &KitPinError{locale.Pick("The project has no model folder: a kit cannot be pinned to it", "У проекта нет папки meta: кит к нему не закрепить")}
	}
	meta := model.Meta
	if meta.raw == nil {
		meta.raw = jsvalue.NewObject()
	} else {
		meta.raw = meta.raw.Copy()
	}
	if pin == nil {
		meta.raw.Delete(kitPinKey)
	} else {
		if !designkit.ValidID(pin.ID) || !designkit.ValidVersion(pin.Version) {
			return errors.New("invalid kit pin")
		}
		meta.raw.Set(kitPinKey, pin.value())
	}
	_, err := l.writeProjectMeta(project, meta)
	return err
}

// ProjectsOnKit are the library's projects pinned to the kit, with their pins.
func (l *Library) ProjectsOnKit(kit string) map[string]KitPin {
	out := map[string]KitPin{}
	for _, a := range l.ListArtifacts() {
		if p, ok := l.KitPinOf(a.ID); ok && p.ID == kit {
			out[a.ID] = p
		}
	}
	return out
}

// draftPathFor is a change in a kit's draft folder (rel, under the library) as the project on that draft sees it,
// ds/<mount>/...; "" when the project is not on a draft or the file is no part of what it mounts.
func (l *Library) draftPathFor(project, rel string) string {
	pin, ok := l.KitPinOf(project)
	if !ok || !pin.Draft {
		return ""
	}
	d, ok := l.KitDraft(pin.ID)
	if !ok {
		return ""
	}
	prefix := d.Project + "/" + ProjectDirname + "/"
	if !strings.HasPrefix(rel, prefix) {
		return ""
	}
	inner := rel[len(prefix):]
	if theme, ok := strings.CutPrefix(inner, "themes/"); ok {
		mount, rest, found := strings.Cut(theme, "/")
		if !found || rest == "" || !pin.mounts(mount) || isRealFile(filepath.Join(l.ProjectDir(project), "ds", mount, filepath.FromSlash(rest))) {
			return ""
		}
		return "ds/" + mount + "/" + rest
	}
	// a kit without themes mounts whole under its id
	if pin.mounts(pin.ID) && !isRealDir(filepath.Join(l.ProjectDirOf(d.Project), "themes")) && !isRealFile(filepath.Join(l.ProjectDir(project), "ds", pin.ID, filepath.FromSlash(inner))) {
		return "ds/" + pin.ID + "/" + inner
	}
	return ""
}

// mountLayoutTheme makes a kit's theme reachable as ds/<theme>/ for a page made from one of its layouts: the project is
// pinned to the kit's version (the theme added to its mounts). A project that keeps its own copy of the theme, is on
// another kit or has no model folder gets the theme copied in, as before pins.
func (l *Library) mountLayoutTheme(project, root, kit, version, theme string) error {
	pin, ok := l.KitPinOf(project)
	if !(ok && pin.ID == kit) && isRealDir(filepath.Join(l.ProjectDir(project), "ds", theme)) {
		return nil
	}
	switch {
	case ok && pin.ID == kit:
		if pin.mounts(theme) {
			return nil
		}
		pin.Mounts = append(pin.Mounts, theme)
		return l.SetKitPin(project, &pin)
	case !ok && l.ReadModel(project) != nil:
		return l.SetKitPin(project, &KitPin{ID: kit, Version: version, Mounts: []string{theme}})
	}
	_, err := designkit.InstallTheme(root, theme, l.ProjectDir(project))
	return err
}

// kitMissingAt is kitMissing on another version of the kit.
func (l *Library) kitMissingAt(project, version, projectRoot string) func(abs string) string {
	return func(abs string) string {
		rel, err := filepath.Rel(projectRoot, filepath.Clean(abs))
		if err != nil || strings.HasPrefix(rel, "..") {
			return ""
		}
		return l.kitFileAt(project, version, strings.Split(filepath.ToSlash(rel), "/"))
	}
}
