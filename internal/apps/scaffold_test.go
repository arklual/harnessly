package apps

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Every template is a package that passes validate as it is, with no warnings (HRN-924).
func TestScaffoldTemplatesPassValidateStrict(t *testing.T) {
	for _, tpl := range Templates {
		dir := filepath.Join(t.TempDir(), "demo")
		if err := Scaffold(dir, "acme.demo", "Don't <break>", tpl); err != nil {
			t.Fatalf("%s: %v", tpl, err)
		}
		_, problems := LoadPackage(dir)
		problems = append(problems, DesignProblems(dir)...)
		if len(problems) > 0 {
			t.Errorf("%s: %v", tpl, problems)
		}
	}
}

// A template draws a state for loading and one for an error, in the host's own style, and speaks protocol 2.
func TestScaffoldPaneChatLooksLikeHarnessly(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo")
	if err := Scaffold(dir, "acme.demo", "Demo", "pane-chat"); err != nil {
		t.Fatal(err)
	}
	page, _ := os.ReadFile(filepath.Join(dir, "ui/index.html"))
	js, _ := os.ReadFile(filepath.Join(dir, "ui/main.js"))
	if !strings.Contains(string(page), "/__hl/hl-ui.css") || !strings.Contains(string(page), "hl-body") {
		t.Errorf("the page does not load hl-ui.css:\n%s", page)
	}
	for _, want := range []string{"hl-skeleton", "showError", "protocol: 2", "hl.refused"} {
		if !strings.Contains(string(js), want) {
			t.Errorf("main.js lacks %q:\n%s", want, js)
		}
	}
	if strings.Contains(string(js), "catch(function () {})") {
		t.Error("main.js swallows the error of connect()")
	}
}

// The vite template builds into the view's folder with relative addresses, and asks for a host that serves modules.
func TestScaffoldViteBuildsIntoUI(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo")
	if err := Scaffold(dir, "acme.demo", "Demo", "vite"); err != nil {
		t.Fatal(err)
	}
	cfg, _ := os.ReadFile(filepath.Join(dir, "vite.config.ts"))
	for _, want := range []string{"base: './'", "outDir: 'ui'"} {
		if !strings.Contains(string(cfg), want) {
			t.Errorf("vite.config.ts lacks %q", want)
		}
	}
	pkg, _ := LoadPackage(dir)
	if pkg == nil || pkg.Manifest.Harnessly != ">="+ViteTemplateVersion {
		t.Errorf("the manifest does not ask for %s: %+v", ViteTemplateVersion, pkg)
	}
	if _, err := os.Stat(filepath.Join(dir, "src/app-sdk.d.ts")); err != nil {
		t.Error(err)
	}
}

// The version a template asks for is the one the guide shows.
func TestTemplateVersionMatchesTheGuide(t *testing.T) {
	re := regexp.MustCompile(`"harnessly": ">=([0-9.]+)"`)
	for _, f := range []string{"01-first-plugin.md", "08-worked-example.md"} {
		for _, root := range []string{"../../docs/build-apps", "../../docs/ru/build-apps"} {
			raw, err := os.ReadFile(filepath.Join(root, f))
			if err != nil {
				t.Fatal(err)
			}
			m := re.FindSubmatch(raw)
			if m == nil || string(m[1]) != TemplateVersion {
				t.Errorf("%s/%s asks for %v, the templates for %s", root, f, m, TemplateVersion)
			}
		}
	}
}
