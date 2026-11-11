package journal

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

func open(t *testing.T) *Journal {
	t.Helper()
	j := Open(t.TempDir(), Options{})
	t.Cleanup(j.Close)
	return j
}

func TestWriteQueryAndChain(t *testing.T) {
	j := open(t)
	now := time.Now()
	at := func(d time.Duration) int64 { return now.Add(d).UnixMilli() }
	j.Add(Entry{At: at(-20 * time.Second), Level: Info, Source: SourceWeb, Event: "view.open", App: "x.app", Chat: "c1", Trace: "v-1"})
	j.Add(Entry{At: at(-19 * time.Second), Level: Info, Source: SourceRuntime, Event: "http", Method: "POST", URL: "/api/platform/apps/session", Status: 200, Dur: 12, Trace: "v-1", App: "x.app", Chat: "c1"})
	j.Add(Entry{At: at(-18 * time.Second), Level: Error, Source: SourceApp, Event: "preview.request", URL: "/tok/__hl/bridge-shim.js", Status: 401, App: "x.app", Chat: "c1", Trace: "v-1"})
	j.Add(Entry{At: at(-10 * time.Second), Level: Error, Source: SourceApp, Event: "ui.initialize", Text: "no handshake in 8 s", App: "x.app", Chat: "c1", Trace: "v-1"})
	j.Add(Entry{At: at(-5 * time.Second), Level: Info, Source: SourceWeb, Event: "nav", URL: "/c/other", Chat: "c2"})
	j.Add(Entry{At: at(-4 * time.Second), Level: Error, Source: SourceRuntime, Event: "mcp.call", Tool: "harnessly/preview_console", Text: "boom", Chat: "c2"})

	lines := j.Query(Filter{Since: now.Add(-time.Minute)})
	if len(lines) != 6 {
		t.Fatalf("lines = %d, want 6", len(lines))
	}
	if got := j.Query(Filter{Since: now.Add(-time.Minute), Level: Error}); len(got) != 3 {
		t.Fatalf("errors = %d, want 3", len(got))
	}
	if got := j.Query(Filter{Since: now.Add(-time.Minute), Source: "web,app"}); len(got) != 4 {
		t.Fatalf("web,app = %d, want 4", len(got))
	}

	chains := j.Chains(Filter{Since: now.Add(-time.Minute)})
	if len(chains) != 2 {
		t.Fatalf("chains = %d, want 2 (one per trace or error)", len(chains))
	}
	first := chains[0]
	if len(first.Errors) != 2 || len(first.Lines) != 4 {
		t.Fatalf("trace chain: %d errors, %d lines; want 2 and 4", len(first.Errors), len(first.Lines))
	}
	text := FormatChains(chains)
	for _, want := range []string{"bridge-shim.js", "401", "x.app", "trace=v-1", "/api/platform/apps/session", "12ms"} {
		if !strings.Contains(text, want) {
			t.Errorf("chains text lacks %q:\n%s", want, text)
		}
	}
	if r := j.Reason("x.app", "c1", "", now.Add(-time.Minute)); !strings.Contains(r, "bridge-shim.js") || !strings.Contains(r, "401") {
		t.Errorf("reason = %q", r)
	}
}

func TestReasonFromServedLines(t *testing.T) {
	j := open(t)
	j.Add(Entry{Level: Info, Source: SourceApp, Event: "view.page", URL: "/index.html", App: "x.app"})
	r := j.Reason("x.app", "", "", time.Now().Add(-time.Minute))
	if !strings.Contains(r, "bridge-shim.js") {
		t.Fatalf("a page with no shim: reason = %q", r)
	}
	j.Add(Entry{Level: Info, Source: SourceApp, Event: "view.sdk", URL: "/__hl/bridge-shim.js", App: "x.app"})
	if r := j.Reason("x.app", "", "", time.Now().Add(-time.Minute)); !strings.Contains(r, "harnessly.connect") {
		t.Fatalf("a page and a shim: reason = %q", r)
	}
}

func TestCleanHidesSecrets(t *testing.T) {
	cases := map[string]string{
		"Authorization: Bearer abcdefghijklmnop": "Authorization: …",
		"sent with Bearer abcdefghijklmnop":      "Bearer …",
		"token=abc123secret&x=1":                 "token=…",
		"password: hunter22":                     "password: …",
		"key sk-ant-api03-abcdefghijkl":          "…",
		"glpat-abcdefghijklmnop":                 "…",
		"code=4/0AeanS0Z":                        "code=…",
	}
	for in, want := range cases {
		got := Redact(in)
		if !strings.Contains(got, want) || strings.Contains(got, "hunter22") || strings.Contains(got, "abcdefghijklmnop") {
			t.Errorf("Redact(%q) = %q, want it to hold %q and no secret", in, got, want)
		}
	}
	if got := Redact("HTTP 401: status code: 401"); got != "HTTP 401: status code: 401" {
		t.Errorf("a status is not a secret: %q", got)
	}
	u := CleanURL("https://user:pw@host/a/AbCdEfGhIjKlMnOp1234567890/__hl/x.js?token=1#f")
	if u != "https://host/a/…/__hl/x.js" {
		t.Errorf("CleanURL = %q", u)
	}
	e := clean(Entry{Level: "nonsense", Text: "a\nb\x1b[31m", URL: "/x?y=1"}, time.Now())
	if e.Level != Info || strings.ContainsAny(e.Text, "\n\x1b") || e.URL != "/x" || e.At == 0 {
		t.Errorf("clean = %+v", e)
	}
}

func TestHourCapDropsInfoFirst(t *testing.T) {
	j := Open(t.TempDir(), Options{HourBytes: 400})
	defer j.Close()
	for i := 0; i < 20; i++ {
		j.Add(Entry{Level: Info, Source: SourceWeb, Event: "click", Tool: "a button"})
	}
	j.Add(Entry{Level: Error, Source: SourceWeb, Event: "exception", Text: "boom"})
	j.Flush()
	if j.Dropped() == 0 {
		t.Fatal("nothing dropped past the hour's cap")
	}
	if got := j.Query(Filter{Level: Error}); len(got) != 1 {
		t.Fatalf("the error past the info cap: %d lines", len(got))
	}
}

func TestPruneKeepsRetention(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, time.Now().Add(-100*time.Hour).UTC().Format("2006010215")+".jsonl")
	if err := os.WriteFile(old, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	j := Open(dir, Options{Keep: 72 * time.Hour})
	defer j.Close()
	j.Add(Entry{Event: "x"})
	j.Flush()
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("a file of 100 hours ago is still there: %v", err)
	}
}

func TestHTTPWritesFailuresAndChangesOnly(t *testing.T) {
	j := open(t)
	SetDefault(j)
	t.Cleanup(func() { SetDefault(nil) })
	h := HTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		SetChat(r.Context(), "c9")
		SetCall(r.Context(), "main", "storage.chat.set", "")
		switch r.URL.Path {
		case "/fail":
			http.Error(w, "no", http.StatusBadGateway)
		case "/denied":
			w.WriteHeader(http.StatusUnauthorized)
		case "/refused":
			SetLevel(r.Context(), Warn)
			_, _ = w.Write([]byte(`{"r":"refused"}`))
		default:
			_, _ = w.Write([]byte("ok"))
		}
	}), HTTPOptions{Quiet: func(r *http.Request) bool { return r.URL.Path == "/quiet" }})
	do := func(method, path string) {
		req := httptest.NewRequest(method, path+"?token=secret", nil)
		req.Header.Set(TraceHeader, "a-1")
		h.ServeHTTP(httptest.NewRecorder(), req)
	}
	do("GET", "/read")
	do("POST", "/quiet")
	do("POST", "/change")
	do("GET", "/fail")
	do("GET", "/denied")
	do("POST", "/refused")
	lines := j.Query(Filter{})
	if len(lines) != 4 {
		t.Fatalf("lines = %d (%+v), want 4: the change, 502, 401 and the refusal", len(lines), lines)
	}
	byURL := map[string]Entry{}
	for _, e := range lines {
		byURL[e.URL] = e
		if e.Trace != "a-1" || e.Chat != "c9" || e.Tool != "storage.chat.set" || strings.Contains(e.URL, "secret") {
			t.Errorf("line %+v: want the trace, chat and call, no query", e)
		}
	}
	if byURL["/fail"].Level != Error || byURL["/denied"].Level != Warn || byURL["/change"].Level != Info || byURL["/refused"].Level != Warn {
		t.Errorf("levels: %+v", byURL)
	}
}

func TestParseSince(t *testing.T) {
	now := time.Date(2026, 11, 9, 19, 0, 0, 0, time.UTC)
	for in, want := range map[string]time.Time{
		"":      now.Add(-30 * time.Minute),
		"10m":   now.Add(-10 * time.Minute),
		"1d":    now.Add(-24 * time.Hour),
		"18:30": time.Date(2026, 11, 9, 18, 30, 0, 0, time.UTC),
	} {
		got, ok := ParseSince(in, now, 30*time.Minute)
		if !ok || !got.Equal(want) {
			t.Errorf("ParseSince(%q) = %v %v, want %v", in, got, ok, want)
		}
	}
	if _, ok := ParseSince("soon", now, time.Minute); ok {
		t.Error("ParseSince(soon) took it")
	}
}

func TestReasonNamesTheFirstFailedFile(t *testing.T) {
	j := open(t)
	j.Add(Entry{Level: Error, Source: SourceApp, Event: "preview.request", URL: "/t/__hl/bridge-shim.js", Text: "blocked", App: "x.app", Trace: "v-2"})
	j.Add(Entry{Level: Error, Source: SourceApp, Event: "preview.request", URL: "/t/__hl/app-sdk.js", Text: "blocked", App: "x.app", Trace: "v-2"})
	if r := j.Reason("x.app", "", "", time.Now().Add(-time.Minute)); !strings.Contains(r, "bridge-shim.js") {
		t.Fatalf("reason = %q, want the first file of the attempt", r)
	}
}

func TestCleanURLHidesATokenWithoutADigit(t *testing.T) {
	if u := CleanURL("/RMSpaEcMVbGiK_zZSsyskZ-KcEKwlq/x.app/__hl/app-sdk.js"); u != "/…/x.app/__hl/app-sdk.js" {
		t.Errorf("a mixed-case token: %q", u)
	}
	if u := CleanURL("/__tk/s/eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ4In0.c2lnbmF0dXJlc2lnbmF0dXJl/v/ui/index.html"); u != "/__tk/s/…/v/ui/index.html" {
		t.Errorf("a gateway's carried session: %q", u)
	}
	if u := CleanURL("/x/example.shop/__hl/bridge-shim.js"); u != "/x/example.shop/__hl/bridge-shim.js" {
		t.Errorf("an app id is not a token: %q", u)
	}
	if u := CleanURL("/api/platform/conversation-history-sidebar"); u != "/api/platform/conversation-history-sidebar" {
		t.Errorf("a hyphenated name is not a token: %q", u)
	}
}

func TestToolsOwnKeepsTheCallerToItsChat(t *testing.T) {
	j := open(t)
	j.Add(Entry{Level: Error, Source: SourceApp, Event: "resource", URL: "/a.js", App: "x.app", Chat: "mine", Trace: "v-1"})
	j.Add(Entry{Level: Info, Source: SourceRuntime, Event: "http", URL: "/b", App: "x.app", Trace: "v-1"})
	j.Add(Entry{Level: Error, Source: SourceApp, Event: "resource", URL: "/secret-of-other.js", App: "x.app", Chat: "other"})
	j.Add(Entry{Level: Error, Source: SourceApp, Event: "resource", URL: "/secret-of-longer.js", App: "y.app", Chat: "mine-and-more"})
	args := jsvalue.NewObject("chat", "other", "since", "5m")
	text := func(o *jsvalue.Object) string {
		return fmt.Sprint(o.Array("content")[0].(*jsvalue.Object).String("text"))
	}
	out := text(Tools{J: j, Chat: "mine", Own: true}.Call(context.Background(), ToolName, args))
	if !strings.Contains(out, "/a.js") || strings.Contains(out, "secret-of-other") || strings.Contains(out, "/b ") {
		t.Errorf("own chat only:\n%s", out)
	}
	info := text(Tools{J: j, Chat: "mine", Own: true}.Call(context.Background(), ToolName, jsvalue.NewObject("level", "info", "since", "5m")))
	if strings.Contains(info, "secret-of") {
		t.Errorf("own chat only, not a chat it is the prefix of:\n%s", info)
	}
	if strings.Contains(out, "secret-of-longer") {
		t.Errorf("a chain of a chat with this prefix:\n%s", out)
	}
	if r := (Tools{J: j, Own: true}).Call(context.Background(), ToolName, args); !r.Has("isError") {
		t.Error("an unknown chat inside the wall got an answer")
	}
	if out := text(Tools{J: j}.Call(context.Background(), ToolName, args)); !strings.Contains(out, "secret-of-other") {
		t.Errorf("the person's agent reads any chat:\n%s", out)
	}
}

func TestReasonOfOneMount(t *testing.T) {
	j := open(t)
	j.Add(Entry{Level: Error, Source: SourceApp, Event: "preview.request", URL: "/t/__hl/app-sdk.js", Text: "blocked", App: "x.app", Trace: "v-old"})
	if r := j.Reason("x.app", "", "v-new", time.Now().Add(-time.Minute)); r != "" {
		t.Errorf("an earlier mount's cause: %q", r)
	}
	if r := j.Reason("x.app", "", "v-old", time.Now().Add(-time.Minute)); !strings.Contains(r, "app-sdk.js") {
		t.Errorf("its own mount: %q", r)
	}
}

// «ResizeObserver loop» the browser reports as an exception is not a failure, and the journal keeps none.
func TestBenignResizeObserver(t *testing.T) {
	for _, e := range []Entry{
		{Event: "exception", Text: "ResizeObserver loop completed with undelivered notifications."},
		{Event: "exception", Text: "ResizeObserver loop limit exceeded"},
	} {
		if !Benign(e) {
			t.Fatalf("benign: %+v", e)
		}
	}
	if Benign(Entry{Event: "exception", Text: "TypeError: x is undefined"}) {
		t.Fatal("a real exception is kept")
	}
}
