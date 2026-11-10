package apps

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The design check warns of what makes a view look foreign, with the file and line, and leaves alone what is fine:
// tokens, var() fallbacks, comments, dependencies.
func TestDesignProblems(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, text string) {
		p := filepath.Join(dir, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("views/main/index.html", "<!doctype html>\n<link rel=\"stylesheet\" href=\"/__hl/hl-ui.css\">\n<div style=\"color:red\">x</div>\n<select><option>a</option></select>\n<style>\n/* Цвет: #fff в комментарии */\n.a { color: var(--t-text, #111); }\n.b { background: #ff0000; }\n</style>\n")
	write("views/main/style.css", ":root { --d-bg: #ffffff; }\n.ok { color: var(--t-accent, rgb(0 0 0)); }\n/* #abc */\n.bad {\n  border: 1px solid rgba(0,0,0,.1);\n}\n")
	write("views/clean/index.html", "<div class=\"hl-card\"><button class=\"hl-btn\">ok</button></div>\n")
	write("templates/board/board.dc.html", "<div style=\"background:#fff\"></div>\n")
	write("node_modules/x/a.css", ".x { color: #000; }\n")
	got := DesignProblems(dir)
	var lines []string
	for _, p := range got {
		if !p.Warning {
			t.Errorf("an error, not a warning: %s", p)
		}
		lines = append(lines, p.Path+" "+strings.SplitN(p.Message, ":", 2)[0])
	}
	want := []string{
		"views/main/index.html:3 an inline style attribute",
		"views/main/index.html:4 a native <select> looks foreign in the pane",
		"views/main/index.html:8 a colour written out (#ff0000)",
		"views/main/style.css:5 a colour written out (rgba)",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
}

// An address from the root misses the session in the view's path: warned in the view's own folder, not for the host's /__hl/.
func TestDesignProblemsRootAddress(t *testing.T) {
	dir := t.TempDir()
	for rel, text := range map[string]string{
		"ui/index.html": "<link rel=\"stylesheet\" href=\"/__hl/hl-ui.css\">\n<script type=\"module\" crossorigin src=\"/assets/index.js\"></script>\n",
		"ui/ok.html":    "<script src=\"/__hl/app-sdk.js\"></script><script type=\"module\" src=\"./assets/index.js\"></script><a href=\"#x\">x</a>\n",
		"index.html":    "<script type=\"module\" src=\"/src/main.tsx\"></script>\n",
	} {
		p := filepath.Join(dir, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := DesignProblems(dir)
	if len(got) != 1 || got[0].Path != "ui/index.html:2" || !strings.Contains(got[0].Message, "base: './'") {
		t.Fatalf("got %v", got)
	}
}
