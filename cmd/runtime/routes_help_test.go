package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The guide for the /help page: the person's session only, the pages of the embedded docs, and nothing but those pages.
func TestHelpRoutes(t *testing.T) {
	call, _ := appRig(t)
	page := map[string]string{"X-Test-Session": "yes"}
	get := func(path string, h map[string]string) (int, string) { return call("GET", platformAPI+path, "", h) }

	if code, _ := get("/help/guide", nil); code != http.StatusUnauthorized {
		t.Errorf("list, no session: %d", code)
	}
	if code, _ := get("/help/guide/page?name=views", nil); code != http.StatusUnauthorized {
		t.Errorf("page, no session: %d", code)
	}

	code, body := get("/help/guide?locale=en", page)
	var listed struct {
		Pages []struct {
			Name, Title string
			Headings    []string
		} `json:"pages"`
	}
	if code != http.StatusOK || json.Unmarshal([]byte(body), &listed) != nil || len(listed.Pages) < 3 {
		t.Fatalf("list: %d %s", code, body)
	}
	if listed.Pages[0].Name != "overview" || listed.Pages[0].Title == "" || listed.Pages[len(listed.Pages)-1].Name != "reference" {
		t.Errorf("order or titles: %+v", listed.Pages)
	}

	code, body = get("/help/guide/page?name=first-plugin&locale=en", page)
	var one struct{ Markdown string }
	if code != http.StatusOK || json.Unmarshal([]byte(body), &one) != nil || !strings.HasPrefix(one.Markdown, "# ") {
		t.Fatalf("page: %d %.80s", code, body)
	}
	// The guide has a Russian page, and the reference does not: there the English one comes.
	code, body = get("/help/guide/page?name=first-plugin&locale=ru", page)
	var ru struct{ Markdown string }
	if code != http.StatusOK || json.Unmarshal([]byte(body), &ru) != nil || ru.Markdown == one.Markdown {
		t.Errorf("Russian page: %d", code)
	}
	_, enRef := get("/help/guide/page?name=reference&locale=en", page)
	if code, ruRef := get("/help/guide/page?name=reference&locale=ru", page); code != http.StatusOK || ruRef != enRef {
		t.Errorf("fallback to English: %d", code)
	}

	for _, name := range []string{"nope", "../../go.mod", "..%2F..%2Fgo.mod", "build-apps/01-first-plugin", "", "ru/overview"} {
		if code, _ := get("/help/guide/page?name="+url.QueryEscape(name), page); code != http.StatusNotFound {
			t.Errorf("name %q: %d, want 404", name, code)
		}
	}
	if code, _ := get("/help/guide/page?name=overview&locale=../../x", page); code != http.StatusOK {
		t.Errorf("a strange locale reads English: %d", code)
	}
}

// The library: built-in sections and the machine's own folder, read again on each ask; pictures only from the page's source.
func TestHelpLibraryRoutes(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HARNESSLY_DOCS_DIR", dir)
	call, _ := appRig(t)
	page := map[string]string{"X-Test-Session": "yes"}
	get := func(path string, h map[string]string) (int, string) { return call("GET", platformAPI+path, "", h) }

	if code, _ := get("/help/library", nil); code != http.StatusUnauthorized {
		t.Errorf("no session: %d", code)
	}
	type section struct {
		ID, Title, Audience, Origin string
		Pages                       []struct{ Name, Title, File string }
	}
	list := func() []section {
		code, body := get("/help/library?locale=ru", page)
		var out struct{ Sections []section }
		if code != http.StatusOK || json.Unmarshal([]byte(body), &out) != nil {
			t.Fatalf("library: %d %.200s", code, body)
		}
		return out.Sections
	}
	before := list()
	if len(before) < 5 || before[0].ID != "start" {
		t.Fatalf("built-in sections: %+v", before)
	}

	// A page written into the folder shows on the next ask, with its picture.
	if err := os.MkdirAll(filepath.Join(dir, "runbooks", "img"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "runbooks", "deploy.md"), []byte("# Deploy\n\n![x](img/x.png)\n\nrollout restart\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "runbooks", "img", "x.png"), []byte("\x89PNG"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "secret.md.txt"), []byte("no"), 0o644)
	after := list()
	if len(after) != len(before)+1 || after[len(after)-1].ID != "runbooks" || after[len(after)-1].Origin != "local" {
		t.Fatalf("local section: %+v", after[len(after)-1])
	}
	code, body := get("/help/library/page?section=runbooks&name=deploy", page)
	if code != http.StatusOK || !strings.Contains(body, "rollout restart") {
		t.Errorf("page: %d %s", code, body)
	}
	code, body = get("/help/library/search?q=rollout+restart", page)
	if code != http.StatusOK || !strings.Contains(body, `"section":"runbooks"`) {
		t.Errorf("search: %d %s", code, body)
	}
	if code, body := get("/help/library/picture?section=runbooks&name=deploy&src=img/x.png", page); code != http.StatusOK || body != "\x89PNG" {
		t.Errorf("picture: %d %q", code, body)
	}
	for _, src := range []string{"../secret.md.txt", "../../go.mod", "img/../../secret.md.txt"} {
		if code, _ := get("/help/library/picture?section=runbooks&name=deploy&src="+url.QueryEscape(src), page); code != http.StatusNotFound {
			t.Errorf("picture %q: %d", src, code)
		}
	}
	if code, _ := get("/help/library/page?section=..&name=..", page); code != http.StatusNotFound {
		t.Errorf("traversal: %d", code)
	}
}
