package share

import (
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

func TestMain(m *testing.M) {
	// The goldens are the previous runtime's answers, which were in Russian.
	os.Setenv("RUNTIME_LOCALE", "ru")
	os.Exit(m.Run())
}

func TestPlaceholderFollowsTheLocale(t *testing.T) {
	t.Setenv("RUNTIME_LOCALE", "en")
	if got := placeholder("known-secret"); got != "[hidden: known-secret]" {
		t.Errorf("en: %q", got)
	}
	if got := truncatedMark(); !strings.Contains(got, "[truncated: entry longer than 20,000 characters]") {
		t.Errorf("en mark: %q", got)
	}
	t.Setenv("RUNTIME_LOCALE", "ru")
	if got := placeholder("known-secret"); got != "[скрыто: known-secret]" {
		t.Errorf("ru: %q", got)
	}
}

// A value that is already a placeholder of either language stays as it is, whatever language the runtime speaks now.
func TestEitherPlaceholderPassesRedactionUnchanged(t *testing.T) {
	for _, lang := range []string{"en", "ru"} {
		t.Setenv("RUNTIME_LOCALE", lang)
		r := NewRedactor(RedactorConfig{})
		for _, text := range []string{
			"Authorization: Bearer [скрыто: bearer]",
			"Authorization: Bearer [hidden: bearer]",
			"password=[скрыто: kv-secret]",
			"password=[hidden: kv-secret]",
		} {
			if got := r.Redact(text); got != text {
				t.Errorf("%s: %q became %q", lang, text, got)
			}
		}
	}
}

func TestRedactionWritesTheRuntimesLanguage(t *testing.T) {
	const secret = "password=hunter2hunter2"
	t.Setenv("RUNTIME_LOCALE", "en")
	if got := NewRedactor(RedactorConfig{}).Redact(secret); got != "password=[hidden: kv-secret]" {
		t.Errorf("en: %q", got)
	}
	t.Setenv("RUNTIME_LOCALE", "ru")
	if got := NewRedactor(RedactorConfig{}).Redact(secret); got != "password=[скрыто: kv-secret]" {
		t.Errorf("ru: %q", got)
	}
}

const englishSnapshot = `{
  "title": "Fix the build",
  "provider": "claude",
  "model": "opus",
  "owner": {"sub": "alice@example.com"},
  "capturedAt": 1760000000000,
  "economics": {"tokens": {"input": 1500000, "output": 2500, "cacheRead": 0, "cacheWrite": 0}, "activeMs": 3900000, "wallMs": 30000, "turns": 1},
  "entries": [
    {"t": "user", "at": 1760000000000, "text": "Why does it fail?", "images": 2},
    {"t": "thinking", "at": 1760000001000, "text": "Looking at the log"},
    {"t": "action", "at": 1760000002000, "name": "Read", "summary": "main.go", "status": "ok"},
    {"t": "action", "at": 1760000003000, "name": "Bash", "summary": "go test", "status": "ok"},
    {"t": "assistant", "at": 1760000004000, "markdown": "Done ![shot](https://example.com/a.png) and ![](https://example.com/b.png)"},
    {"t": "plan", "at": 1760000005000, "markdown": "1. ship it"},
    {"t": "note", "at": 1760000006000, "text": "Turn interrupted"}
  ]
}`

func englishPage(t *testing.T) (Page, *jsvalue.Object) {
	v, err := jsvalue.Parse([]byte(englishSnapshot))
	if err != nil {
		t.Fatal(err)
	}
	return Page{Brand: "Harnessly"}, v.(*jsvalue.Object)
}

func TestEnglishPage(t *testing.T) {
	t.Setenv("RUNTIME_LOCALE", "en")
	page, snap := englishPage(t)
	html := page.RenderHTML(snap, NewRedactor(RedactorConfig{}), RenderOptions{Mode: ModePreview, Viewer: "bob", ExpiresAt: 1762600000000})
	for _, want := range []string{
		`<html lang="en">`,
		"Preview: this is how a colleague will see the snapshot.",
		" · read-only",
		"opened: bob",
		"snapshot 2025-10-09, 11:53 MSK",
		"author alice@example.com",
		"1.5M tokens",
		"active 1 h 5 min",
		"total 1 min",
		"1 turn",
		"[images hidden: 2]",
		"Model reasoning",
		"2 agent actions · reading and search: 1 · commands: 1",
		"[image hidden: shot]",
		"[image hidden]",
		"Plan ",
		"Secrets and paths are hidden automatically · link valid until 2025-11-08 · opened as bob",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("the English page lacks %q", want)
		}
	}
	if regexp.MustCompile(`[А-Яа-яЁё]`).MatchString(html) {
		t.Errorf("the English page has Russian text: %s", regexp.MustCompile(`[^<>]*[А-Яа-яЁё][^<>]*`).FindString(html))
	}
}

func TestEnglishMarkdown(t *testing.T) {
	t.Setenv("RUNTIME_LOCALE", "en")
	page, snap := englishPage(t)
	md := page.RenderMarkdown(snap, NewRedactor(RedactorConfig{}))
	for _, want := range []string{
		"_[images hidden: 2]_",
		"<summary>Model reasoning</summary>",
		"**Plan** · ",
		"<summary>2 agent actions · reading and search: 1 · commands: 1</summary>",
		"_Secrets and paths are hidden automatically_",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("the English Markdown lacks %q", want)
		}
	}
	if regexp.MustCompile(`[А-Яа-яЁё]`).MatchString(md) {
		t.Errorf("the English Markdown has Russian text")
	}
}

func TestRussianPageKeepsTheOldTexts(t *testing.T) {
	t.Setenv("RUNTIME_LOCALE", "ru")
	page, snap := englishPage(t)
	html := page.RenderHTML(snap, NewRedactor(RedactorConfig{}), RenderOptions{Mode: ModeShared})
	for _, want := range []string{`<html lang="ru">`, " · только чтение", "1,5 млн токенов", "активно 1 ч 5 мин", "[изображения скрыты: 2]", "МСК"} {
		if !strings.Contains(html, want) {
			t.Errorf("the Russian page lacks %q", want)
		}
	}
}

func TestPluralAndNumbersFollowTheLocale(t *testing.T) {
	t.Setenv("RUNTIME_LOCALE", "en")
	if got := formatNumber(1234567.25); got != "1,234,567.3" {
		t.Errorf("en number: %q", got)
	}
	if got := count(1, "turn", "turns", "ход", "хода", "ходов"); got != "turn" {
		t.Errorf("en one: %q", got)
	}
	if got := count(0, "turn", "turns", "ход", "хода", "ходов"); got != "turns" {
		t.Errorf("en zero: %q", got)
	}
	t.Setenv("RUNTIME_LOCALE", "ru")
	if got := formatNumber(1234567.25); got != "1 234 567,3" {
		t.Errorf("ru number: %q", got)
	}
	if got := count(3, "turn", "turns", "ход", "хода", "ходов"); got != "хода" {
		t.Errorf("ru few: %q", got)
	}
}

// A route answers its errors in the runtime's language.
func TestRouteErrorsFollowTheLocale(t *testing.T) {
	g := readGolden(t)
	rt, _, _ := testRoute(t, g)
	srv := serveRoute(t, rt, "/api/platform")
	for _, c := range []struct{ lang, want string }{
		{"en", "application/json is required"},
		{"ru", "Нужен application/json"},
	} {
		t.Setenv("RUNTIME_LOCALE", c.lang)
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/platform/snapshots", strings.NewReader("tabId=1"))
		req.Header.Set("Content-Type", "text/plain")
		req.AddCookie(&http.Cookie{Name: "session-token", Value: "person"})
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != http.StatusUnsupportedMediaType || !strings.Contains(string(body), c.want) {
			t.Errorf("%s: %d %s", c.lang, res.StatusCode, body)
		}
	}
	t.Setenv("RUNTIME_LOCALE", "en")
	if got := errLinkMissing.String(); got != "Link not found" {
		t.Errorf("en link: %q", got)
	}
	if got := CleanTitle("  "); got != "Untitled" {
		t.Errorf("en title: %q", got)
	}
}
