package panel

import (
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

var targets = Targets{
	StandDomain: ".preview.example.org",
	PortalHost:  "a.example.org",
	ReaderHost:  "ai.example.org",
	GitLabHosts: []string{"gitlab.example.org", "gitlab.com"},
	TaskKey:     regexp.MustCompile(`^(TCK|EXP|OPS)-\d{1,6}$`),
}

func obj(pairs ...any) *jsvalue.Object { return jsvalue.NewObject(pairs...) }

func parse(t *testing.T, input any) (Command, bool) {
	t.Helper()
	c, err := targets.Parse(input)
	return c, err == ""
}

func open(tab string, target any) *jsvalue.Object {
	o := obj("kind", "open", "tab", tab)
	if target != nil {
		o.Set("target", target)
	}
	return o
}

func wantOpen(t *testing.T, input *jsvalue.Object, tab, target string) {
	t.Helper()
	c, ok := parse(t, input)
	if !ok || c != (Command{Kind: "open", Tab: tab, Target: target}) {
		t.Errorf("%s → %+v %v", jsvalue.Stringify(input, ""), c, ok)
	}
}

func refused(t *testing.T, input any) {
	t.Helper()
	if c, ok := parse(t, input); ok {
		t.Errorf("accepted %v → %+v", input, c)
	}
}

// Ported from the previous runtime's panel command tests (its domains replaced by example ones).
func TestOpenEveryKind(t *testing.T) {
	for _, tab := range TabKinds {
		wantOpen(t, open(tab, nil), tab, "")
	}
}

func TestBrowserPorts(t *testing.T) {
	wantOpen(t, open("browser", "4400"), "browser", "localhost:4400")
	wantOpen(t, open("browser", "localhost:19090/events?x=1"), "browser", "localhost:19090/events?x=1")
	wantOpen(t, open("browser", "http://127.0.0.1:8097/"), "browser", "localhost:8097")
	wantOpen(t, open("browser", "4400/путь"), "browser", "localhost:4400/%D0%BF%D1%83%D1%82%D1%8C")
	wantOpen(t, open("browser", "https://feat-1.preview.example.org/x?y=1"), "browser", "https://feat-1.preview.example.org/x?y=1")
	for _, target := range []string{"example.org", "https://evil.example/x", "0", "70000", "localhost", "javascript:alert(1)", "4400 && rm -rf /",
		"4400/\u001b[201~", "4400/a\nb", "https://ai-1.preview.example.org", "https://auth.preview.example.org", "4400/%zz"} {
		refused(t, open("browser", target))
	}
	refused(t, open("browser", float64(4400)))
	refused(t, open("browser", "4400/"+strings.Repeat("a", MaxArg)))
}

func TestPaths(t *testing.T) {
	wantOpen(t, open("files", "  /work/report.md "), "files", "/work/report.md")
	wantOpen(t, open("changes", "src/lib/a.ts"), "changes", "src/lib/a.ts")
	for _, target := range []string{"../../etc/passwd", "/work/../etc/passwd", "a\u001b[201~b", "a\rb", "a\u0000b", strings.Repeat("x", MaxArg+1)} {
		refused(t, open("files", target))
	}
	wantOpen(t, open("files", "   "), "files", "")
}

func TestDocuments(t *testing.T) {
	wantOpen(t, open("artifacts", "https://a.example.org/a/plan-abc"), "artifacts", "/agents/docs/plan-abc")
	wantOpen(t, open("artifacts", "https://ai.example.org/agents/docs/plan-abc"), "artifacts", "/agents/docs/plan-abc")
	wantOpen(t, open("artifacts", " https://a.example.org/b/board-1 "), "artifacts", "/agents/docs/board-1")
	for _, target := range []string{"https://evil.example/a/plan", "/agents/docs/plan/../../api", "//evil.example/agents/docs/plan",
		"https://a.example.org/b/api", "https://a.example.org/a/x\u001b[201~", "javascript:alert(1)"} {
		refused(t, open("artifacts", target))
	}
	board := "https://a.example.org/b/board-1?k=Key_1"
	wantOpen(t, open("artifacts", board+"&embed=1"), "artifacts", board)
	wantOpen(t, open("artifacts", board), "artifacts", board)
	wantOpen(t, open("artifacts", board+"&at=snap-2"), "artifacts", board+"&at=snap-2")
	refused(t, open("artifacts", "https://evil.example/b/board-1?k=Key_1"))
}

func TestReparse(t *testing.T) {
	for _, input := range []*jsvalue.Object{
		open("artifacts", "https://a.example.org/a/plan-abc"), open("gitlab", "https://gitlab.com/a/b/-/merge_requests/1"),
		open("browser", "4400/events"), open("files", "/work/report.md"), open("terminal", nil),
		obj("kind", "show"), obj("kind", "hide"), obj("kind", "status"),
	} {
		once, ok := parse(t, input)
		if !ok {
			t.Fatalf("%s", jsvalue.Stringify(input, ""))
		}
		if again, ok := parse(t, once.JSON()); !ok || again != once {
			t.Errorf("%+v → %+v", once, again)
		}
	}
}

func TestGitLab(t *testing.T) {
	mr := "https://gitlab.example.org/team/app/-/merge_requests/147"
	commit := "https://gitlab.example.org/team/app/-/commit/35f679ec"
	archived := "https://gitlab.com/old/team/app/-/merge_requests/147"
	for _, target := range []string{mr, commit, archived} {
		wantOpen(t, open("gitlab", target), "gitlab", target)
	}
	for _, target := range []string{"x", "https://evil.example/a/b/-/merge_requests/1", "https://gitlab.com/a/b", "http://gitlab.com/a/b/-/merge_requests/1",
		"http://gitlab.example.org/a/b/-/merge_requests/1", "https://gitlab.example.org.evil.example/a/b/-/merge_requests/1", mr + "\u001b[201~"} {
		refused(t, open("gitlab", target))
	}
}

func TestTasks(t *testing.T) {
	for _, key := range []string{"TCK-2147", "EXP-14", "OPS-9"} {
		wantOpen(t, open("task", " "+key+" "), "task", key)
	}
	for _, target := range []any{"ABC-1", "tck-1", "TCK-", "TCK-1234567", "TCK-1/../x", "TCK-1\u001b[201~", "https://tracker.example.org/issue/TCK-1",
		strings.Repeat("x", MaxArg+1), float64(5)} {
		refused(t, open("task", target))
	}
	wantOpen(t, open("task", nil), "task", "")
}

func TestKindsAndUnknown(t *testing.T) {
	for _, tab := range []string{"terminal", "tools", "overview", "side-chat", "gallery", "plan"} {
		refused(t, open(tab, "x"))
	}
	for _, tab := range []any{"island", "nope", "__proto__", "constructor", "", float64(5), nil} {
		refused(t, obj("kind", "open", "tab", tab))
	}
	for _, kind := range []string{"show", "hide", "status"} {
		if c, ok := parse(t, obj("kind", kind)); !ok || c.Kind != kind {
			t.Errorf("%s", kind)
		}
	}
	for _, body := range []any{obj("kind", "island", "id", "files"), obj("kind", "mode"), obj("kind", "exec", "cmd", "ls"), nil, "status", []any{}, float64(5)} {
		refused(t, body)
	}
	if c, _ := parse(t, obj("kind", "status", "tabId", "x", "extra", 1)); string(jsvalue.Stringify(c.JSON(), "")) != `{"kind":"status"}` {
		t.Fatal("unknown keys passed on")
	}
	if _, err := targets.Parse(obj("kind", "exec")); !strings.Contains(err, "Неизвестная команда") {
		t.Fatal(err)
	}
}

func TestParseResult(t *testing.T) {
	if r := ParseResult(obj("ok", true, "message", "Открыто: localhost:4400")); r == nil || !r.OK || r.Message != "Открыто: localhost:4400" || r.State != nil {
		t.Fatal(r)
	}
	r := ParseResult(obj("ok", false, "message", "bad\u001b[2J"+strings.Repeat("x", 2000)))
	if r == nil || r.OK || !strings.HasPrefix(r.Message, "bad␛[2J") || len([]rune(r.Message)) > MaxMessage {
		t.Fatal(r)
	}
	for _, body := range []any{nil, obj("ok", "yes", "message", "x"), obj("ok", true), obj("ok", true, "message", float64(5))} {
		if ParseResult(body) != nil {
			t.Errorf("%v", body)
		}
	}
	r = ParseResult(obj("ok", true, "message", "s", "state", obj("visible", true, "open", []any{"browser", "bogus", "files"}, "active", "browser", "extra", 1)))
	if string(jsvalue.Stringify(r.State, "")) != `{"visible":true,"open":["browser","files"],"active":"browser"}` {
		t.Fatal(string(jsvalue.Stringify(r.State, "")))
	}
	r = ParseResult(obj("ok", true, "message", "x", "state", obj("visible", "yes", "open", "no", "active", "nope")))
	if string(jsvalue.Stringify(r.State, "")) != `{"visible":null,"open":[],"active":null}` {
		t.Fatal(string(jsvalue.Stringify(r.State, "")))
	}
}

func TestRateDecision(t *testing.T) {
	var stamps []int64
	for i := int64(0); i < 12; i++ {
		ok, next := RateDecision(stamps, 1000+i, 12, 60_000)
		if !ok {
			t.Fatal(i)
		}
		stamps = next
	}
	if ok, next := RateDecision(stamps, 1100, 12, 60_000); ok || len(next) != 12 {
		t.Fatal("13th")
	}
	if ok, next := RateDecision(stamps, 62_000, 12, 60_000); !ok || len(next) != 1 || next[0] != 62_000 {
		t.Fatal(next)
	}
}

type harness struct {
	mu        sync.Mutex
	published []*jsvalue.Object
	b         *Bridge
}

func newHarness(subscribers int) *harness {
	h := &harness{}
	h.b = NewBridge(func(e *jsvalue.Object) { h.mu.Lock(); h.published = append(h.published, e); h.mu.Unlock() }, func() int { return subscribers })
	return h
}

func (h *harness) waitPublished(t *testing.T, n int) *jsvalue.Object {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		h.mu.Lock()
		if len(h.published) >= n {
			e := h.published[n-1]
			h.mu.Unlock()
			return e
		}
		h.mu.Unlock()
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("not published %d", n)
	return nil
}

var status = Command{Kind: "status"}

// Ported from the previous runtime's panel bridge tests.
func TestBridge(t *testing.T) {
	h := newHarness(1)
	done := make(chan Outcome)
	go func() { done <- h.b.Dispatch("tab-1", status, 2*time.Second) }()
	e := h.waitPublished(t, 1)
	if e.String("type") != "panel-command" || e.String("tabId") != "tab-1" || string(jsvalue.Stringify(e.Object("command"), "")) != `{"kind":"status"}` {
		t.Fatal(string(jsvalue.Stringify(e, "")))
	}
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(e.String("id")) {
		t.Fatal(e.String("id"))
	}
	// One command per tab at a time; another tab is not held up.
	if o := h.b.Dispatch("tab-1", status, time.Second); o.Kind != "busy" {
		t.Fatal(o)
	}
	if !h.b.Answer(e.String("id"), Result{OK: true, Message: "first"}) || h.b.Answer(e.String("id"), Result{Message: "second"}) {
		t.Fatal("first answer only")
	}
	if o := <-done; o.Kind != "answered" || o.Result.Message != "first" {
		t.Fatal(o)
	}
	if h.b.Answer("00000000-0000-4000-8000-000000000000", Result{}) {
		t.Fatal("unknown id")
	}
	if o := newHarness(0).b.Dispatch("tab-1", status, time.Second); o.Kind != "no-ui" {
		t.Fatal(o)
	}
	h2 := newHarness(1)
	if o := h2.b.Dispatch("tab-1", status, 20*time.Millisecond); o.Kind != "timeout" {
		t.Fatal(o)
	}
	if h2.b.Answer(h2.published[0].String("id"), Result{OK: true}) {
		t.Fatal("a timed-out command is forgotten")
	}
}

func TestBridgeRateLimit(t *testing.T) {
	h := newHarness(1)
	now := time.UnixMilli(1000)
	h.b.Now = func() time.Time { return now }
	for i := 0; i < 12; i++ {
		done := make(chan Outcome)
		go func() { done <- h.b.Dispatch("tab-1", status, 2*time.Second) }()
		e := h.waitPublished(t, i+1)
		h.b.Answer(e.String("id"), Result{OK: true, Message: "ok"})
		if o := <-done; o.Kind != "answered" {
			t.Fatal(o)
		}
	}
	if o := h.b.Dispatch("tab-1", status, time.Second); o.Kind != "rate-limited" || len(h.published) != 12 {
		t.Fatal(o)
	}
}

func TestURI(t *testing.T) {
	for in, want := range map[string]string{"/a b": "/a%20b", "/%D0%BF": "/%D0%BF", "/%3F": "/%253F", "/%25": "/%25", "/x?y=1#z": "/x?y=1#z"} {
		d, ok := decodeURI(in)
		if !ok || encodeURI(d) != want {
			t.Errorf("%s → %s", in, encodeURI(d))
		}
	}
	for _, bad := range []string{"%", "%zz", "%80", "%E2%82"} {
		if _, ok := decodeURI(bad); ok {
			t.Errorf("%s decoded", bad)
		}
	}
}
