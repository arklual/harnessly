package design

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"syscall"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/locale"
)

// Releases of the board format. Boards load their engine as ./support.js; a project with a page model (meta/) is on
// the release its meta/project.json names ("legacy" when it names none) and gets that release's engine under that
// name, from a folder of immutable snapshots kept outside the library (RUNTIME_DESIGN_RELEASES_DIR, a checkout of a
// design-upstream mirror). Its releases.json lists the releases oldest first, each with its status: only a stable one
// is offered to projects. Without the folder, for a project without meta/, or for a release the mirror does not have,
// the support.js on disk is served as before; boards themselves are never changed.

const (
	// ReleasesFile is the mirror's list of releases at the top of the releases folder.
	ReleasesFile  = "releases.json"
	ReleaseStable = "stable"
	ReleaseBeta   = "beta"
	// EngineFile is the name boards load the engine by.
	EngineFile = "support.js"
	// releaseKind is the mirror's type the library's boards are made in.
	releaseKind = "design"
	// librarySettingsFile keeps the library's own choices, such as the release new projects start on.
	librarySettingsFile = ".design-releases.json"
)

var releaseIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// IsReleaseID: a release is named by its folder in the mirror.
func IsReleaseID(v string) bool { return releaseIDRe.MatchString(v) && v != "." && v != ".." }

// Release is one release a project can be on.
type Release struct {
	ID     string `json:"release"`
	Date   string `json:"date"`
	Status string `json:"status"`
	// engine is the verified engine file; skill the folder of the agents' rules for it ("" when there is none).
	engine string
	skill  string
}

// ProjectRelease is the release a project is on, as the panel shows it.
type ProjectRelease struct {
	// Current is the project's release; a project that names none is on the engine it carries, shown as legacy.
	Current string `json:"current"`
	// Pinned: the project names its release (else its own support.js is served).
	Pinned bool `json:"pinned"`
	// Upgrade is the newest stable release after Current, null when there is none.
	Upgrade *string `json:"upgrade"`
	// Skill is the folder of the agents' rules for Current, null when the mirror has none.
	Skill *string `json:"skill"`
}

type releasesState struct {
	mu      sync.Mutex
	key     string
	list    []Release
	digests map[string]string
}

// ReleasesDir is the mirror's folder, "" when releases are not used.
func (l *Library) ReleasesDir() string {
	if l.cfg.ReleasesDir == "" {
		return ""
	}
	abs, err := filepath.Abs(l.cfg.ReleasesDir)
	if err != nil {
		return filepath.Clean(l.cfg.ReleasesDir)
	}
	return abs
}

// Releases are the usable releases, oldest first; nil when releases are not used or the list cannot be read. A
// release whose engine is missing, leaves the folder or does not match its checksum is left out.
func (l *Library) Releases() []Release {
	dir := l.ReleasesDir()
	if dir == "" {
		return nil
	}
	file := filepath.Join(dir, ReleasesFile)
	st, err := os.Stat(file)
	if err != nil || !st.Mode().IsRegular() {
		return nil
	}
	key := file + "\n" + strconv.FormatInt(st.ModTime().UnixNano(), 10) + "\n" + strconv.FormatInt(st.Size(), 10)
	s := &l.releases
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.key == key {
		return append([]Release{}, s.list...)
	}
	list, err := l.readReleases(dir, file)
	if err != nil {
		l.warn("design releases: "+file+" cannot be read", err)
		return nil
	}
	s.key, s.list = key, list
	return append([]Release{}, list...)
}

func (l *Library) readReleases(dir, file string) ([]Release, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	raw, err := jsvalue.Parse([]byte(jscompat.DecodeUTF8(b)))
	if err != nil {
		return nil, err
	}
	root, ok := raw.(*jsvalue.Object)
	if !ok {
		return nil, errors.New("not an object")
	}
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, err
	}
	types := root.Object("types")
	if types == nil {
		return []Release{}, nil
	}
	kind := types.Object(releaseKind)
	if kind == nil {
		return []Release{}, nil
	}
	out := []Release{}
	seen := map[string]bool{}
	for _, item := range kind.Array("releases") {
		e, ok := item.(*jsvalue.Object)
		if !ok {
			continue
		}
		r := Release{ID: e.String("release"), Date: e.String("date"), Status: e.String("status")}
		if !IsReleaseID(r.ID) || seen[r.ID] || (r.Status != ReleaseStable && r.Status != ReleaseBeta) {
			continue
		}
		engine := confined(realDir, e.String("engine"))
		if engine == "" || !l.engineMatches(engine, e.String("engineSha256")) {
			l.warn("design releases: the engine of release "+r.ID+" is missing or does not match its checksum", nil)
			continue
		}
		r.engine = engine
		if skill := confined(realDir, e.String("skill")); skill != "" {
			r.skill = skill
		}
		seen[r.ID] = true
		out = append(out, r)
	}
	return out, nil
}

// confined is a path of the releases folder as a real path inside it, "" when it is empty, absolute or leads out.
func confined(realDir, rel string) string {
	if rel == "" || filepath.IsAbs(rel) {
		return ""
	}
	real, err := filepath.EvalSymlinks(filepath.Join(realDir, filepath.FromSlash(rel)))
	if err != nil || !isInside(real, realDir) || real == realDir {
		return ""
	}
	return real
}

// engineMatches checks an engine against its sha256 once per version of the file; a release without a checksum is
// not served.
func (l *Library) engineMatches(file, want string) bool {
	if len(want) != 64 {
		return false
	}
	st, err := os.Stat(file)
	if err != nil || !st.Mode().IsRegular() {
		return false
	}
	key := file + "\n" + strconv.FormatInt(st.ModTime().UnixNano(), 10) + "\n" + strconv.FormatInt(st.Size(), 10)
	if l.releases.digests == nil {
		l.releases.digests = map[string]string{}
	}
	got, ok := l.releases.digests[key]
	if !ok {
		f, err := os.Open(file)
		if err != nil {
			return false
		}
		h := sha256.New()
		_, err = io.Copy(h, f)
		f.Close()
		if err != nil {
			return false
		}
		got = hex.EncodeToString(h.Sum(nil))
		l.releases.digests[key] = got
	}
	return got == want
}

func findRelease(list []Release, id string) (int, bool) {
	for i, r := range list {
		if r.ID == id {
			return i, true
		}
	}
	return -1, false
}

// NewestStable is the newest stable release, "" when there is none.
func NewestStable(list []Release) string {
	for i := len(list) - 1; i >= 0; i-- {
		if list[i].Status == ReleaseStable {
			return list[i].ID
		}
	}
	return ""
}

// upgradeFrom is the newest stable release after current; for a release the list does not know, the newest stable.
func upgradeFrom(list []Release, current string) string {
	newest := NewestStable(list)
	if newest == "" || newest == current {
		return ""
	}
	at, known := findRelease(list, current)
	if !known {
		return newest
	}
	if n, _ := findRelease(list, newest); n > at {
		return newest
	}
	return ""
}

// PinnedRelease is the release a project with meta/ is on (legacy when its project.json names none); "" for a project
// without meta/, which is served its own engine.
func (l *Library) PinnedRelease(id string) string {
	if !l.HasMeta(id) {
		return ""
	}
	o, ok := readSmallJSON(filepath.Join(l.MetaDir(id), MetaProjectFile))
	if !ok {
		return ""
	}
	if r := parseProjectMeta(o, id).Release; IsReleaseID(r) {
		return r
	}
	return ""
}

// EngineFor is the engine file served as the project's support.js, "" to serve the one on disk: releases are not
// used, the project names no release, or the mirror does not have the one it names.
func (l *Library) EngineFor(id string) string {
	pinned := l.PinnedRelease(id)
	if pinned == "" {
		return ""
	}
	list := l.Releases()
	if list == nil {
		return ""
	}
	i, ok := findRelease(list, pinned)
	if !ok {
		l.warn("design releases: project "+id+" is on release "+pinned+", which the mirror does not have; its own "+EngineFile+" is served", nil)
		return ""
	}
	return list[i].engine
}

// ProjectReleaseOf is the project's release as the panel shows it; nil when releases are not used.
func (l *Library) ProjectReleaseOf(id string, list []Release) *ProjectRelease {
	if list == nil {
		return nil
	}
	pinned := l.PinnedRelease(id)
	p := &ProjectRelease{Current: pinned, Pinned: pinned != ""}
	if !p.Pinned {
		p.Current = LegacyRelease
	}
	if up := upgradeFrom(list, p.Current); up != "" {
		p.Upgrade = &up
	}
	if i, ok := findRelease(list, p.Current); ok && list[i].skill != "" {
		s := list[i].skill
		p.Skill = &s
	}
	return p
}

// AttachReleases puts each project's release on the listing when releases are used.
func (l *Library) AttachReleases(artifacts []*Artifact) {
	list := l.Releases()
	if list == nil {
		return
	}
	for _, a := range artifacts {
		a.Release = l.ProjectReleaseOf(a.ID, list)
	}
}

// ReleaseError is a refusal worded for the person.
type ReleaseError struct{ Msg string }

func (e *ReleaseError) Error() string { return e.Msg }

func releaseErr(en, ru string) error { return &ReleaseError{Msg: locale.Pick(en, ru)} }

// IsReleaseError: the refusal is the person's to read.
func IsReleaseError(err error) bool {
	var re *ReleaseError
	return errors.As(err, &re)
}

// stableRelease is the named release when it is stable, else a refusal.
func (l *Library) stableRelease(release string) (Release, error) {
	list := l.Releases()
	if list == nil {
		return Release{}, releaseErr("Releases are not set up on this machine", "На этой машине не настроены выпуски")
	}
	i, ok := findRelease(list, release)
	if !IsReleaseID(release) || !ok {
		return Release{}, releaseErr("There is no release \""+release+"\"", "Нет выпуска «"+release+"»")
	}
	if list[i].Status != ReleaseStable {
		return Release{}, releaseErr("Release \""+release+"\" has not passed the check yet", "Выпуск «"+release+"» ещё не прошёл проверку")
	}
	return list[i], nil
}

// SetProjectRelease moves a project to a stable release: the release of its meta/project.json, through the page
// model (which rebuilds the files made from meta/). The boards are not touched; the next load of a board gets the
// release's engine. A project without meta/ is refused until the library is migrated.
func (l *Library) SetProjectRelease(id, release string) (*ProjectRelease, error) {
	if !IsLibraryID(id) || l.ReadArtifact(id) == nil {
		return nil, releaseErr("There is no such project", "Такого проекта нет")
	}
	if _, err := l.stableRelease(release); err != nil {
		return nil, err
	}
	if !l.HasMeta(id) {
		return nil, &ReleaseError{Msg: l.noMetaError(id).Error()}
	}
	if err := l.SetRelease(id, release); err != nil {
		if IsPageError(err) {
			return nil, &ReleaseError{Msg: err.Error()}
		}
		return nil, err
	}
	l.listing.invalidateAll()
	return l.ProjectReleaseOf(id, l.Releases()), nil
}

// writeFileAtomic writes a fresh file beside the target and renames it over, so a link at that name is replaced,
// never written through.
func writeFileAtomic(file string, data []byte) error {
	temp := filepath.Join(filepath.Dir(file), "."+filepath.Base(file)+"."+randomHex(8)+".tmp")
	f, err := os.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o664)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(temp, file)
	}
	if err != nil {
		_ = os.Remove(temp)
	}
	return err
}

func (l *Library) librarySettings() *jsvalue.Object {
	file := filepath.Join(l.Root(), librarySettingsFile)
	st, err := os.Lstat(file)
	if err != nil || !st.Mode().IsRegular() || st.Size() > 1<<16 {
		return nil
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return nil
	}
	raw, err := jsvalue.Parse([]byte(jscompat.DecodeUTF8(b)))
	if err != nil {
		return nil
	}
	o, _ := raw.(*jsvalue.Object)
	return o
}

// NewProjectsRelease is the release a new project starts on: the library's choice while it is stable, else the
// newest stable release; "" when releases are not used.
func (l *Library) NewProjectsRelease() string {
	list := l.Releases()
	if list == nil {
		return ""
	}
	if s := l.librarySettings(); s != nil {
		if chosen := s.String("newProjects"); chosen != "" {
			if i, ok := findRelease(list, chosen); ok && list[i].Status == ReleaseStable {
				return chosen
			}
		}
	}
	return NewestStable(list)
}

// ChosenNewProjectsRelease is the library's own choice, "" when new projects follow the newest stable release.
func (l *Library) ChosenNewProjectsRelease() string {
	if s := l.librarySettings(); s != nil {
		if chosen := s.String("newProjects"); IsReleaseID(chosen) {
			return chosen
		}
	}
	return ""
}

// SetNewProjectsRelease pins new projects to a stable release; "" lets them follow the newest stable one.
func (l *Library) SetNewProjectsRelease(release string) error {
	if release != "" {
		if _, err := l.stableRelease(release); err != nil {
			return err
		}
	}
	m := l.lockOf("library-settings")
	m.Lock()
	defer m.Unlock()
	s := l.librarySettings()
	if s == nil {
		s = jsvalue.NewObject("v", 1)
	}
	if release == "" {
		s.Delete("newProjects")
	} else {
		s.Set("newProjects", release)
	}
	if err := os.MkdirAll(l.Root(), 0o777); err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(l.Root(), librarySettingsFile), append(jsvalue.Stringify(s, "  "), '\n'))
}
