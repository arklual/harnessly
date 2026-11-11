package design

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/designkit"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// kitToolsLibrary is a library whose kit store holds the fixture kit of the designkit tests as acme-ui 1.0.0.
func kitToolsLibrary(t *testing.T) *Library {
	t.Helper()
	l := New(Config{LibraryDir: t.TempDir(), KitsDir: t.TempDir(), Texts: Texts{Tag: "test-design", CLI: "acme"}})
	installKitFixture(t, l.KitsDir(), "acme-ui", "1.0.0")
	return l
}

func installKitFixture(t *testing.T, kits, id, version string) {
	t.Helper()
	copyKitFixture(t, "../designkit/testdata/kit/acme-ui", filepath.Join(kits, id, version))
}

func copyKitFixture(t *testing.T, from, dst string) {
	t.Helper()
	src, _ := filepath.Abs(from)
	err := filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if info.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// callKit runs a tool and parses its JSON answer; the second value is whether the tool refused.
func callKit(t *testing.T, l *Library, tool string, args ...any) (map[string]any, string, bool) {
	t.Helper()
	res := KitTools{L: l}.Call(context.Background(), tool, jsvalue.NewObject(args...))
	content := res.Array("content")
	text := content[0].(*jsvalue.Object).String("text")
	isErr, _ := res.Get("isError")
	if isErr == true {
		return nil, text, true
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("%s: not JSON: %v\n%s", tool, err, text)
	}
	if out["schema"] != designkit.ViewSchema {
		t.Errorf("%s: schema %v", tool, out["schema"])
	}
	return out, text, false
}

func TestKitToolsAreListedAndOwnTheirNames(t *testing.T) {
	set := KitTools{L: nil}
	listed := map[string]bool{}
	for _, tl := range set.Tools() {
		o := tl.(*jsvalue.Object)
		name := o.String("name")
		listed[name] = true
		if !set.Has(name) || o.String("description") == "" || o.Object("inputSchema") == nil {
			t.Errorf("%s: has %v, desc %q", name, set.Has(name), o.String("description"))
		}
	}
	for _, n := range KitToolNames {
		if !listed[n] {
			t.Errorf("%s is not listed", n)
		}
	}
	if len(listed) != len(KitToolNames) || set.Has("kit_add") {
		t.Errorf("listed %v", listed)
	}
	if res := set.Call(context.Background(), "kit_list", jsvalue.NewObject()); res.Has("isError") == false {
		t.Error("without a library a call is refused")
	}
}

func TestKitListGivesKitsVersionsAndThemes(t *testing.T) {
	l := kitToolsLibrary(t)
	installKitFixture(t, l.KitsDir(), "acme-ui", "1.1.0")
	out, _, bad := callKit(t, l, "kit_list")
	if bad {
		t.Fatal("refused")
	}
	kits := out["kits"].([]any)
	k := kits[0].(map[string]any)
	if len(kits) != 1 || k["id"] != "acme-ui" || k["title"] != "Acme UI" || k["canvas"] != true {
		t.Fatalf("%v", kits)
	}
	if v := k["versions"].([]any); v[0] != "1.1.0" || v[1] != "1.0.0" {
		t.Errorf("newest first: %v", v)
	}
	if _, has := k["draft"]; has {
		t.Errorf("no draft yet: %v", k)
	}
}

func TestKitIndexComponentSearchTokensTemplatesAndCheck(t *testing.T) {
	l := kitToolsLibrary(t)

	idx, _, bad := callKit(t, l, "kit_index", "kit", "acme-ui")
	if bad || idx["version"] != "1.0.0" || idx["namespace"] != "Acme" || idx["next"] == "" {
		t.Fatalf("index: %v", idx)
	}

	comp, _, bad := callKit(t, l, "kit_component", "kit", "acme-ui", "name", "Button")
	if bad || comp["sampleJsx"] != `<Button variant="primary">Save</Button>` {
		t.Fatalf("component: %v", comp)
	}
	card := comp["card"].(map[string]any)
	if card["found"] != true || len(card["whenNotToUse"].([]any)) != 1 || comp["code"].(map[string]any)["import"] != "@acme/ui" {
		t.Errorf("component card/code: %v", comp)
	}
	if _, text, bad := callKit(t, l, "kit_component", "kit", "acme-ui", "name", "Nope"); !bad || !strings.Contains(text, "kit_search") {
		t.Errorf("unknown component: %v %q", bad, text)
	}

	found, _, _ := callKit(t, l, "kit_search", "kit", "acme-ui", "query", "panel")
	if r := found["results"].([]any); len(r) == 0 || r[0].(map[string]any)["ref"] != "Card" {
		t.Errorf("search by keyword: %v", found)
	}
	if _, _, bad := callKit(t, l, "kit_search", "kit", "acme-ui", "query", " "); !bad {
		t.Error("an empty query is refused")
	}

	tokens, _, _ := callKit(t, l, "kit_tokens", "kit", "acme-ui", "group", "color")
	colors := tokens["groups"].(map[string]any)["color"].([]any)
	if len(colors) != 2 || colors[0].(map[string]any)["var"] != "--acme-surface-1" || len(tokens["groups"].(map[string]any)) != 1 {
		t.Errorf("tokens: %v", tokens)
	}
	if _, text, bad := callKit(t, l, "kit_tokens", "kit", "acme-ui", "group", "nope"); !bad || !strings.Contains(text, "color") {
		t.Errorf("unknown family: %q", text)
	}

	list, _, _ := callKit(t, l, "kit_templates", "kit", "acme-ui")
	if ts := list["templates"].([]any); len(ts) != 1 {
		t.Errorf("templates: %v", list)
	}
	tpl, _, bad := callKit(t, l, "kit_template", "kit", "acme-ui", "id", "record-page")
	if bad || !strings.Contains(tpl["source"].(string), "Acme.Card") || tpl["template"] != "pages" {
		t.Errorf("template: %v", tpl)
	}

	chk, _, bad := callKit(t, l, "kit_check", "kit", "acme-ui")
	if bad || chk["ok"] != true || chk["components"] != float64(7) {
		t.Errorf("check: %v", chk)
	}
}

func TestKitIndexOfTheFixtureIsSmall(t *testing.T) {
	l := kitToolsLibrary(t)
	_, text, _ := callKit(t, l, "kit_index", "kit", "acme-ui")
	if len(text) > designkit.IndexBudget {
		t.Fatalf("a seven-component kit's index is %d bytes:\n%s", len(text), text)
	}
}

func TestKitToolsRefuseWhatIsNotThere(t *testing.T) {
	l := kitToolsLibrary(t)
	for name, args := range map[string][]any{
		"no kit id":       {},
		"unknown kit":     {"kit", "nope"},
		"bad id":          {"kit", "../acme-ui"},
		"unknown version": {"kit", "acme-ui", "version", "9.9.9"},
		"unknown theme":   {"kit", "acme-ui", "theme", "other"},
		"draft, none":     {"kit", "acme-ui", "version", designkit.DraftVersion},
	} {
		if _, text, bad := callKit(t, l, "kit_index", args...); !bad || !strings.HasPrefix(text, "refused: ") {
			t.Errorf("%s: refused %v: %q", name, bad, text)
		}
		if _, _, bad := callKit(t, l, "kit_check", args...); !bad && name != "unknown theme" {
			t.Errorf("%s: kit_check answered", name)
		}
	}
	// A folder that is not a whole installed version is not shown: only what the store lists is read.
	if err := os.MkdirAll(filepath.Join(l.KitsDir(), "acme-ui", ".unpack-1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, bad := callKit(t, l, "kit_index", "kit", "acme-ui", "version", ".unpack-1"); !bad {
		t.Error("a staging folder is not a version")
	}
}

func TestKitToolsReadTheDraftOfAProjectThePersonMayOpen(t *testing.T) {
	l, _ := kitLibrary(t)
	installKitFixture(t, l.KitsDir(), "acme-ui", "1.0.0")
	if _, _, bad := callKit(t, l, "kit_index", "kit", "acme-ui", "version", designkit.DraftVersion); !bad {
		t.Fatal("no draft yet")
	}
	_, err := l.StartKitDraft("acme-ui")
	if err != nil {
		t.Fatal(err)
	}
	out, _, bad := callKit(t, l, "kit_index", "kit", "acme-ui", "version", designkit.DraftVersion)
	if bad || out["version"] != designkit.DraftVersion {
		t.Fatalf("the maker reads the draft: %v", out)
	}
	list, _, _ := callKit(t, l, "kit_list")
	for _, k := range list["kits"].([]any) {
		if m := k.(map[string]any); m["id"] == "acme-ui" && m["draft"] != true {
			t.Errorf("the list marks the draft: %v", m)
		}
	}
}
