package docs

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
)

func file(s string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(s)} }

func builtinFixture() Source {
	return Source{Origin: OriginBuiltin, FS: fstest.MapFS{
		"start/section.json":     file(`{"title":{"en":"Start","ru":"Старт"},"audience":"people","order":10}`),
		"start/README.md":        file("# What it is\n\n## Chats\n\nA chat with an agent.\n"),
		"start/02-pane.md":       file("# The pane\n\n![main](img/pane.png)\n"),
		"start/01-first.md":      file("# First chat\n"),
		"start/img/pane.png":     file("PNG"),
		"ru/start/README.md":     file("# Что это\n\n## Чаты\n"),
		"inside/section.json":    file(`{"title":{"en":"Inside"},"order":50,"pages":["README.md","../arch.md","../hub/arch.md"]}`),
		"inside/README.md":       file("# Overview\n"),
		"arch.md":                file("# Architecture\n\nThe runtime gate.\n"),
		"hub/arch.md":            file("# The hub\n"),
		"secret.txt":             file("no"),
		"bad/README.md":          file("# not a section: no section.json\n"),
		"agents/section.json":    file(`{"title":{"en":"Agents"},"audience":"agents","order":40}`),
		"agents/README.md":       file("# For agents\n\nRun harnessly docs search.\n"),
		"ru/agents/section.json": file(`{}`),
	}}
}

func TestLoadOrdersSectionsAndPages(t *testing.T) {
	lib, err := Load([]Source{builtinFixture()}, LocaleEN)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, s := range lib.Sections {
		ids = append(ids, s.ID)
	}
	if strings.Join(ids, ",") != "start,agents,inside" {
		t.Fatalf("sections: %v", ids)
	}
	start := lib.Sections[0]
	var names []string
	for _, p := range start.Pages {
		names = append(names, p.Name)
	}
	if strings.Join(names, ",") != "overview,first,pane" || start.Audience != AudiencePeople || start.Title != "Start" {
		t.Errorf("start: %v %+v", names, start)
	}
	inside, _ := lib.Section("inside")
	if len(inside.Pages) != 3 || inside.Pages[1].Name != "arch" || inside.Pages[2].Name != "hub-arch" || inside.Audience != AudienceAll {
		t.Errorf("inside: %+v", inside.Pages)
	}
}

func TestLoadLocaleFallsBackToEnglish(t *testing.T) {
	lib, err := Load([]Source{builtinFixture()}, LocaleRU)
	if err != nil {
		t.Fatal(err)
	}
	start := lib.Sections[0]
	if start.Title != "Старт" || start.Pages[0].Title != "Что это" || start.Pages[1].Title != "First chat" {
		t.Errorf("ru: %+v", start)
	}
	raw, _ := start.Pages[0].Markdown()
	if !strings.HasPrefix(string(raw), "# Что это") {
		t.Errorf("ru text: %q", raw)
	}
	if start.Pages[0].File != "start/README.md" {
		t.Errorf("file is the English path: %q", start.Pages[0].File)
	}
	if inside, _ := lib.Section("inside"); inside.Title != "Inside" {
		t.Errorf("title fallback: %q", inside.Title)
	}
}

func TestManifestCannotLeaveTheSource(t *testing.T) {
	src := Source{Origin: OriginBuiltin, FS: fstest.MapFS{
		"x/section.json": file(`{"pages":["../../etc/passwd.md"]}`),
	}}
	if _, err := Load([]Source{src}, LocaleEN); err == nil {
		t.Fatal("a page outside the source was accepted")
	}
}

func TestLocalFolderAddsReplacesAndSurvivesBreakage(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, s string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("start/first.md", "# Our first chat\n") // replaces the built-in "first"
	write("start/vpn.md", "# Our VPN\n")          // adds to the built-in section
	write("runbooks/section.json", `{"title":{"en":"Runbooks"},"order":20}`)
	write("runbooks/deploy.md", "# Deploy our service\n\nkubectl rollout restart.\n")
	write("onboarding/README.md", "# Onboarding\n") // a folder with no manifest
	write("broken/section.json", `{not json`)       // left out, not fatal
	write("broken/a.md", "# A\n")
	write("Team notes.md", "# Notes\n") // loose file: the team section
	write(".git/HEAD.md", "# not docs\n")

	lib, err := Load(append([]Source{builtinFixture()}, Sources([]string{dir, filepath.Join(dir, "missing")})[1:]...), LocaleEN)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, s := range lib.Sections {
		ids = append(ids, s.ID)
	}
	if strings.Join(ids, ",") != "start,runbooks,agents,inside,onboarding,team" {
		t.Fatalf("sections: %v", ids)
	}
	first, _ := lib.Find("start", "first")
	if first.Title != "Our first chat" || first.Origin != OriginLocal || first.Path() == "" {
		t.Errorf("replace: %+v", first)
	}
	if _, ok := lib.Find("start", "vpn"); !ok {
		t.Error("added page missing")
	}
	if team, _ := lib.Section(TeamSection); len(team.Pages) != 1 || team.Pages[0].Name != "team-notes" {
		t.Errorf("team: %+v", team.Pages)
	}
	if on, _ := lib.Section("onboarding"); on.Title != "Onboarding" || on.Audience != AudienceAll {
		t.Errorf("onboarding: %+v", on)
	}
	hits := lib.Search("rollout restart", 10)
	if len(hits) != 1 || hits[0].Section != "runbooks" || !strings.Contains(hits[0].Snippet, "rollout") {
		t.Errorf("search: %+v", hits)
	}
}

func TestPictureStaysInTheSourceAndIsAPicture(t *testing.T) {
	lib, _ := Load([]Source{builtinFixture()}, LocaleEN)
	pane, ok := lib.Find("start", "pane")
	if !ok {
		t.Fatal("no page")
	}
	if raw, kind, err := pane.Picture("img/pane.png"); err != nil || string(raw) != "PNG" || kind != "image/png" {
		t.Errorf("picture: %q %q %v", raw, kind, err)
	}
	for _, bad := range []string{"../secret.txt", "../../x.png", "/etc/x.png", "https://x/y.png", "img/../../secret.txt", "../arch.md"} {
		if _, _, err := pane.Picture(bad); err == nil {
			t.Errorf("%s was read", bad)
		}
	}
}

func TestSearchRanksTitleFirst(t *testing.T) {
	lib, _ := Load([]Source{builtinFixture()}, LocaleEN)
	hits := lib.Search("architecture", 10)
	if len(hits) == 0 || hits[0].Name != "arch" {
		t.Errorf("hits: %+v", hits)
	}
	if hits := lib.Search("chat agent", 10); len(hits) != 1 || hits[0].Heading != "Chats" {
		t.Errorf("heading: %+v", hits)
	}
	if lib.Search("   ", 10) != nil {
		t.Error("empty query")
	}
}

func TestEmbeddedLibraryLoads(t *testing.T) {
	for _, locale := range []string{LocaleEN, LocaleRU} {
		lib, err := Load([]Source{Builtin()}, locale)
		if err != nil {
			t.Fatalf("%s: %v", locale, err)
		}
		for _, id := range []string{"start", "agents", "inside", "self-host", "build-apps"} {
			s, ok := lib.Section(id)
			if !ok || len(s.Pages) == 0 {
				t.Errorf("%s: section %s missing or empty", locale, id)
			}
			for _, p := range s.Pages {
				if !ValidID(p.Name) {
					t.Errorf("%s/%s: not an address", id, p.Name)
				}
			}
		}
	}
}

// Every relative link and picture of a built-in page leads to a file the binary carries: a broken one would show as
// plain text, or no picture, with nobody noticing.
func TestEmbeddedLinksResolve(t *testing.T) {
	ref := regexp.MustCompile(`!?\[[^\]]*\]\(([^)\s]+)\)`)
	for _, locale := range []string{LocaleEN, LocaleRU} {
		lib, err := Load([]Source{Builtin()}, locale)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range lib.Sections {
			if s.ID == "build-apps" {
				continue // the apps guide links its example packages' folders, as the repository shows them
			}
			for _, p := range s.Pages {
				if !strings.HasPrefix(p.File, s.ID+"/") {
					continue // a page of the repository's docs: it links the repository's files, as GitHub shows them
				}
				raw, _ := p.Markdown()
				text := inline.ReplaceAllString(fenced.ReplaceAllString(string(raw), ""), "")
				for _, m := range ref.FindAllStringSubmatch(text, -1) {
					target, _, _ := strings.Cut(m[1], "#")
					if target == "" || strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") || strings.HasPrefix(target, "/") {
						continue
					}
					if strings.HasPrefix(m[0], "!") {
						if _, _, err := p.Picture(target); err != nil {
							t.Errorf("%s %s/%s: picture %s", locale, s.ID, p.Name, target)
						}
						continue
					}
					file := path.Clean(path.Join(path.Dir(p.Read), target))
					if _, err := fs.Stat(Guide, file); err != nil {
						t.Errorf("%s %s/%s: link %s (%s)", locale, s.ID, p.Name, m[1], file)
					}
				}
			}
		}
	}
}

var (
	fenced = regexp.MustCompile("(?s)(````.*?````|```.*?```)")
	inline = regexp.MustCompile("`[^`\n]*`")
)
