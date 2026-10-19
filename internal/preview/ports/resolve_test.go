package ports

import (
	"os"
	"path/filepath"
	"testing"
)

// A picked component is found in its project with grep where rg is missing; a hit outside the project, through a
// link, is dropped, and an Angular component brings its template.
func TestResolveComponent(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	write := func(p, body string) {
		_ = os.MkdirAll(filepath.Dir(filepath.Join(root, p)), 0o755)
		_ = os.WriteFile(filepath.Join(root, p), []byte(body), 0o644)
	}
	write("src/app.tsx", "export const App = () => null;\n")
	write("src/card.component.ts", "@Component({\n  selector: 'app-card',\n  templateUrl: './card.component.html'\n})\n")
	write("src/card.component.html", "<div></div>\n")
	write("node_modules/x/app.tsx", "const App = 1;\n")
	_ = os.WriteFile(filepath.Join(outside, "evil.tsx"), []byte("function App() {}\n"), 0o644)
	_ = os.Symlink(outside, filepath.Join(root, "linked"))
	s := Searcher{RG: "definitely-not-rg"}
	got, err := s.Resolve(root, Component{Name: "App"})
	if err != nil || len(got) != 1 || got[0] != (Match{"src/app.tsx", 1, "component"}) {
		t.Fatalf("react: %+v %v", got, err)
	}
	got, err = s.Resolve(root, Component{Name: "CardComponent", HostTag: "app-card"})
	if err != nil || len(got) != 2 || got[0] != (Match{"src/card.component.ts", 2, "component"}) || got[1] != (Match{"src/card.component.html", 1, "template"}) {
		t.Fatalf("angular: %+v %v", got, err)
	}
	got, _ = s.Resolve(root, Component{Name: "App", File: "src/app.tsx", Line: 7})
	if len(got) != 1 || got[0].Line != 7 {
		t.Errorf("react source: %+v", got)
	}
	if got, _ = s.Resolve(root, Component{Name: "App", File: "../../etc/passwd", Line: 1}); len(got) != 1 || got[0].File != "src/app.tsx" {
		t.Errorf("source outside the project must fall back to the search: %+v", got)
	}
}
