package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWebExport(t *testing.T) {
	t.Setenv("RUNTIME_WEB_DIR", webDir(t))
	t.Setenv("RUNTIME_APP_BASE_PATH", "")
	t.Setenv("RUNTIME_APP_NAME", "Acme")
	out := t.TempDir()
	if ran, err := subcommand([]string{"web", "export", out}); !ran || err != nil {
		t.Fatalf("export: %v %v", ran, err)
	}
	page, err := os.ReadFile(filepath.Join(out, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`<base href="/agents/" />`, `<meta name="api-base" content="/agents" />`, `<meta name="platform-api" content="/api/platform" />`} {
		if !strings.Contains(string(page), want) {
			t.Fatalf("the exported page lacks %s: %s", want, page)
		}
	}
	// The page the runtime serves and the exported one are the same bytes.
	c := webConfigFromEnv(true)
	served := serveWeb(c, "GET", "/agents/", nil).Body.String()
	if served != string(page) {
		t.Fatalf("served %s\nexported %s", served, page)
	}
	if raw, err := os.ReadFile(filepath.Join(out, "assets", "app-1a2b.js")); err != nil || string(raw) != "console.log(1)" {
		t.Fatalf("asset: %q %v", raw, err)
	}
}

func TestSubcommandLeavesOtherArgumentsToTheServer(t *testing.T) {
	for _, args := range [][]string{nil, {"--listen", ":1"}, {"serve"}} {
		if ran, _ := subcommand(args); ran {
			t.Fatalf("%v is not a subcommand", args)
		}
	}
	if ran, err := subcommand([]string{"web"}); !ran || err == nil {
		t.Fatal("`web` alone is a usage error")
	}
}
