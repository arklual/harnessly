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
		if e.Transport == Stdio {
			if len(e.Command) < 2 || (e.Runtime != "npx" && e.Runtime != "uvx") || e.Command[0] != e.Runtime || e.URL != "" {
				t.Errorf("%s: a local server is npx or uvx with a pinned package, and no url", e.ID)
			}
			if e.Runtime == "npx" && !strings.Contains(e.Command[len(e.Command)-1], "@") && !slices.ContainsFunc(e.Command, func(c string) bool { return strings.Contains(c[1:], "@") }) {
				t.Errorf("%s: the npm package is not pinned", e.ID)
			}
			if e.Runtime == "uvx" && !slices.ContainsFunc(e.Command, func(c string) bool { return strings.Contains(c, "==") || strings.Contains(c, "@") }) {
				t.Errorf("%s: the Python package is not pinned", e.ID)
			}
		}
		if e.Transport == Stdio || e.Transport == API {
			names := map[string]bool{}
			for _, f := range e.Env {
				if f.Name == "" || strings.ToUpper(f.Name) != f.Name || strings.TrimSpace(f.Label) == "" || names[f.Name] {
					t.Errorf("%s: env field %+v", e.ID, f)
				}
				names[f.Name] = true
			}
			required := slices.ContainsFunc(e.Env, func(f EnvField) bool { return !f.Optional })
			if required != (e.Auth == AuthEnv) {
				t.Errorf("%s: a keyed entry signs in with env when it has a required field, else none", e.ID)
			}
			for _, tpl := range append(mapValues(e.APIHeaders), mapValues(e.APIQuery)...) {
				for _, m := range placeholder.FindAllStringSubmatch(tpl, -1) {
					if !names[m[1]] {
						t.Errorf("%s: {%s} is not a field", e.ID, m[1])
					}
				}
			}
		}
		if e.Transport == Stdio {
			for field, raw := range map[string]string{"docsUrl": e.DocsURL, "tokenUrl": e.TokenURL} {
				if raw == "" && field == "tokenUrl" {
					continue
				}
				if u, err := url.Parse(raw); err != nil || u.Scheme != "https" || u.Host == "" {
					t.Errorf("%s: %s %q is not an https address", e.ID, field, raw)
				}
			}
			checkBlurbs(t, e)
			continue
		}
		if e.Transport != HTTP && e.Transport != SSE && e.Transport != API {
			t.Errorf("%s: transport %q", e.ID, e.Transport)
		}
		if e.Auth != AuthOAuth && e.Auth != AuthHeader && e.Auth != AuthNone && e.Auth != AuthEnv {
			t.Errorf("%s: auth %q", e.ID, e.Auth)
		}
		if e.Auth == AuthHeader && e.HeaderName == "" {
			t.Errorf("%s: a header sign-in without the header's name", e.ID)
		}
		if e.TokenPrefix != "" && (e.HeaderName != "Authorization" || strings.TrimSpace(e.TokenPrefix) == "") {
			t.Errorf("%s: a token prefix goes only in Authorization", e.ID)
		}

		addr := e.URL
		if e.Transport == API {
			addr = placeholder.ReplaceAllString(strings.NewReplacer("{MATTERMOST_URL}", "https://chat.example", "{BITRIX24_WEBHOOK}", "https://x.bitrix24.ru/rest/1/x").Replace(addr), "x")
		}
		for field, raw := range map[string]string{"url": addr, "docsUrl": e.DocsURL, "tokenUrl": e.TokenURL} {
			if raw == "" && field == "tokenUrl" {
				continue
			}
			u, err := url.Parse(raw)
			if err != nil || u.Scheme != "https" || u.Host == "" {
				t.Errorf("%s: %s %q is not an https address", e.ID, field, raw)
			}
		}
		canon, _, err := canonical(addr)
		if err != nil {
			t.Errorf("%s: url %q: %v", e.ID, e.URL, err)
		} else if other, seen := urls[canon]; seen {
			t.Errorf("%s: the same address as %s", e.ID, other)
		} else {
			urls[canon] = e.ID
		}

		checkBlurbs(t, e)
	}

	// Remote servers ours never signs in to today (a local server of the same service is another thing: Figma's runs
	// with a personal token).
	remoteIDs := map[string]bool{}
	for _, e := range catalog {
		if e.Transport == HTTP || e.Transport == SSE {
			remoteIDs[e.ID] = true
		}
	}
	for gone, why := range map[string]string{
		"figma": "admits only its own list of clients (a live registration answers 403)",
		"slack": "has no client registration",
		// Local packages that do not run at their pinned version (TestRealLocalServers, 2026-11-18).
		"moex-finmap": "finmap-mcp 3.3.6 misses dist/api.js", "weather": "the package does not run",
		"paper-search": "the package does not run", "google-sheets": "the package does not run",
		// /v2/mcp: no client registration and no personal token, only an OAuth app made in Asana's console
		// beforehand; back with our own OAuth client (wave 2 of HRN-1533).
		"asana": "needs an OAuth app registered beforehand",
		// Looked at for HRN-1706 and left out: the agent has its own browser (browser_* and chrome_* tools), so these
		// would start a second, empty one; Puppeteer's reference package is archived.
		"playwright": "duplicates the agent's own browser", "chrome-devtools": "duplicates the agent's own browser",
		"puppeteer": "archived, and duplicates the agent's own browser",
		// HubSpot's and Box's hosted servers let no client register (no registration endpoint, only an app made in
		// their consoles beforehand): HubSpot is the local server with a private app token, Box has no working package.
		"hubspot-remote": "has no client registration", "box": "has no client registration, and the local package fails to import",
	} {
		if remoteIDs[gone] || ids[gone] && !strings.HasPrefix(why, "admits") && !strings.HasPrefix(why, "has no") {
			t.Errorf("%s is back in the catalog: it %s", gone, why)
		}
	}
	for _, kind := range Kinds {
		if !slices.ContainsFunc(catalog, func(e CatalogEntry) bool { return e.Kind == kind }) {
			t.Errorf("kind %q has no entry: drop it from Kinds or add one", kind)
		}
	}
}

func checkBlurbs(t *testing.T, e CatalogEntry) {
	t.Helper()
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

func mapValues(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

func hasCyrillic(s string) bool {
	for _, r := range s {
		if r >= 'А' && r <= 'я' {
			return true
		}
	}
	return false
}

// A service's own page (service_pages.go) is written by hand: each guide belongs to an entry, says what an agent can
// do in three to five phrases in both languages, and links only to official https pages, no address twice.
func TestCatalogGuides(t *testing.T) {
	kinds := []string{LinkDocs, LinkSetup, LinkToken, LinkPricing, LinkHome}
	for id, g := range catalogGuides {
		e, ok := catalogEntry(id)
		if !ok {
			t.Errorf("%s: a guide for an entry the catalog does not have", id)
			continue
		}
		if g.About == "" || g.AboutRu == "" {
			t.Errorf("%s: the description is in both languages", id)
		}
		if len(g.Can) < 3 || len(g.Can) > 5 || len(g.Can) != len(g.CanRu) {
			t.Errorf("%s: three to five things the agent can do, in both languages (%d/%d)", id, len(g.Can), len(g.CanRu))
		}
		if len(g.Steps) != len(g.StepsRu) {
			t.Errorf("%s: the steps are in both languages (%d/%d)", id, len(g.Steps), len(g.StepsRu))
		}
		if len(e.Can) != len(g.Can) {
			t.Errorf("%s: the guide did not reach the entry", id)
		}
		seen := map[string]bool{}
		for _, l := range g.Links {
			u, err := url.Parse(l.URL)
			if err != nil || u.Scheme != "https" || u.Host == "" {
				t.Errorf("%s: link %q is not an https address", id, l.URL)
			}
			if !slices.Contains(kinds, l.Kind) {
				t.Errorf("%s: link kind %q", id, l.Kind)
			}
			if seen[l.URL] {
				t.Errorf("%s: link %q twice", id, l.URL)
			}
			seen[l.URL] = true
		}
		// A token form that the page links to is the entry's own.
		if e.TokenURL != "" && !seen[e.TokenURL] {
			t.Logf("%s: the token link of the entry is not among the guide's links", id)
		}
		for _, line := range slices.Concat(g.Can, g.CanRu, g.Steps, g.StepsRu) {
			if strings.TrimSpace(line) == "" || strings.Contains(line, "  ") {
				t.Errorf("%s: a blank or doubled-space line %q", id, line)
			}
		}
	}
	// Every entry has a page: written by hand, in both languages, with steps and official links.
	for _, e := range catalog {
		g, ok := catalogGuides[e.ID]
		if !ok {
			t.Errorf("%s: the entry has no page: add it to a service_pages_*.go file", e.ID)
			continue
		}
		if len(g.Steps) < 2 {
			t.Errorf("%s: the page needs the steps to connect (at least two)", e.ID)
		}
		if len(g.Links) < 2 {
			t.Errorf("%s: the page needs at least two official links", e.ID)
		}
		if !slices.ContainsFunc(g.Links, func(l Link) bool { return l.Kind == LinkDocs || l.Kind == LinkHome }) {
			t.Errorf("%s: the page links neither to the docs nor to the product", e.ID)
		}
		if !hasCyrillic(g.AboutRu) || slices.ContainsFunc(g.CanRu, func(s string) bool { return !hasCyrillic(s) }) ||
			slices.ContainsFunc(g.StepsRu, func(s string) bool { return !hasCyrillic(s) }) {
			t.Errorf("%s: the Russian text is not Russian", e.ID)
		}
		if hasCyrillic(g.About) || slices.ContainsFunc(slices.Concat(g.Can, g.Steps), hasCyrillic) {
			t.Errorf("%s: the English text holds Russian", e.ID)
		}
		if len(g.About) < 60 || len(g.AboutRu) < 60 {
			t.Errorf("%s: the description is too short to say anything", e.ID)
		}
	}
}
