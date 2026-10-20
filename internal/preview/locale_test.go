package preview

import (
	"errors"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/preview/weburl"
)

func TestMain(m *testing.M) {
	// The goldens are the previous runtime's answers, which were in Russian.
	os.Setenv("RUNTIME_LOCALE", "ru")
	os.Exit(m.Run())
}

var cyrillic = regexp.MustCompile(`[А-Яа-яЁё]`)

func TestEnglishPreviewPages(t *testing.T) {
	t.Setenv("RUNTIME_LOCALE", "en")
	u, _ := weburl.Parse("https://stand.example/app")
	pages := map[string]string{
		"status":  statusPage(502, "x"),
		"offer":   offerPage(u, "https://host.example", "nonce"),
		"sign-in": signInPage("https://stand.example/login", "https://host.example", "nonce"),
	}
	for name, want := range map[string][]string{
		"status":  {`<html lang="en">`, "<title>Preview · 502</title>"},
		"offer":   {"Preview · redirect", "The site redirects to https://stand.example", "Open in preview", "Open in a new tab"},
		"sign-in": {"Preview · sign-in", "The stand is behind a Keycloak sign-in", `Press "Sign in" above the page`, ">Sign in</a>"},
	} {
		for _, w := range want {
			if !strings.Contains(pages[name], w) {
				t.Errorf("%s page lacks %q", name, w)
			}
		}
		if cyrillic.MatchString(pages[name]) {
			t.Errorf("%s page has Russian text: %s", name, cyrillic.FindString(pages[name]))
		}
	}
}

func TestEnglishProxyRefusals(t *testing.T) {
	t.Setenv("RUNTIME_LOCALE", "en")
	port := &route{kind: "port", port: 4400}
	if status, text := failure(errors.New("x"), port); status != 502 || text != "The server on port 4400 is not responding" {
		t.Errorf("port: %d %q", status, text)
	}
	u, _ := weburl.Parse("https://site.example")
	site := &route{kind: "url", url: u}
	if _, text := failure(errors.New("x"), site); text != "site.example is not responding" {
		t.Errorf("site: %q", text)
	}
	if got := blockedText("*.ads.example"); got != "The request is blocked in DevTools: *.ads.example" {
		t.Errorf("blocked: %q", got)
	}
	if got := offlineText.String(); got != `No network: the "Offline" mode is on in DevTools` {
		t.Errorf("offline: %q", got)
	}
	t.Setenv("RUNTIME_LOCALE", "ru")
	if got := blockedText("a"); got != "Запрос заблокирован в DevTools: a" {
		t.Errorf("ru blocked: %q", got)
	}
}
