package main

import (
	"encoding/json"
	"net/http"
	"net/url"
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
