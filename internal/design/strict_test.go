package design

import (
	"path/filepath"
	"strings"
	"testing"
)

const strictKitManifest = `{
  "title": "Acme", "namespace": "Acme",
  "kit": {"id": "acme-ds", "version": "1.0.0", "source": "library", "package": "@acme/ui", "root": "Root", "tokenPrefix": "--acme-"STRICT},
  "groups": [{"id": "g", "title": "G", "components": [
    {"id": "Root", "description": "The provider every page sits in", "code": {"import": "@acme/ui", "export": "Root"}, "props": [{"name": "children", "type": "node"}]},
    {"id": "Button", "description": "The main way to act on a page", "code": {"import": "@acme/ui", "export": "Button"},
     "props": [{"name": "variant", "type": "enum", "values": ["primary", "secondary"]}, {"name": "children", "type": "node"}]},
    {"id": "Badge", "description": "An old label kept for old pages", "status": "deprecated", "replacedBy": "Button", "code": {"import": "@acme/ui", "export": "Badge"}}
  ]}]
}`

// strictRig is a library with a project "x" pinned to a kit (theme "plain"), a page, a local component and a board.
func strictRig(t *testing.T, kitStrict, projectStrict bool) (*Library, string) {
	t.Helper()
	t.Setenv("RUNTIME_LOCALE", "")
	root, _ := filepath.EvalSymlinks(t.TempDir())
	lib, kits := filepath.Join(root, "lib"), filepath.Join(root, "kits")
	project := filepath.Join(lib, "x", "project")
	meta := `{"v":1,"title":"X","kit":{"id":"acme-ds","version":"1.0.0","mounts":["plain"]}}`
	if projectStrict {
		meta = strings.Replace(meta, `"v":1`, `"v":1,"strict":true`, 1)
	}
	writeFile(t, filepath.Join(lib, "x", "meta", "project.json"), meta)
	manifest := strings.Replace(strictKitManifest, "STRICT", "", 1)
	if kitStrict {
		manifest = strings.Replace(strictKitManifest, "STRICT", `, "strict": true`, 1)
	}
	v := filepath.Join(kits, "acme-ds", "1.0.0")
	writeFile(t, filepath.Join(v, "kit.json"), `{"title": "Acme", "themes": [{"id": "plain", "folder": "themes/plain", "namespace": "Acme"}]}`)
	writeFile(t, filepath.Join(v, "themes", "plain", "design-system.json"), manifest)
	writeFile(t, filepath.Join(v, "themes", "plain", "tokens.json"),
		`{"color": {"tokens": [{"name": "accent", "value": "#2563eb", "usage": "Links"}]}, "spacing": {"tokens": [{"name": "space-4", "value": "16px", "usage": "Gaps"}]}}`)
	writeFile(t, filepath.Join(project, "pages", "home.tsx"), `import { Root, Button } from "@acme/ui";
import { Title } from "../components/title";
import { rows } from "./home.fixtures";

export default function Home() {
  return (
    <Root>
      <Title text={rows[0].name} />
      <Button variant="primary">Go</Button>
    </Root>
  );
}
`)
	writeFile(t, filepath.Join(project, "pages", "home.fixtures.ts"), "export const rows = [{ name: 'A', color: '#ff0000' }];\n")
	writeFile(t, filepath.Join(project, "components", "title.tsx"),
		"export const Title = ({ text }: { text: string }) => <h1 style={{ color: 'var(--acme-accent)' }}>{text}</h1>;\n")
	writeFile(t, filepath.Join(project, "old.dc.html"), `<!doctype html><html><body><x-dc><main class="board">
<x-import component-from-global-scope="Acme.Root"><x-import component-from-global-scope="Acme.Button">Go</x-import></x-import>
</main></x-dc></body></html>
`)
	l := New(Config{LibraryDir: lib, KitsDir: kits, Texts: Texts{Tag: "test-design", CLI: "acme"}})
	t.Cleanup(func() { l.StopWatching(); l.StopPages() })
	return l, project
}

func TestStrictModeIsTurnedOnByTheProjectOrTheKit(t *testing.T) {
	cases := []struct {
		kit, project bool
		want         string
	}{{false, false, ""}, {false, true, "project"}, {true, false, "kit"}}
	for _, c := range cases {
		l, _ := strictRig(t, c.kit, c.project)
		if got := l.StrictOn("x"); got != c.want {
			t.Errorf("kit %v project %v: on %q, want %q", c.kit, c.project, got, c.want)
		}
	}
}

func TestStrictCheckOfAProjectOnAKit(t *testing.T) {
	l, project := strictRig(t, false, false)
	rep, err := l.CheckStrict("x", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Findings) != 0 || strings.Join(rep.Checked, ",") != "components/title.tsx,old.dc.html,pages/home.tsx" {
		t.Fatalf("a project made of the kit: %+v", rep)
	}
	// One of each case, in a page, a board and a local component.
	writeFile(t, filepath.Join(project, "pages", "home.tsx"), `import { Root, Button, Badge } from "@acme/ui";
export default function Home() {
  return (
    <Root>
      <div>raw</div>
      <Button size="l">Go</Button>
      <Badge />
    </Root>
  );
}
`)
	writeFile(t, filepath.Join(project, "components", "title.tsx"), "export const Title = () => <h1 style={{ padding: '13px' }} />;\n")
	writeFile(t, filepath.Join(project, "old.dc.html"), `<!doctype html><html><body><x-dc><main class="board">
<x-import component-from-global-scope="Acme.Root"><span>raw</span></x-import>
</main></x-dc></body></html>
`)
	rep, err = l.CheckStrict("x", nil)
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, f := range rep.Findings {
		lines = append(lines, f.String())
	}
	got := strings.Join(lines, "\n")
	for _, want := range []string{
		"components/title.tsx:1:51 strict.value — the length 13px is not a token",
		"old.dc.html:2:51 strict.element — <span> is plain HTML",
		"pages/home.tsx:5:8 strict.element — <div> is plain HTML",
		"pages/home.tsx:6:15 strict.prop — Button has no prop \"size\"",
		"pages/home.tsx:7:8 strict.deprecated — Badge is deprecated in the kit acme-ds. Fix: use Button (import { Button } from \"@acme/ui\")",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if len(rep.Findings) != 5 {
		t.Errorf("want 5 findings, got %d:\n%s", len(rep.Findings), got)
	}
	// Only the page named.
	rep, _ = l.CheckStrict("x", []string{"home"})
	if strings.Join(rep.Checked, ",") != "pages/home.tsx" {
		t.Errorf("only home: %v", rep.Checked)
	}
}

func TestStrictCheckNeedsAKit(t *testing.T) {
	l, _ := strictRig(t, false, false)
	writeFile(t, filepath.Join(l.MetaDir("x"), MetaProjectFile), `{"v":1,"title":"X"}`)
	if _, err := l.CheckStrict("x", nil); err == nil || !strings.Contains(err.Error(), "not on a kit") {
		t.Fatalf("no kit: %v", err)
	}
}
