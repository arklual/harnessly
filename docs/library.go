package docs

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// The documentation is a library of sections. A section is a folder with a section.json beside its pages:
//
//	{"title": {"en": "Getting started", "ru": "Начало работы"}, "audience": "people", "order": 10,
//	 "pages": ["README.md", "first-chat.md", "../voice.md"]}
//
// "pages" is optional: without it the section is every *.md of its folder, README first, then by file name (a number in
// front of a name orders it and is dropped from the page's name). A page listed with ../ is a file of the same source
// outside the folder, so one Markdown file can be a page of the repository's docs and of a section at once.
//
// A translation is the same path under ru/ (ru/start/README.md); a page without one is read in English.
//
// The library is the built-in sections (this package, embedded) and then the sections of the machine's own folders
// (LocalDirs): a team's or a self-hosted install's documentation, read from disk on every ask, so a page written there
// shows at once without a build. A local section with the id of a built-in one adds its pages to it, and a page with
// the same name replaces the built-in page. Markdown files lying in the root of a local folder are the section "team".

// Audiences of a section: who it is written for. The page shows them all; an agent's list marks them.
const (
	AudiencePeople = "people"
	AudienceAgents = "agents"
	AudienceAll    = "all"
)

// Origins of a section or a page.
const (
	OriginBuiltin = "builtin"
	OriginLocal   = "local"
)

// TeamSection is the id of the section made of the Markdown files in the root of a local folder.
const TeamSection = "team"

// ManifestFile is the file that makes a folder a section.
const ManifestFile = "section.json"

// Source is one place the documentation is read from: the embedded docs or a folder on the machine.
type Source struct {
	Origin string
	FS     fs.FS
	// Dir is the folder on disk of a local source (empty for the embedded one).
	Dir string
}

// Section is a section of the library in one locale.
type Section struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Audience string `json:"audience"`
	Order    int    `json:"order"`
	Origin   string `json:"origin"`
	Pages    []Doc  `json:"pages"`

	// titled: the title is the manifest's, not made up from the folder or the first page.
	titled bool
}

// Doc is a page of a section: its name in the section, the file it was read from and its outline.
type Doc struct {
	Name     string   `json:"name"`
	Title    string   `json:"title"`
	Headings []string `json:"headings"`
	// File is the page's path in its source, the English one: links and pictures of the page are relative to it.
	File string `json:"file"`
	// Read is the file actually read: the translation (ru/...) when the locale has one, else File.
	Read   string `json:"read"`
	Origin string `json:"origin"`

	source *Source
}

type manifest struct {
	Title    map[string]string `json:"title"`
	Audience string            `json:"audience"`
	Order    *int              `json:"order"`
	Pages    []string          `json:"pages"`
}

// Library is the sections in reading order.
type Library struct {
	Sections []Section
}

// Builtin is the embedded documentation as a source.
func Builtin() Source { return Source{Origin: OriginBuiltin, FS: Guide} }

// LocalDirs are the machine's own documentation folders: HARNESSLY_DOCS_DIR (several separated like PATH), else
// <data>/docs. The first one is where `harnessly docs add` writes.
func LocalDirs(getenv func(string) string, dataDir string) []string {
	if v := strings.TrimSpace(getenv("HARNESSLY_DOCS_DIR")); v != "" {
		var dirs []string
		for _, d := range filepath.SplitList(v) {
			if d = strings.TrimSpace(d); d != "" {
				dirs = append(dirs, d)
			}
		}
		return dirs
	}
	if dataDir == "" {
		return nil
	}
	return []string{filepath.Join(dataDir, "docs")}
}

// Sources are the built-in source and the local folders that exist.
func Sources(localDirs []string) []Source {
	out := []Source{Builtin()}
	for _, d := range localDirs {
		if st, err := os.Stat(d); err == nil && st.IsDir() {
			out = append(out, Source{Origin: OriginLocal, FS: os.DirFS(d), Dir: d})
		}
	}
	return out
}

// Load reads the library in the locale. A broken local section (a bad section.json) is left out, not the library: the
// person's folder must never take the built-in documentation down with it.
func Load(sources []Source, locale string) (Library, error) {
	var sections []Section
	index := map[string]int{}
	for i := range sources {
		src := &sources[i]
		found, err := sectionsOf(src, locale)
		if err != nil {
			if src.Origin == OriginBuiltin {
				return Library{}, err
			}
			continue
		}
		for _, s := range found {
			if at, ok := index[s.ID]; ok {
				sections[at] = merge(sections[at], s)
				continue
			}
			index[s.ID] = len(sections)
			sections = append(sections, s)
		}
	}
	sort.SliceStable(sections, func(i, j int) bool { return sections[i].Order < sections[j].Order })
	return Library{Sections: sections}, nil
}

// merge adds a local section's pages to a section of the same id: a page of the same name replaces the earlier one.
func merge(base, more Section) Section {
	if more.titled {
		base.Title = more.Title
	}
	for _, p := range more.Pages {
		replaced := false
		for i := range base.Pages {
			if base.Pages[i].Name == p.Name {
				base.Pages[i] = p
				replaced = true
				break
			}
		}
		if !replaced {
			base.Pages = append(base.Pages, p)
		}
	}
	return base
}

func sectionsOf(src *Source, locale string) ([]Section, error) {
	manifests, err := fs.Glob(src.FS, "*/"+ManifestFile)
	if err != nil {
		return nil, err
	}
	var out []Section
	seen := map[string]bool{}
	for _, file := range manifests {
		dir := path.Dir(file)
		if dir == LocaleRU {
			continue
		}
		s, err := readSection(src, dir, locale)
		if err != nil {
			if src.Origin == OriginBuiltin {
				return nil, err
			}
			continue
		}
		seen[dir] = true
		out = append(out, s)
	}
	if src.Origin != OriginLocal {
		return out, nil
	}
	// A local folder without a section.json is a section too, named after the folder; loose files are "team".
	entries, err := fs.ReadDir(src.FS, ".")
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() || seen[e.Name()] || e.Name() == LocaleRU || strings.HasPrefix(e.Name(), ".") || !ValidID(e.Name()) {
			continue
		}
		if md, _ := fs.Glob(src.FS, e.Name()+"/*.md"); len(md) == 0 {
			continue
		}
		if s, err := readSection(src, e.Name(), locale); err == nil {
			out = append(out, s)
		}
	}
	if s, err := readSection(src, ".", locale); err == nil && len(s.Pages) > 0 {
		out = append(out, s)
	}
	return out, nil
}

// readSection reads the folder dir of the source as a section ("." is the root of a local folder: the team section).
func readSection(src *Source, dir, locale string) (Section, error) {
	id := dir
	if dir == "." {
		id = TeamSection
	}
	m := manifest{}
	raw, err := fs.ReadFile(src.FS, path.Join(dir, ManifestFile))
	switch {
	case err == nil:
		if err := json.Unmarshal(raw, &m); err != nil {
			return Section{}, errors.New(path.Join(dir, ManifestFile) + ": " + err.Error())
		}
	case !errors.Is(err, fs.ErrNotExist):
		return Section{}, err
	}
	s := Section{ID: id, Audience: m.Audience, Origin: src.Origin, Order: 1000}
	switch s.Audience {
	case AudiencePeople, AudienceAgents, AudienceAll:
	default:
		s.Audience = AudienceAll
	}
	if m.Order != nil {
		s.Order = *m.Order
	}
	s.Title = m.Title[locale]
	if s.Title == "" {
		s.Title = m.Title[LocaleEN]
	}
	s.titled = s.Title != ""
	files, err := pageFiles(src.FS, dir, m.Pages)
	if err != nil {
		return Section{}, err
	}
	names := map[string]bool{}
	for _, file := range files {
		text, read, err := readLocalized(src.FS, locale, file)
		if err != nil {
			return Section{}, err
		}
		title, headings := outline(string(text))
		name := docName(file)
		if name == "" {
			name = "page-" + strconv.Itoa(len(s.Pages)+1)
		}
		if names[name] {
			// Two files of one name (architecture.md and hub/architecture.md): the folder tells them apart.
			name = path.Base(path.Dir(file)) + "-" + name
		}
		names[name] = true
		if title == "" {
			title = name
		}
		if headings == nil {
			headings = []string{}
		}
		s.Pages = append(s.Pages, Doc{Name: name, Title: title, Headings: headings, File: file, Origin: src.Origin, source: src, Read: read})
	}
	if s.Title == "" && len(s.Pages) > 0 && dir != "." {
		s.Title = s.Pages[0].Title
	}
	if s.Title == "" {
		s.Title = id
	}
	return s, nil
}

// pageFiles are the files of a section: the listed ones (each kept inside the source), else every *.md of the folder.
func pageFiles(fsys fs.FS, dir string, listed []string) ([]string, error) {
	if len(listed) > 0 {
		out := make([]string, 0, len(listed))
		for _, p := range listed {
			file := path.Clean(path.Join(dir, p))
			if !fs.ValidPath(file) || !strings.HasSuffix(file, ".md") || strings.HasPrefix(file, LocaleRU+"/") {
				return nil, errors.New(path.Join(dir, ManifestFile) + ": a page outside the docs: " + p)
			}
			out = append(out, file)
		}
		return out, nil
	}
	files, err := fs.Glob(fsys, path.Join(dir, "*.md"))
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool {
		a, b := path.Base(files[i]), path.Base(files[j])
		if (a == "README.md") != (b == "README.md") {
			return a == "README.md"
		}
		return a < b
	})
	return files, nil
}

// docName is a page's name from its file: the folder, a reading-order number and the extension dropped; a README is
// the section's "overview".
func docName(file string) string {
	name := strings.TrimSuffix(path.Base(file), ".md")
	if name == "README" {
		return "overview"
	}
	if num, rest, ok := strings.Cut(name, "-"); ok && num != "" && strings.Trim(num, "0123456789") == "" {
		name = rest
	}
	return Slug(name)
}

// Slug makes an address name of a title or a file name: lower-case latin letters and digits, the rest a single dash.
// It is empty for a name with no latin letter or digit.
func Slug(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			dash = false
			continue
		}
		dash = true
	}
	out := b.String()
	if len(out) > 63 {
		out = strings.TrimRight(out[:63], "-")
	}
	return out
}

// ValidID is a name of a section or a page as it may stand in an address: lower-case letters, digits and dashes.
func ValidID(s string) bool {
	if s == "" || len(s) > 63 || s[0] == '-' {
		return false
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
}

// Section is the section of the id.
func (l Library) Section(id string) (Section, bool) {
	for _, s := range l.Sections {
		if s.ID == id {
			return s, true
		}
	}
	return Section{}, false
}

// Find is the page of a section; the names are matched against the library, so nothing a caller sends reaches a path.
func (l Library) Find(section, name string) (Doc, bool) {
	s, ok := l.Section(section)
	if !ok {
		return Doc{}, false
	}
	for _, p := range s.Pages {
		if p.Name == name {
			return p, true
		}
	}
	return Doc{}, false
}

// Markdown is the page's text in the locale it was loaded in.
func (d Doc) Markdown() ([]byte, error) {
	if d.source == nil {
		return nil, fs.ErrNotExist
	}
	return fs.ReadFile(d.source.FS, d.Read)
}

// Path is where the page lies on disk, for a local page; empty for a built-in one.
func (d Doc) Path() string {
	if d.source == nil || d.source.Dir == "" {
		return ""
	}
	return filepath.Join(d.source.Dir, filepath.FromSlash(d.Read))
}

// Picture kinds a page may show, by extension.
var pictureTypes = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".svg":  "image/svg+xml",
}

// MaxPicture is the largest picture a page may show.
const MaxPicture = 8 << 20

// Picture reads a picture a page points at (src relative to the page's file, as Markdown writes it) from the page's
// own source; it never leaves the source and only reads pictures. The type is its media type.
func (d Doc) Picture(src string) ([]byte, string, error) {
	if d.source == nil || src == "" || strings.Contains(src, "://") || strings.HasPrefix(src, "/") || strings.Contains(src, "\\") {
		return nil, "", fs.ErrNotExist
	}
	src, _, _ = strings.Cut(src, "#")
	src, _, _ = strings.Cut(src, "?")
	kind, ok := pictureTypes[strings.ToLower(path.Ext(src))]
	if !ok {
		return nil, "", fs.ErrNotExist
	}
	// A translation may have pictures of its own beside it; else it shows the English page's.
	bases := []string{path.Dir(d.Read)}
	if d.Read != d.File {
		bases = append(bases, path.Dir(d.File))
	}
	for _, base := range bases {
		file := path.Clean(path.Join(base, src))
		if !fs.ValidPath(file) {
			continue
		}
		st, err := fs.Stat(d.source.FS, file)
		if err != nil || st.IsDir() || st.Size() > MaxPicture {
			continue
		}
		raw, err := fs.ReadFile(d.source.FS, file)
		return raw, kind, err
	}
	return nil, "", fs.ErrNotExist
}

// Hit is a page that matched a search, with the line that shows why.
type Hit struct {
	Section string `json:"section"`
	Name    string `json:"name"`
	Title   string `json:"title"`
	// Heading is the heading the match is under, if any (its anchor is the page's to make).
	Heading string `json:"heading"`
	Snippet string `json:"snippet"`
	score   int
}

// Search finds the pages that have every word of the query, best first: a word in the title counts most, then in a
// heading, then in the text.
func (l Library) Search(query string, limit int) []Hit {
	words := strings.Fields(strings.ToLower(query))
	if len(words) == 0 {
		return nil
	}
	var hits []Hit
	for _, s := range l.Sections {
		for _, p := range s.Pages {
			raw, err := p.Markdown()
			if err != nil {
				continue
			}
			text := strings.ToLower(string(raw))
			title := strings.ToLower(p.Title)
			score := 0
			all := true
			for _, w := range words {
				n := strings.Count(text, w)
				if n == 0 && !strings.Contains(title, w) {
					all = false
					break
				}
				score += min(n, 10)
				if strings.Contains(title, w) {
					score += 20
				}
				for _, h := range p.Headings {
					if strings.Contains(strings.ToLower(h), w) {
						score += 6
					}
				}
			}
			if !all {
				continue
			}
			heading, snippet := snippetOf(string(raw), words)
			hits = append(hits, Hit{Section: s.ID, Name: p.Name, Title: p.Title, Heading: heading, Snippet: snippet, score: score})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}

// snippetOf is the first line of text with a word of the query, shortened around it, and the heading it is under.
func snippetOf(text string, words []string) (string, string) {
	heading := ""
	fenced := false
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			fenced = !fenced
			continue
		}
		if !fenced {
			if h, ok := strings.CutPrefix(trimmed, "## "); ok {
				heading = strings.TrimSpace(h)
				continue
			}
			if h, ok := strings.CutPrefix(trimmed, "### "); ok {
				heading = strings.TrimSpace(h)
				continue
			}
			if strings.HasPrefix(trimmed, "#") {
				continue
			}
		}
		lower := strings.ToLower(trimmed)
		for _, w := range words {
			if at := strings.Index(lower, w); at >= 0 {
				return heading, around([]rune(trimmed), len([]rune(lower[:at])), 160)
			}
		}
	}
	return "", ""
}

func around(line []rune, at, width int) string {
	if len(line) <= width {
		return string(line)
	}
	start := max(at-width/3, 0)
	end := min(start+width, len(line))
	out := string(line[start:end])
	if start > 0 {
		out = "…" + out
	}
	if end < len(line) {
		out += "…"
	}
	return out
}
