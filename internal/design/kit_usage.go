package design

// Where a kit is used and which kit a project is built on («Связь проектов с китом»). A project is on a kit's
// theme when it keeps the theme's folder (ds/<theme>/, what a layout put there) or loads the kit by address
// (/kit/<id>/<version>/). Its boards name the kit's nodes: a layout board says data-layout="<id>", a component is an
// <x-import component-from-global-scope="<Namespace>.<Component>"> with the props that pick a layout or an asset.

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/ly-solutions/harnessly/internal/designkit"
)

var (
	xImportRe    = regexp.MustCompile(`<x-import\b[^>]*>`)
	attrRe       = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9-]*)="([^"]*)"`)
	dataLayoutRe = regexp.MustCompile(`data-layout="([a-z0-9][a-z0-9-]{0,62})"`)
	kitAddrRe    = regexp.MustCompile(`/kit/([a-z0-9][a-z0-9-]{0,62})/(\d{1,9}\.\d{1,9}\.\d{1,9}[0-9A-Za-z.-]*)/`)
)

const maxUsageBoard = 4 << 20

// KitBoardUse is a board of a project and the kit's nodes it uses.
type KitBoardUse struct {
	Path  string   `json:"path"`
	Title string   `json:"title"`
	Nodes []string `json:"nodes"`
}

// KitLink is the kit a project is built on: its theme and version ("" when the project's copy matches no installed
// version), the newest installed version, and the boards with the kit's nodes they use.
type KitLink struct {
	Project string        `json:"project"`
	Title   string        `json:"title"`
	Kit     string        `json:"kit"`
	KitName string        `json:"kitTitle"`
	Theme   string        `json:"theme"`
	Version string        `json:"version"`
	Latest  string        `json:"latest"`
	Boards  []KitBoardUse `json:"boards"`
	// Nodes names every node the boards use, with its picture (a path in the version) where it has one.
	Nodes map[string]KitNodeInfo `json:"nodes"`
}

// KitNodeInfo is what a project's bar says of a kit node: its name and its picture.
type KitNodeInfo struct {
	Name string `json:"name"`
	Shot string `json:"shot,omitempty"`
}

// nodeFinder maps what a board says to the nodes of one canvas.
type nodeFinder struct {
	ns        string
	layouts   map[string]string // layout id → node
	comps     map[string]string // component → node
	byProps   map[string][]propNode
	assetsBy  map[string]assetKey // component → the prop that names an asset and the group
	assetNode map[string]string   // group/name → node
	info      map[string]KitNodeInfo
}

type propNode struct {
	props map[string]string
	node  string
}

type assetKey struct{ prop, group string }

func newNodeFinder(c *designkit.Canvas) *nodeFinder {
	f := &nodeFinder{ns: c.Namespace, layouts: map[string]string{}, comps: map[string]string{}, byProps: map[string][]propNode{},
		assetsBy: map[string]assetKey{}, assetNode: map[string]string{}, info: map[string]KitNodeInfo{}}
	for _, g := range c.Groups {
		for _, k := range g.Components {
			f.comps[k.ID] = k.NodeID
			f.info[k.NodeID] = KitNodeInfo{Name: k.Name}
		}
	}
	for _, t := range c.Templates {
		for _, s := range t.Sections {
			for _, l := range s.Layouts {
				f.layouts[l.ID] = l.NodeID
				f.info[l.NodeID] = KitNodeInfo{Name: l.Name, Shot: shotOf(c.Base, l.Thumb)}
				if t.Component == "" || len(l.Props) == 0 {
					continue
				}
				props := map[string]string{}
				for k, v := range l.Props {
					if sv, ok := v.(string); ok {
						props[k] = sv
					}
				}
				f.byProps[t.Component] = append(f.byProps[t.Component], propNode{props, l.NodeID})
			}
		}
	}
	for _, g := range c.Assets {
		if g.Component != "" && g.Prop != "" {
			f.assetsBy[g.Component] = assetKey{g.Prop, g.ID}
		}
		for _, a := range g.Items {
			f.assetNode[g.ID+"/"+a.Name] = a.NodeID
			f.info[a.NodeID] = KitNodeInfo{Name: a.Name, Shot: shotOf(c.Base, a.File)}
		}
	}
	return f
}

// nodes are the canvas's nodes a board's source names, in the order first named.
func (f *nodeFinder) nodes(src []byte) []string {
	seen := map[string]bool{}
	var out []string
	add := func(n string) {
		if n != "" && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	for _, m := range dataLayoutRe.FindAllSubmatch(src, -1) {
		add(f.layouts[string(m[1])])
	}
	prefix := f.ns + "."
	for _, tag := range xImportRe.FindAll(src, -1) {
		attrs := map[string]string{}
		for _, m := range attrRe.FindAllSubmatch(tag, -1) {
			attrs[string(m[1])] = string(m[2])
		}
		scope := attrs["component-from-global-scope"]
		if f.ns == "" || !strings.HasPrefix(scope, prefix) {
			continue
		}
		comp := strings.TrimPrefix(scope, prefix)
		for _, pn := range f.byProps[comp] {
			match := true
			for k, v := range pn.props {
				if attrs[k] != v {
					match = false
					break
				}
			}
			if match {
				add(pn.node)
				break
			}
		}
		if ak, ok := f.assetsBy[comp]; ok {
			add(f.assetNode[ak.group+"/"+attrs[ak.prop]])
		}
		add(f.comps[comp])
	}
	return out
}

// kitTheme is an installed theme of a kit the usage looks for.
type usageTheme struct {
	kit, kitTitle, theme, folder, latest string
	canvas                               *designkit.Canvas
	finder                               *nodeFinder
	// sums are the hashes of the theme's design-system.json by installed version: a project's copy tells its version.
	sums map[[32]byte]string
}

func (l *Library) usageThemes(only string) []usageTheme {
	dir := l.KitsDir()
	var out []usageTheme
	for _, k := range designkit.List(dir) {
		if (only != "" && k.ID != only) || len(k.Versions) == 0 {
			continue
		}
		latest := k.Versions[0].Version
		for _, th := range designkit.Themes(dir, k.ID, latest) {
			c, err := designkit.ReadCanvas(dir, k.ID, latest, th.ID)
			if err != nil {
				continue
			}
			u := usageTheme{kit: k.ID, kitTitle: c.Title, theme: th.ID, folder: filepath.Base(th.Folder), latest: latest, canvas: c,
				finder: newNodeFinder(c), sums: map[[32]byte]string{}}
			for _, v := range k.Versions {
				segs := append(strings.Split(th.Folder, "/"), designkit.ManifestFile)
				if th.Folder == "" {
					segs = []string{designkit.ManifestFile}
				}
				if p := designkit.Resolve(dir, k.ID, v.Version, segs); p != "" {
					if raw, err := os.ReadFile(p); err == nil {
						u.sums[sha256.Sum256(raw)] = v.Version
					}
				}
			}
			out = append(out, u)
		}
	}
	return out
}

// themeOfProject is the installed theme a project is built on and its version, or nil.
func (l *Library) themeOfProject(id string, themes []usageTheme, boards [][]byte) (*usageTheme, string) {
	// A project pinned to a kit's version is on the pinned theme and version; its ds/<theme>/ comes from the
	// kit and may not be in its folder at all.
	if pin, ok := l.KitPinOf(id); ok {
		for i := range themes {
			t := &themes[i]
			if t.kit == pin.ID && t.folder != "" && t.folder != "." && pin.mounts(t.folder) {
				return t, pin.Version
			}
		}
	}
	dir := l.ProjectDir(id)
	for i := range themes {
		t := &themes[i]
		if t.folder == "" || t.folder == "." {
			continue
		}
		raw, err := readFileLimited(filepath.Join(dir, "ds", t.folder, designkit.ManifestFile), 32<<20)
		if err != nil {
			continue
		}
		var m struct {
			Namespace string `json:"namespace"`
		}
		if json.Unmarshal(raw, &m) != nil || m.Namespace != t.canvas.Namespace {
			continue
		}
		return t, t.sums[sha256.Sum256(raw)]
	}
	// A project that loads the kit by address (/kit/<id>/<version>/…) names the version itself.
	for _, src := range boards {
		for _, m := range kitAddrRe.FindAllSubmatch(src, -1) {
			for i := range themes {
				t := &themes[i]
				if t.kit == string(m[1]) && (t.folder == "" || bytes.Contains(src, []byte("/"+t.folder+"/")) || len(themes) == 1) {
					return t, string(m[2])
				}
			}
		}
	}
	return nil, ""
}

func (l *Library) readBoards(a *Artifact) [][]byte {
	out := make([][]byte, len(a.Boards))
	for i, b := range a.Boards {
		if p, err := l.boardFile(a.ID, b.Path); err == nil {
			if src, err := readFileLimited(p, maxUsageBoard); err == nil {
				out[i] = src
			}
		}
	}
	return out
}

func (l *Library) linkOf(a *Artifact, themes []usageTheme) *KitLink {
	boards := l.readBoards(a)
	t, version := l.themeOfProject(a.ID, themes, boards)
	if t == nil {
		return nil
	}
	link := &KitLink{Project: a.ID, Title: a.Title, Kit: t.kit, KitName: t.kitTitle, Theme: t.theme, Version: version, Latest: t.latest,
		Boards: []KitBoardUse{}, Nodes: map[string]KitNodeInfo{}}
	for i, b := range a.Boards {
		if nodes := t.finder.nodes(boards[i]); len(nodes) > 0 {
			link.Boards = append(link.Boards, KitBoardUse{Path: b.Path, Title: b.Title, Nodes: nodes})
			for _, n := range nodes {
				link.Nodes[n] = t.finder.info[n]
			}
		}
	}
	return link
}

// KitLinkOf is the kit a project is built on, or nil.
func (l *Library) KitLinkOf(id string) *KitLink {
	a := l.ReadArtifact(id)
	if a == nil {
		return nil
	}
	return l.linkOf(a, l.usageThemes(""))
}

// KitUses are the projects (that person may see) built on a kit, with the boards that use its nodes.
func (l *Library) KitUses(kit, person string) []KitLink {
	themes := l.usageThemes(kit)
	out := []KitLink{}
	if len(themes) == 0 {
		return out
	}
	for _, a := range l.VisibleToFor(l.ListArtifacts(), person) {
		if link := l.linkOf(a, themes); link != nil {
			out = append(out, *link)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Title < out[j].Title })
	return out
}

// KitUsage is GET ?kit= (a session): {projects: [KitLink]}, the projects on the kit.
func (a *API) KitUsage(r *http.Request) Answer {
	if no, ok := a.kitGet(r); !ok {
		return no
	}
	return JSON(r, http.StatusOK, struct {
		Projects []KitLink `json:"projects"`
	}{a.L.KitUses(QueryString(r, "kit"), a.PersonOf(r))}, noStore())
}

// KitLinkRoute is GET ?artifact= (a session, view): {link: KitLink | null}, the kit the project is built on.
func (a *API) KitLinkRoute(r *http.Request) Answer {
	if no, ok := a.kitGet(r); !ok {
		return no
	}
	id := QueryString(r, "artifact")
	if !IsLibraryID(id) || !a.L.CanFor(id, RoleView, a.PersonOf(r)) {
		return JSON(r, http.StatusNotFound, errorBody("Not found"), noStore())
	}
	return JSON(r, http.StatusOK, struct {
		Link *KitLink `json:"link"`
	}{a.L.KitLinkOf(id)}, noStore())
}

// ---- the projects made from templates (KitProject), so «Открыть как проект» opens the same one again

var kitProjectsMu sync.Mutex

func (l *Library) kitProjectsFile() string { return filepath.Join(l.Root(), ".kit-projects.json") }

func (l *Library) readKitProjects() map[string]string {
	m := map[string]string{}
	if raw, err := readFileLimited(l.kitProjectsFile(), 1<<20); err == nil {
		_ = json.Unmarshal(raw, &m)
	}
	return m
}

func (l *Library) kitProjectOf(key string) string {
	kitProjectsMu.Lock()
	defer kitProjectsMu.Unlock()
	return l.readKitProjects()[key]
}

func (l *Library) rememberKitProject(key, id string) {
	kitProjectsMu.Lock()
	defer kitProjectsMu.Unlock()
	m := l.readKitProjects()
	m[key] = id
	raw, _ := json.Marshal(m)
	_, _ = writeIfChanged(l.kitProjectsFile(), raw)
}

// shotOf is a picture of a theme's folder as a path in the version, "" when there is none.
func shotOf(base, rel string) string {
	if rel == "" {
		return ""
	}
	return joinBase(base, rel)
}
