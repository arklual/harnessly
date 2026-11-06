package hlcli

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ly-solutions/harnessly/docs"
)

const docsUsage = `harnessly docs - the documentation of harnessly and of this machine's team, from the agent's side

  harnessly docs [--lang ru]               the sections and their pages (people / agents / all), and where yours live
  harnessly docs search WORDS... [--lang ru]
                                           the pages that have every word, best first, with the line that matched
  harnessly docs read SECTION/PAGE [--lang ru]
                                           print a page (a page name alone is enough when only one section has it)
  harnessly docs add SECTION/PAGE [FILE|-] [--title T] [--section-title T] [--audience people|agents|all]
                     [--picture FILE]... [--replace]
                                           write a page into this machine's own docs (stdin when no FILE). It shows on
                                           the /help page at once. A section that does not exist yet is made; a picture
                                           is copied next to the page as img/<name>, to show with ![what](img/<name>).
                                           A page of the same name as a built-in one replaces it on this machine.
  harnessly docs path                      the folders of this machine's own docs (HARNESSLY_DOCS_DIR, else <data>/docs);
                                           add writes into the first

Read before guessing how harnessly or this team's setup works; when you found out something the docs lacked and the
person agrees, add it.
`

func (c *cli) docsSources() []docs.Source {
	return docs.Sources(c.docsDirs())
}

func (c *cli) docsDirs() []string { return docs.LocalDirs(c.env.Getenv, c.dataDir()) }

// docsArgs splits the arguments of a docs command: --lang and the add options, the rest positional.
type docsArgs struct {
	lang, title, sectionTitle, audience string
	pictures                            []string
	replace                             bool
	pos                                 []string
}

func parseDocsArgs(args []string) (docsArgs, error) {
	a := docsArgs{lang: docs.LocaleEN}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		name, val, hasVal := strings.Cut(arg, "=")
		valued := func() (string, error) {
			if hasVal {
				return val, nil
			}
			if i+1 >= len(args) {
				return "", usageErr("%s needs a value", arg)
			}
			i++
			return args[i], nil
		}
		var err error
		switch name {
		case "--lang":
			a.lang, err = valued()
		case "--title":
			a.title, err = valued()
		case "--section-title":
			a.sectionTitle, err = valued()
		case "--audience":
			a.audience, err = valued()
		case "--picture":
			var p string
			p, err = valued()
			a.pictures = append(a.pictures, p)
		case "--replace":
			a.replace = true
		case "--":
			a.pos = append(a.pos, args[i+1:]...)
			i = len(args)
		default:
			if strings.HasPrefix(arg, "--") {
				return a, usageErr("unknown option %s", arg)
			}
			a.pos = append(a.pos, arg)
		}
		if err != nil {
			return a, err
		}
	}
	if a.lang != docs.LocaleRU {
		a.lang = docs.LocaleEN
	}
	return a, nil
}

func (c *cli) docs(args []string) error {
	sub := "list"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	a, err := parseDocsArgs(args)
	if err != nil {
		return err
	}
	switch sub {
	case "list", "ls":
		return c.docsList(a)
	case "search", "find":
		return c.docsSearch(a)
	case "read", "show", "cat":
		return c.docsRead(a)
	case "add", "write":
		return c.docsAdd(a)
	case "path", "dir":
		for _, d := range c.docsDirs() {
			c.out("%s\n", d)
		}
		return nil
	case "help", "-h", "--help":
		c.out("%s", docsUsage)
		return nil
	}
	return usageErr("unknown docs command %q; see harnessly docs help", sub)
}

func (c *cli) docsLibrary(lang string) (docs.Library, error) {
	lib, err := docs.Load(c.docsSources(), lang)
	if err != nil {
		return lib, fail("the docs: %v", err)
	}
	return lib, nil
}

func (c *cli) docsList(a docsArgs) error {
	lib, err := c.docsLibrary(a.lang)
	if err != nil {
		return err
	}
	for _, s := range lib.Sections {
		mark := ""
		if s.Origin == docs.OriginLocal {
			mark = ", this machine's"
		}
		c.out("%s — %s (%s%s)\n", s.ID, s.Title, s.Audience, mark)
		for _, p := range s.Pages {
			c.out("  %-36s %s\n", s.ID+"/"+p.Name, p.Title)
		}
		c.out("\n")
	}
	dirs := c.docsDirs()
	if len(dirs) > 0 {
		c.out("This machine's own docs: %s (harnessly docs add writes there)\n", dirs[0])
	}
	c.out("harnessly docs search WORDS · harnessly docs read SECTION/PAGE · harnessly docs help\n")
	return nil
}

func (c *cli) docsSearch(a docsArgs) error {
	if len(a.pos) == 0 {
		return usageErr("docs search needs words: harnessly docs search hub deploy")
	}
	lib, err := c.docsLibrary(a.lang)
	if err != nil {
		return err
	}
	hits := lib.Search(strings.Join(a.pos, " "), 12)
	if len(hits) == 0 {
		c.out("Nothing has all of: %s. Try fewer or other words, or harnessly docs to see the sections.\n", strings.Join(a.pos, " "))
		return nil
	}
	for _, h := range hits {
		where := h.Title
		if h.Heading != "" {
			where += " › " + h.Heading
		}
		c.out("%s/%s\t%s\n", h.Section, h.Name, where)
		if h.Snippet != "" {
			c.out("    %s\n", h.Snippet)
		}
	}
	c.out("\nharnessly docs read SECTION/PAGE prints a page.\n")
	return nil
}

// docsFind is the page an argument names: SECTION/PAGE, or a page name that only one section has.
func docsFind(lib docs.Library, ref string) (docs.Doc, string, error) {
	if section, name, ok := strings.Cut(ref, "/"); ok {
		if d, found := lib.Find(section, name); found {
			return d, section, nil
		}
		return docs.Doc{}, "", fail("no page %s; harnessly docs lists them", ref)
	}
	var found []string
	var doc docs.Doc
	for _, s := range lib.Sections {
		if d, ok := lib.Find(s.ID, ref); ok {
			found = append(found, s.ID+"/"+ref)
			doc = d
		}
	}
	switch len(found) {
	case 0:
		return docs.Doc{}, "", fail("no page %s; harnessly docs lists them, harnessly docs search finds them", ref)
	case 1:
		section, _, _ := strings.Cut(found[0], "/")
		return doc, section, nil
	}
	return docs.Doc{}, "", usageErr("%s is in several sections: %s", ref, strings.Join(found, ", "))
}

func (c *cli) docsRead(a docsArgs) error {
	if len(a.pos) != 1 {
		return usageErr("docs read takes one page: harnessly docs read start/overview")
	}
	lib, err := c.docsLibrary(a.lang)
	if err != nil {
		return err
	}
	doc, _, err := docsFind(lib, a.pos[0])
	if err != nil {
		return err
	}
	raw, err := doc.Markdown()
	if err != nil {
		return fail("the page: %v", err)
	}
	c.out("%s", raw)
	if !strings.HasSuffix(string(raw), "\n") {
		c.out("\n")
	}
	return nil
}

func (c *cli) docsAdd(a docsArgs) error {
	if len(a.pos) < 1 || len(a.pos) > 2 {
		return usageErr("docs add takes SECTION/PAGE and a file (or stdin): harnessly docs add runbooks/deploy ./deploy.md")
	}
	section, name, ok := strings.Cut(a.pos[0], "/")
	if !ok || !docs.ValidID(section) || !docs.ValidID(name) {
		return usageErr("SECTION/PAGE is two names of lower-case latin letters, digits and dashes: runbooks/deploy")
	}
	switch a.audience {
	case "", docs.AudiencePeople, docs.AudienceAgents, docs.AudienceAll:
	default:
		return usageErr("--audience is people, agents or all")
	}
	dirs := c.docsDirs()
	if len(dirs) == 0 {
		return fail("no folder for this machine's docs: set HARNESSLY_DOCS_DIR")
	}
	root := dirs[0]

	var raw []byte
	var err error
	if len(a.pos) == 2 && a.pos[1] != "-" {
		raw, err = os.ReadFile(c.absPath(a.pos[1]))
	} else {
		if c.env.Stdin == nil {
			return usageErr("docs add needs the Markdown: a FILE, or on stdin")
		}
		raw, err = io.ReadAll(io.LimitReader(c.env.Stdin, 2<<20))
	}
	if err != nil {
		return fail("the page: %v", err)
	}
	text := strings.TrimLeft(string(raw), "\ufeff\n\r\t ")
	if !strings.HasPrefix(text, "# ") {
		if a.title == "" {
			return usageErr("the page needs a title: a first line \"# Title\", or --title")
		}
		text = "# " + strings.TrimSpace(a.title) + "\n\n" + text
	}
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}

	dir := filepath.Join(root, section)
	file := filepath.Join(dir, name+".md")
	if name == "overview" {
		file = filepath.Join(dir, "README.md")
	}
	if _, err := os.Stat(file); err == nil && !a.replace {
		return fail("%s exists; --replace writes over it", file)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fail("%v", err)
	}
	if err := c.docsManifest(dir, section, a); err != nil {
		return err
	}
	for _, p := range a.pictures {
		if err := c.docsPicture(dir, p); err != nil {
			return err
		}
	}
	if err := os.WriteFile(file, []byte(text), 0o644); err != nil {
		return fail("%v", err)
	}
	c.out("wrote %s\n", file)
	c.out("it shows on the documentation page at /help/%s/%s; harnessly docs read %s/%s prints it\n", section, name, section, name)
	return nil
}

// docsManifest writes the section's section.json when it has none, or changes the title or audience asked for.
func (c *cli) docsManifest(dir, section string, a docsArgs) error {
	path := filepath.Join(dir, docs.ManifestFile)
	m := map[string]any{}
	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := json.Unmarshal(raw, &m); err != nil {
			return fail("%s: %v", path, err)
		}
	case errors.Is(err, os.ErrNotExist):
		// A built-in section gets pages added without a manifest of its own: its title and place stay.
		if lib, err := docs.Load([]docs.Source{docs.Builtin()}, docs.LocaleEN); err == nil {
			if _, builtin := lib.Section(section); builtin && a.sectionTitle == "" && a.audience == "" {
				return nil
			}
		}
	default:
		return fail("%v", err)
	}
	if a.sectionTitle != "" {
		titles, _ := m["title"].(map[string]any)
		if titles == nil {
			titles = map[string]any{}
		}
		titles[a.lang] = a.sectionTitle
		if _, ok := titles[docs.LocaleEN]; !ok {
			titles[docs.LocaleEN] = a.sectionTitle
		}
		m["title"] = titles
	}
	if a.audience != "" {
		m["audience"] = a.audience
	}
	if _, ok := m["audience"]; !ok {
		m["audience"] = docs.AudienceAll
	}
	out, _ := json.MarshalIndent(m, "", "  ")
	return writeFileErr(path, append(out, '\n'))
}

func writeFileErr(path string, raw []byte) error {
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return fail("%v", err)
	}
	return nil
}

func (c *cli) docsPicture(dir, from string) error {
	src := c.absPath(from)
	switch strings.ToLower(filepath.Ext(src)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg":
	default:
		return usageErr("--picture takes a png, jpg, gif, webp or svg: %s", from)
	}
	raw, err := os.ReadFile(src)
	if err != nil {
		return fail("the picture: %v", err)
	}
	if len(raw) > docs.MaxPicture {
		return fail("%s is larger than 8 MB", from)
	}
	if err := os.MkdirAll(filepath.Join(dir, "img"), 0o755); err != nil {
		return fail("%v", err)
	}
	to := filepath.Join(dir, "img", filepath.Base(src))
	if err := writeFileErr(to, raw); err != nil {
		return err
	}
	c.out("copied %s → img/%s (show it with ![what it shows](img/%s))\n", from, filepath.Base(src), filepath.Base(src))
	return nil
}
