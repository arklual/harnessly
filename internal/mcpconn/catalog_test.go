package mcpconn

import (
	"net/url"
	"slices"
	"strings"
	"testing"
)

// The catalog is what the page offers to connect: every entry must be whole, reachable by https and tell the person
// in one Russian sentence what an agent gets from it.
func TestCatalogIntegrity(t *testing.T) {
	if len(catalog) == 0 {
		t.Fatal("the catalog is empty")
	}
	ids := map[string]bool{}
	urls := map[string]string{}
	for _, e := range catalog {
		if e.ID == "" || strings.ToLower(e.ID) != e.ID || strings.ContainsAny(e.ID, " _/") {
			t.Errorf("%q: the id must be a lowercase slug", e.ID)
		}
		if ids[e.ID] {
			t.Errorf("%q: the id is not unique", e.ID)
		}
		ids[e.ID] = true

		if strings.TrimSpace(e.Name) == "" {
			t.Errorf("%s: no name", e.ID)
		}
		if !slices.Contains(Kinds, e.Kind) {
			t.Errorf("%s: kind %q is not one of Kinds", e.ID, e.Kind)
		}
		if e.Transport != HTTP && e.Transport != SSE {
			t.Errorf("%s: transport %q", e.ID, e.Transport)
		}
		if e.Auth != AuthOAuth && e.Auth != AuthHeader && e.Auth != AuthNone {
			t.Errorf("%s: auth %q", e.ID, e.Auth)
		}
		if e.Auth == AuthHeader && e.HeaderName == "" {
			t.Errorf("%s: a header sign-in without the header's name", e.ID)
		}
		if e.TokenPrefix != "" && (e.HeaderName != "Authorization" || strings.TrimSpace(e.TokenPrefix) == "") {
			t.Errorf("%s: a token prefix goes only in Authorization", e.ID)
		}

		for field, raw := range map[string]string{"url": e.URL, "docsUrl": e.DocsURL, "tokenUrl": e.TokenURL} {
			if raw == "" && field == "tokenUrl" {
				continue
			}
			u, err := url.Parse(raw)
			if err != nil || u.Scheme != "https" || u.Host == "" {
				t.Errorf("%s: %s %q is not an https address", e.ID, field, raw)
			}
		}
		canon, _, err := canonical(e.URL)
		if err != nil {
			t.Errorf("%s: url %q: %v", e.ID, e.URL, err)
		} else if other, seen := urls[canon]; seen {
			t.Errorf("%s: the same address as %s", e.ID, other)
		} else {
			urls[canon] = e.ID
		}

		if strings.TrimSpace(e.Blurb) == "" || strings.TrimSpace(e.BlurbRu) == "" {
			t.Errorf("%s: both blurbs are required", e.ID)
		}
		if !hasCyrillic(e.BlurbRu) {
			t.Errorf("%s: blurbRu %q is not Russian", e.ID, e.BlurbRu)
		}
		if strings.ContainsAny(e.BlurbRu, "\n") || strings.Count(e.BlurbRu, ". ") > 0 {
			t.Errorf("%s: blurbRu must be one sentence", e.ID)
		}
	}

	// Servers ours never signs in to today.
	for gone, why := range map[string]string{
		"figma":  "admits only its own list of clients",
		"square": "admits only its own list of clients",
		// /v2/mcp: no client registration and no personal token, only an OAuth app made in Asana's console
		// beforehand; back with our own OAuth client (wave 2 of HRN-1533).
		"asana": "needs an OAuth app registered beforehand",
	} {
		if ids[gone] {
			t.Errorf("%s is back in the catalog: it %s", gone, why)
		}
	}
	for _, kind := range Kinds {
		if !slices.ContainsFunc(catalog, func(e CatalogEntry) bool { return e.Kind == kind }) {
			t.Errorf("kind %q has no entry: drop it from Kinds or add one", kind)
		}
	}
}

func hasCyrillic(s string) bool {
	for _, r := range s {
		if r >= 'А' && r <= 'я' {
			return true
		}
	}
	return false
}
