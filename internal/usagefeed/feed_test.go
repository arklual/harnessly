package usagefeed

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hubproto"
	"github.com/ly-solutions/harnessly/internal/usageoutbox"
	"github.com/ly-solutions/harnessly/internal/usagereport"
)

var t0 = time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

type rig struct {
	t      *testing.T
	dir    string
	claude string // the CLI's projects folder
	clock  *clock
	box    *usageoutbox.Outbox
	feed   *Feed
	ident  func(string) Identity
	// cap and source change what open builds: the limits, and what the feed reads through.
	maxPending int
	maxBytes   int64
	wrap       func(Source) Source
}

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{t: t, dir: t.TempDir(), clock: &clock{t: t0}}
	r.claude = filepath.Join(r.dir, "claude", "projects")
	if err := os.MkdirAll(r.claude, 0o755); err != nil {
		t.Fatal(err)
	}
	r.open()
	return r
}

// open (re)opens the outbox and the feed, as a restart of the agent does.
func (r *rig) open() {
	r.t.Helper()
	if r.box != nil {
		r.box.Close()
	}
	box, err := usageoutbox.Open(filepath.Join(r.dir, "usage-outbox"), r.clock.now)
	if err != nil {
		r.t.Fatal(err)
	}
	r.t.Cleanup(func() { box.Close() })
	r.box = box
	col := usagereport.NewCollector(usagereport.Config{
		ClaudeRoot: r.claude, CodexRoot: filepath.Join(r.dir, "codex"), CacheFile: filepath.Join(r.dir, "facts.json"),
		Projects: &usagereport.Projects{}, Now: r.clock.now, Workers: 1,
	})
	var src Source = col
	if r.wrap != nil {
		src = r.wrap(col)
	}
	feed, err := New(Config{Outbox: box, Source: src, StatePath: StatePath(filepath.Join(r.dir, "usage-outbox")), Identity: r.ident, Now: r.clock.now,
		MaxPending: r.maxPending, MaxBytes: r.maxBytes})
	if err != nil {
		r.t.Fatal(err)
	}
	r.feed = feed
}

func (r *rig) pass() int {
	r.t.Helper()
	n, err := r.feed.Pass(context.Background())
	if err != nil {
		r.t.Fatal(err)
	}
	return n
}

func (r *rig) all() []hubproto.UsageRecord {
	r.t.Helper()
	res, err := r.box.Read(r.box.Epoch(), 0, hubproto.UsageReadMax)
	if err != nil {
		r.t.Fatal(err)
	}
	return res.Records
}

func kinds(recs []hubproto.UsageRecord) (calls []*hubproto.UsageCallFact, sessions []*hubproto.UsageSessionFact) {
	for _, r := range recs {
		switch r.Kind {
		case hubproto.UsageCall:
			calls = append(calls, r.Call)
		case hubproto.UsageSession:
			sessions = append(sessions, r.Session)
		}
	}
	return
}

type msg struct {
	req, id string
	at      time.Time
	out     int
}

// assistant is a line of a Claude Code transcript with the usage of one model call.
func assistant(session string, m msg) string {
	return fmt.Sprintf(`{"type":"assistant","sessionId":%q,"requestId":%q,"timestamp":%q,"cwd":"/work/app","message":{"id":%q,"model":"claude-opus-4-6","usage":{"input_tokens":10,"output_tokens":%d,"cache_read_input_tokens":200,"cache_creation_input_tokens":30,"cache_creation":{"ephemeral_5m_input_tokens":20,"ephemeral_1h_input_tokens":10}}}}`,
		session, m.req, m.at.UTC().Format("2006-01-02T15:04:05.000Z"), m.id, m.out)
}

func prompt(session, text string, at time.Time) string {
	return fmt.Sprintf(`{"type":"user","sessionId":%q,"uuid":"u-%d","timestamp":%q,"cwd":"/work/app","message":{"role":"user","content":%q}}`,
		session, at.UnixNano(), at.UTC().Format("2006-01-02T15:04:05.000Z"), text)
}

// write puts lines in a transcript and dates it.
func (r *rig) write(rel string, mtime time.Time, lines ...string) string {
	r.t.Helper()
	p := filepath.Join(r.claude, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		r.t.Fatal(err)
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		r.t.Fatal(err)
	}
	for _, l := range lines {
		_, _ = f.WriteString(l + "\n")
	}
	f.Close()
	if err := os.Chtimes(p, mtime, mtime); err != nil {
		r.t.Fatal(err)
	}
	return p
}

const promptText = "PROMPT-TEXT-THAT-MUST-STAY-ON-THE-MACHINE"

func standard(r *rig) {
	base := t0.Add(-3 * time.Hour)
	r.write("-work-app/sess-1.jsonl", base.Add(time.Hour),
		prompt("sess-1", promptText, base),
		// One answer streamed in two chunks: the larger output count is the call.
		assistant("sess-1", msg{"req_a", "msg_a", base.Add(time.Second), 5}),
		assistant("sess-1", msg{"req_a", "msg_a", base.Add(2 * time.Second), 50}),
		assistant("sess-1", msg{"req_b", "msg_b", base.Add(time.Minute), 70}),
	)
	r.write("-work-app/sess-1/subagents/agent-ab12.jsonl", base.Add(time.Hour),
		assistant("sess-1", msg{"req_c", "msg_c", base.Add(2 * time.Minute), 9}),
	)
}

func TestFeedWritesCallsAndSessionsOnce(t *testing.T) {
	r := newRig(t)
	standard(r)
	if n := r.pass(); n != 5 {
		t.Fatalf("wrote %d records", n)
	}
	calls, sessions := kinds(r.all())
	if len(calls) != 3 || len(sessions) != 2 {
		t.Fatalf("%d calls, %d sessions", len(calls), len(sessions))
	}
	base := t0.Add(-3 * time.Hour)
	byKey := map[string]*hubproto.UsageCallFact{}
	for _, c := range calls {
		byKey[c.MsgKey] = c
	}
	a := byKey["req_a:msg_a"]
	if a == nil || a.Provider != "claude" || a.SessionKey != "sess-1" || a.Model != "claude-opus-4-6" || a.Ts != base.Add(time.Second).UnixMilli() ||
		a.Input != 10 || a.Output != 50 || a.CacheRead != 200 || a.CacheWrite5m != 20 || a.CacheWrite1h != 10 || a.BaseURLHost != "" {
		t.Fatalf("%+v", a)
	}
	c := byKey["req_c:msg_c"]
	if c == nil || c.SessionKey != "sess-1/agent-ab12" {
		t.Fatalf("%+v", c)
	}
	var main, sub *hubproto.UsageSessionFact
	for _, s := range sessions {
		if s.ParentKey == "" {
			main = s
		} else {
			sub = s
		}
	}
	if main == nil || sub == nil || main.SessionKey != "sess-1" || sub.SessionKey != "sess-1/agent-ab12" || sub.ParentKey != "sess-1" {
		t.Fatalf("%+v %+v", main, sub)
	}
	if main.Provider != "claude" || main.Origin != "live" || main.Attribution != "guessed" || main.Subject != "" || main.AccountRef != "" ||
		main.AuthMode != "" || main.BaseURLHost != "" || main.StartedAt != base.UnixMilli() || main.EndedAt == nil || *main.EndedAt != base.Add(time.Minute).UnixMilli() {
		t.Fatalf("%+v", main)
	}

	// Nothing changed: nothing is written, also after a restart.
	if n := r.pass(); n != 0 {
		t.Fatalf("second pass wrote %d", n)
	}
	r.open()
	if n := r.pass(); n != 0 {
		t.Fatalf("pass after a restart wrote %d", n)
	}
	if got := r.box.Last(); got != 5 {
		t.Fatalf("last %d", got)
	}

	// One more call: that call and its session, no more.
	r.write("-work-app/sess-1.jsonl", t0.Add(-time.Hour), assistant("sess-1", msg{"req_d", "msg_d", t0.Add(-2 * time.Hour), 3}))
	if n := r.pass(); n != 2 {
		t.Fatalf("wrote %d", n)
	}
	recs := r.all()
	if calls, sessions := kinds(recs[5:]); len(calls) != 1 || calls[0].MsgKey != "req_d:msg_d" || len(sessions) != 1 || sessions[0].SessionKey != "sess-1" {
		t.Fatalf("%+v", recs[5:])
	}
}

// A forked or resumed transcript copies the history with the original ids: those calls are written once.
func TestFeedWritesACopiedCallOnce(t *testing.T) {
	r := newRig(t)
	base := t0.Add(-3 * time.Hour)
	line := assistant("sess-1", msg{"req_a", "msg_a", base, 5})
	r.write("-work-app/sess-1.jsonl", base.Add(time.Hour), line)
	r.write("-work-app/sess-2.jsonl", base.Add(time.Hour), strings.Replace(line, "sess-1", "sess-2", 1))
	r.pass()
	if calls, sessions := kinds(r.all()); len(calls) != 1 || len(sessions) != 2 {
		t.Fatalf("%d calls %d sessions", len(calls), len(sessions))
	}
	r.open()
	r.write("-work-app/sess-2.jsonl", base.Add(2*time.Hour), assistant("sess-2", msg{"req_e", "msg_e", base.Add(time.Minute), 5}))
	r.pass()
	if calls, _ := kinds(r.all()); len(calls) != 2 {
		t.Fatalf("%d calls", len(calls))
	}
}

// The last call of a transcript that is being written may still be growing, and a record never changes.
func TestFeedWaitsForTheCallStillBeingWritten(t *testing.T) {
	r := newRig(t)
	at := t0.Add(-5 * time.Second)
	r.write("-work-app/sess-1.jsonl", t0.Add(-2*time.Second),
		assistant("sess-1", msg{"req_a", "msg_a", t0.Add(-time.Minute), 5}),
		assistant("sess-1", msg{"req_b", "msg_b", at, 1}),
	)
	r.pass()
	if calls, _ := kinds(r.all()); len(calls) != 1 || calls[0].MsgKey != "req_a:msg_a" {
		t.Fatalf("%+v", calls)
	}
	r.write("-work-app/sess-1.jsonl", t0.Add(-time.Second), assistant("sess-1", msg{"req_b", "msg_b", at.Add(time.Second), 400}))
	r.clock.t = t0.Add(time.Minute)
	r.pass()
	calls, _ := kinds(r.all())
	if len(calls) != 2 || calls[1].MsgKey != "req_b:msg_b" || calls[1].Output != 400 {
		t.Fatalf("%+v", calls)
	}
}

// A session that goes on after it had an end is written again without it.
func TestFeedSessionEndsAndGoesOn(t *testing.T) {
	r := newRig(t)
	r.write("-work-app/sess-1.jsonl", t0.Add(-time.Hour), assistant("sess-1", msg{"req_a", "msg_a", t0.Add(-10 * time.Minute), 5}))
	r.pass()
	_, sessions := kinds(r.all())
	if len(sessions) != 1 || sessions[0].EndedAt != nil {
		t.Fatalf("%+v", sessions)
	}
	r.clock.t = t0.Add(time.Hour)
	if n := r.pass(); n != 1 {
		t.Fatalf("wrote %d", n)
	}
	_, sessions = kinds(r.all())
	if len(sessions) != 2 || sessions[1].EndedAt == nil || *sessions[1].EndedAt != t0.Add(-10*time.Minute).UnixMilli() {
		t.Fatalf("%+v", sessions)
	}
	if n := r.pass(); n != 0 {
		t.Fatalf("wrote %d", n)
	}
}

func TestFeedStartsOverWithANewEpoch(t *testing.T) {
	r := newRig(t)
	standard(r)
	r.pass()
	old := r.box.Epoch()
	if err := os.RemoveAll(filepath.Join(r.dir, "usage-outbox")); err != nil {
		t.Fatal(err)
	}
	r.open()
	if r.box.Epoch() == old {
		t.Fatal("the epoch did not change")
	}
	// The hub reads the new epoch from the start, so the backfill is written again.
	if n := r.pass(); n != 5 {
		t.Fatalf("wrote %d", n)
	}
}

// No key and no word of a conversation reaches the outbox.
func TestNothingSecretOrSaidReachesTheOutbox(t *testing.T) {
	const fakeKey = "sk-ant-FAKE-0123456789abcdefghijklmnop"
	const fakeToken = "sk-ant-FAKE-TOKEN-zyxwvutsrqponmlkjihg"
	t.Setenv("ANTHROPIC_API_KEY", fakeKey)
	t.Setenv("ANTHROPIC_AUTH_TOKEN", fakeToken)
	t.Setenv("ANTHROPIC_BASE_URL", "https://api.kimi.com/coding/")
	home := t.TempDir()
	r := &rig{t: t, dir: t.TempDir(), clock: &clock{t: t0}}
	r.claude = filepath.Join(r.dir, "claude", "projects")
	r.ident = Local{Home: home}.Identity
	r.open()
	// The first pass sees the account; what happens after it is the account's.
	r.pass()
	r.clock.t = t0.Add(2 * time.Hour)
	at := t0.Add(time.Hour)
	r.write("-work-app/sess-1.jsonl", t0.Add(90*time.Minute),
		prompt("sess-1", promptText+" "+fakeKey+" "+fakeToken, at),
		assistant("sess-1", msg{"req_a", "msg_a", at.Add(time.Second), 5}),
	)
	if n := r.pass(); n != 2 {
		t.Fatalf("wrote %d", n)
	}
	calls, sessions := kinds(r.all())
	want := KeyRef(fakeKey)
	if !strings.HasPrefix(want, "key:sha256:") || len(want) != len("key:sha256:")+16 {
		t.Fatal(want)
	}
	if len(calls) != 1 || calls[0].BaseURLHost != "api.kimi.com" || len(sessions) != 1 ||
		sessions[0].AccountRef != want || sessions[0].AuthMode != "api_key" || sessions[0].BaseURLHost != "api.kimi.com" {
		t.Fatalf("%+v %+v", calls, sessions[0])
	}
	files, err := os.ReadDir(filepath.Join(r.dir, "usage-outbox"))
	if err != nil || len(files) == 0 {
		t.Fatal(err)
	}
	for _, f := range files {
		raw, err := os.ReadFile(filepath.Join(r.dir, "usage-outbox", f.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, bad := range []string{fakeKey, fakeToken, "sk-ant", "FAKE", promptText, "PROMPT-TEXT", "/work/app", "work-app"} {
			if strings.Contains(string(raw), bad) {
				t.Errorf("%s holds %q", f.Name(), bad)
			}
		}
	}
	// Whatever a hub could read has no field for text: the records hold only the fields of the contract.
	res, _ := r.box.Read(r.box.Epoch(), 0, 100)
	raw, _ := json.Marshal(res)
	if strings.Contains(string(raw), promptText) || strings.Contains(string(raw), fakeKey) {
		t.Fatalf("%s", raw)
	}
}

func TestLocalIdentity(t *testing.T) {
	home := t.TempDir()
	write := func(rel, text string) {
		p := filepath.Join(home, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	none := func(string, string) string { return "" }
	l := Local{Home: home, Getenv: none}
	if id := l.Identity("claude"); id != (Identity{}) {
		t.Fatalf("%+v", id)
	}
	write(".claude.json", `{"oauthAccount":{"accountUuid":"7f9c1e52-0000-4000-8000-000000000001","emailAddress":"a@b.c"}}`)
	if id := l.Identity("claude"); id.AccountRef != "claude:7f9c1e52-0000-4000-8000-000000000001" || id.AuthMode != "subscription" || id.BaseURLHost != "" {
		t.Fatalf("%+v", id)
	}
	// A key in the CLI's own settings wins over the sign-in, and the endpoint is named by host only.
	write(".claude/settings.json", `{"env":{"ANTHROPIC_AUTH_TOKEN":"tok-FAKE","ANTHROPIC_BASE_URL":"https://api.z.ai/api/anthropic"}}`)
	id := l.Identity("claude")
	if id.AccountRef != KeyRef("tok-FAKE") || id.AuthMode != "api_key" || id.BaseURLHost != "api.z.ai" {
		t.Fatalf("%+v", id)
	}
	if strings.Contains(id.AccountRef, "FAKE") {
		t.Fatal("the key is in the reference")
	}

	write(".codex/auth.json", `{"auth_mode":"chatgpt","OPENAI_API_KEY":null,"tokens":{"id_token":"x","access_token":"y","account_id":"acc-123"}}`)
	if id := l.Identity("codex"); id.AccountRef != "codex:acc-123" || id.AuthMode != "subscription" {
		t.Fatalf("%+v", id)
	}
	write(".codex/auth.json", `{"auth_mode":"apikey","OPENAI_API_KEY":"sk-FAKE-openai"}`)
	if id := l.Identity("codex"); id.AccountRef != KeyRef("sk-FAKE-openai") || id.AuthMode != "api_key" {
		t.Fatalf("%+v", id)
	}
	if l.Identity("gemini") != (Identity{}) {
		t.Fatal("gemini has no identity")
	}
	for in, want := range map[string]string{"": "", "https://api.anthropic.com": "", "api.kimi.com": "api.kimi.com", "https://OpenRouter.ai:443/api": "openrouter.ai", "http://%zz": ""} {
		if got := HostOf(in); got != want {
			t.Errorf("%q: %q want %q", in, got, want)
		}
	}
}

type countingSource struct {
	Source
	refreshes *int
}

func (c countingSource) Refresh() { *c.refreshes++; c.Source.Refresh() }

// Reaching the cap pauses the feed without a scan and without dropping anything; the hub's ack lets it go on from there.
func TestFeedPausesAtTheCapAndResumesAfterAck(t *testing.T) {
	r := newRig(t)
	r.maxPending = 3
	refreshes := 0
	r.wrap = func(s Source) Source { return countingSource{s, &refreshes} }
	r.open()
	base := t0.Add(-3 * time.Hour)
	for i := range 6 {
		name := fmt.Sprintf("s-%d", i)
		r.write("-work-app/"+name+".jsonl", base.Add(time.Hour), assistant(name, msg{fmt.Sprintf("req_%d", i), fmt.Sprintf("msg_%d", i), base.Add(time.Duration(i) * time.Minute), 5}))
	}
	if n := r.pass(); n != 3 {
		t.Fatalf("wrote %d records before the cap", n)
	}
	calls, sessions := kinds(r.all())
	if len(calls) != 3 || len(sessions) != 0 {
		t.Fatalf("%d calls %d sessions", len(calls), len(sessions))
	}
	// Paused: no scan, nothing written, however often it is asked.
	scans := refreshes
	for range 3 {
		if n := r.pass(); n != 0 {
			t.Fatalf("paused feed wrote %d", n)
		}
	}
	if refreshes != scans || r.box.Pending() != 3 {
		t.Fatalf("scans %d (was %d), pending %d", refreshes, scans, r.box.Pending())
	}
	// A restart keeps the pause: the records are still unacknowledged.
	r.open()
	if n := r.pass(); n != 0 {
		t.Fatalf("wrote %d after a restart", n)
	}
	// The hub reads and acknowledges; the feed goes on where it stopped, until all six files are in.
	got := r.all()
	for round := 0; round < 10; round++ {
		if err := r.box.Ack(r.box.Epoch(), r.box.Last()); err != nil {
			t.Fatal(err)
		}
		if r.pass() == 0 {
			break
		}
		got = append(got, r.all()...)
	}
	calls, sessions = kinds(got)
	keys := map[string]int{}
	for _, c := range calls {
		keys[c.MsgKey]++
	}
	sess := map[string]int{}
	for _, s := range sessions {
		sess[s.SessionKey]++
	}
	if len(calls) != 6 || len(keys) != 6 || len(sessions) != 6 || len(sess) != 6 {
		t.Fatalf("%d calls (%d distinct), %d sessions (%d distinct)", len(calls), len(keys), len(sessions), len(sess))
	}
}

// A byte cap pauses just as the count does.
func TestFeedPausesAtTheByteCap(t *testing.T) {
	r := newRig(t)
	r.maxBytes = 1
	r.open()
	standard(r)
	// The first pass finds an empty outbox and writes at least the first file's calls before it sees the cap.
	r.pass()
	last := r.box.Last()
	if last == 0 || r.pass() != 0 || r.box.Last() != last {
		t.Fatalf("last %d then %d", last, r.box.Last())
	}
}
