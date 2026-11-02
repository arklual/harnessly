package budget

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

func TestMain(m *testing.M) {
	os.Setenv("RUNTIME_LOCALE", "en")
	os.Exit(m.Run())
}

func entry(user, status string, pct float64) hubproto.BudgetEntry {
	return hubproto.BudgetEntry{User: user, Status: status, Pct: pct, RuleID: "r-" + status, ResetsAt: time.Date(2026, 11, 1, 9, 30, 0, 0, time.Local).UnixMilli(),
		Reason: "Team limit for October is used up"}
}

func TestApplies(t *testing.T) {
	e := hubproto.BudgetEntry{User: "Ann@X.ru", Account: "claude:u1", ModelPattern: "Opus"}
	for _, c := range []struct {
		name string
		e    hubproto.BudgetEntry
		s    Subject
		want bool
	}{
		{"all three", e, Subject{"ann@x.ru", "claude:u1", "claude-opus-4-1"}, true},
		{"other person", e, Subject{"bob@x.ru", "claude:u1", "claude-opus-4-1"}, false},
		{"other account", e, Subject{"ann@x.ru", "claude:u2", "claude-opus-4-1"}, false},
		{"unknown account, entry names one", e, Subject{"ann@x.ru", "", "claude-opus-4-1"}, false},
		{"other model", e, Subject{"ann@x.ru", "claude:u1", "claude-sonnet-4-5"}, false},
		{"unknown model, entry names one", e, Subject{"ann@x.ru", "claude:u1", ""}, false},
		{"any account", hubproto.BudgetEntry{User: "ann@x.ru"}, Subject{"ann@x.ru", "codex:7", "gpt-5"}, true},
		{"any account, unknown", hubproto.BudgetEntry{User: "ann@x.ru"}, Subject{User: "ann@x.ru"}, true},
		{"no person", hubproto.BudgetEntry{User: "ann@x.ru"}, Subject{}, false},
	} {
		if got := Applies(c.e, c.s); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

func TestDecide(t *testing.T) {
	ann := Subject{User: "ann@x.ru", Model: "claude-opus-4-1"}
	warn, stop := entry("ann@x.ru", hubproto.BudgetWarn, 90), entry("ann@x.ru", hubproto.BudgetStop, 100)
	other := entry("bob@x.ru", hubproto.BudgetStop, 100)
	forbid := entry("ann@x.ru", hubproto.BudgetStop, 10)
	forbid.Forbid, forbid.ModelPattern = true, "opus"
	set := func(mode string, es ...hubproto.BudgetEntry) hubproto.BudgetSet {
		return hubproto.BudgetSet{Version: 1, Mode: mode, Entries: es}
	}
	for _, c := range []struct {
		name      string
		set       hubproto.BudgetSet
		status    string
		block     bool
		forbidden bool
	}{
		{"mode off", set("off", stop), "", false, false},
		{"unknown mode", set("", stop), "", false, false},
		{"no list", set("stop"), "", false, false},
		{"someone else's", set("stop", other), "", false, false},
		{"ok entries are not listed", set("stop", entry("ann@x.ru", hubproto.BudgetOK, 10)), "", false, false},
		{"warn in warn mode", set("warn", warn), "warn", false, false},
		{"warn in stop mode", set("stop", warn), "warn", false, false},
		{"stop in warn mode is a notice", set("warn", stop), "stop", false, false},
		{"stop in stop mode blocks", set("stop", stop), "stop", true, false},
		{"stop beats warn", set("stop", warn, stop), "stop", true, false},
		{"stop beats warn, whatever the order", set("stop", stop, warn), "stop", true, false},
		{"a forbidden model is first", set("stop", stop, forbid), "stop", true, true},
	} {
		d := Decide(c.set, ann)
		if d.Entry.Status != c.status || d.Block != c.block || d.Entry.Forbid != c.forbidden || d.None() != (c.status == "") {
			t.Errorf("%s: %+v", c.name, d)
		}
	}
	// The same rule in the same period is one notice however far the share moved; a new period or status is another.
	a, b := warn, warn
	b.Pct = 95
	next := warn
	next.ResetsAt += 1000
	if Decide(set("warn", a), ann).Key() != Decide(set("warn", b), ann).Key() || Decide(set("warn", a), ann).Key() == Decide(set("warn", next), ann).Key() {
		t.Error("the notice key")
	}
}

func TestOnlyUser(t *testing.T) {
	one := hubproto.BudgetSet{Entries: []hubproto.BudgetEntry{{User: "Ann@x.ru"}, {User: "ann@x.ru"}}}
	two := hubproto.BudgetSet{Entries: []hubproto.BudgetEntry{{User: "ann@x.ru"}, {User: "bob@x.ru"}}}
	if OnlyUser(one) != "ann@x.ru" || OnlyUser(two) != "" || OnlyUser(hubproto.BudgetSet{}) != "" {
		t.Errorf("%q %q", OnlyUser(one), OnlyUser(two))
	}
}

func TestSaveLoadAndReader(t *testing.T) {
	dir := t.TempDir()
	path := Path(dir)
	if set, err := Load(path); err != nil || set.Version != 0 {
		t.Fatalf("a missing file: %+v %v", set, err)
	}
	r := &Reader{Path: path}
	if set := r.Get(); set.Version != 0 {
		t.Fatalf("%+v", set)
	}
	want := hubproto.BudgetSet{Version: 3, Mode: "stop", IssuedAt: 5, Entries: []hubproto.BudgetEntry{entry("ann@x.ru", hubproto.BudgetStop, 100)}}
	if err := Save(path, want); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("%v %v", err, fi)
	}
	if got := r.Get(); got.Version != 3 || len(got.Entries) != 1 {
		t.Fatalf("%+v", got)
	}
	// A newer list is picked up; a damaged file keeps the list read before; a removed file is an empty list.
	want.Version, want.Mode = 4, "warn"
	if err := Save(path, want); err != nil {
		t.Fatal(err)
	}
	if got := r.Get(); got.Version != 4 || got.Mode != "warn" {
		t.Fatalf("%+v", got)
	}
	if err := os.WriteFile(path, []byte(`{"version": 9, "mo`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := r.Get(); got.Version != 4 {
		t.Fatalf("a damaged file: %+v", got)
	}
	_ = os.Remove(path)
	if got := r.Get(); got.Version != 0 || got.Mode != "" {
		t.Fatalf("a removed file: %+v", got)
	}
}

func gateWith(t *testing.T, mode string, es ...hubproto.BudgetEntry) *Gate {
	t.Helper()
	path := Path(t.TempDir())
	if err := Save(path, hubproto.BudgetSet{Version: 1, Mode: mode, Entries: es}); err != nil {
		t.Fatal(err)
	}
	return &Gate{Reader: &Reader{Path: path}, Owner: "ann@x.ru", Account: func(string) string { return "claude:u1" }}
}

func reply(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	if raw == nil {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("%s: %v", raw, err)
	}
	return m
}

func TestReplyInStopMode(t *testing.T) {
	g := gateWith(t, "stop", entry("ann@x.ru", hubproto.BudgetStop, 100))
	p := reply(t, g.Reply("claude", "UserPromptSubmit", "s1", "claude-opus-4-1"))
	text, _ := p["reason"].(string)
	if p["decision"] != "block" || !strings.HasPrefix(text, "Team limit for October is used up. Resets 01.11 09:30") {
		t.Fatalf("%v", p)
	}
	// Every prompt is refused again; a tool call ends the turn.
	if reply(t, g.Reply("claude", "UserPromptSubmit", "s1", "")) == nil {
		t.Error("the second prompt went through")
	}
	tool := reply(t, g.Reply("codex", "PreToolUse", "s1", ""))
	if tool["continue"] != false || tool["stopReason"] != text {
		t.Fatalf("%v", tool)
	}
	// Any other hook only tells.
	if n := reply(t, g.Reply("claude", "Notification", "s1", "")); n["systemMessage"] != text || n["decision"] != nil {
		t.Fatalf("%v", n)
	}
	// Another person's chat is left alone.
	g.Owner = "bob@x.ru"
	if g.Reply("claude", "UserPromptSubmit", "s2", "") != nil || g.Admit("claude", "") != "" {
		t.Error("someone else was stopped")
	}
}

func TestReplyInWarnMode(t *testing.T) {
	for name, status := range map[string]string{"a warning": hubproto.BudgetWarn, "a stop": hubproto.BudgetStop} {
		g := gateWith(t, "warn", entry("ann@x.ru", status, 95))
		n := reply(t, g.Reply("claude", "UserPromptSubmit", "s1", ""))
		if n["systemMessage"] == nil || n["decision"] != nil || n["continue"] != nil {
			t.Fatalf("%s: %v", name, n)
		}
		// Once per chat and entry; another chat hears it too; tools are never stopped.
		if g.Reply("claude", "UserPromptSubmit", "s1", "") != nil || g.Reply("claude", "PreToolUse", "s1", "") != nil {
			t.Errorf("%s: told twice", name)
		}
		if reply(t, g.Reply("claude", "UserPromptSubmit", "s2", "")) == nil {
			t.Errorf("%s: the other chat not told", name)
		}
		if g.Admit("claude", "") != "" {
			t.Errorf("%s: a new chat refused in warn mode", name)
		}
	}
}

func TestWarnAgainWhenTheEntryChanges(t *testing.T) {
	path := Path(t.TempDir())
	g := &Gate{Reader: &Reader{Path: path}, Owner: "ann@x.ru"}
	save := func(version int64, e hubproto.BudgetEntry) {
		if err := Save(path, hubproto.BudgetSet{Version: version, Mode: "stop", Entries: []hubproto.BudgetEntry{e}}); err != nil {
			t.Fatal(err)
		}
	}
	w := entry("ann@x.ru", hubproto.BudgetWarn, 90)
	save(1, w)
	if g.Reply("claude", "UserPromptSubmit", "s", "") == nil {
		t.Fatal("no first notice")
	}
	w.Pct = 93
	save(2, w)
	if g.Reply("claude", "UserPromptSubmit", "s", "") != nil {
		t.Error("the share moved, the rule did not: no new notice")
	}
	w.ResetsAt += 86400000
	save(3, w)
	if g.Reply("claude", "UserPromptSubmit", "s", "") == nil {
		t.Error("a new period is a new notice")
	}
}

func TestAdmit(t *testing.T) {
	stop := entry("ann@x.ru", hubproto.BudgetStop, 100)
	if r := gateWith(t, "stop", stop).Admit("claude", "claude-opus-4-1"); !strings.Contains(r, "Team limit") || !strings.Contains(r, "Resets 01.11 09:30") {
		t.Errorf("%q", r)
	}
	for _, mode := range []string{"warn", "off", ""} {
		if r := gateWith(t, mode, stop).Admit("claude", ""); r != "" {
			t.Errorf("mode %q: %q", mode, r)
		}
	}
	if r := gateWith(t, "stop", entry("ann@x.ru", hubproto.BudgetWarn, 90)).Admit("claude", ""); r != "" {
		t.Errorf("a warning refused a chat: %q", r)
	}
	// A limit of one model stops that model only; an unknown model passes it.
	opus := stop
	opus.ModelPattern = "opus"
	g := gateWith(t, "stop", opus)
	if g.Admit("claude", "claude-opus-4-1") == "" || g.Admit("claude", "claude-sonnet-4-5") != "" || g.Admit("claude", "") != "" {
		t.Error("the model pattern")
	}
	// No file, or no gate: nothing.
	if (&Gate{Reader: &Reader{Path: filepath.Join(t.TempDir(), "none")}, Owner: "ann@x.ru"}).Admit("claude", "") != "" || (*Gate)(nil).Admit("claude", "") != "" {
		t.Error("no list, no refusal")
	}
}

func TestThePersonOfAChat(t *testing.T) {
	stop := entry("ann@x.ru", hubproto.BudgetStop, 100)
	// Without a known person the one the list names is taken, but not when it names several.
	g := gateWith(t, "stop", stop)
	g.Owner = ""
	if g.Admit("claude", "") == "" {
		t.Error("the only person in the list")
	}
	g = gateWith(t, "stop", stop, entry("bob@x.ru", hubproto.BudgetStop, 100))
	g.Owner = ""
	if g.Admit("claude", "") != "" {
		t.Error("two people, none known")
	}
	g.UserOf = func(session string) string { return map[string]string{"s-bob": "bob@x.ru"}[session] }
	if g.Reply("claude", "UserPromptSubmit", "s-bob", "") == nil || g.Reply("claude", "UserPromptSubmit", "s-none", "") != nil {
		t.Error("UserOf")
	}
}

func TestAccount(t *testing.T) {
	dir := t.TempDir()
	write := func(name, text string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	codexHome := filepath.Join(dir, "codex")
	_ = os.MkdirAll(codexHome, 0o700)
	_ = os.WriteFile(filepath.Join(codexHome, "auth.json"), []byte(`{"tokens":{"access_token":"t","account_id":"acc-7"}}`), 0o600)
	a := Accounts{ClaudeJSON: write("c.json", `{"oauthAccount":{"accountUuid":"u-1","emailAddress":"a@x.ru"}}`), CodexHome: codexHome}
	if a.Of("claude") != "claude:u-1" || a.Of("codex") != "codex:acc-7" || a.Of("grok") != "" {
		t.Errorf("%q %q", a.Of("claude"), a.Of("codex"))
	}
	none := Accounts{ClaudeJSON: write("n.json", `{}`), CodexHome: filepath.Join(dir, "no")}
	if none.Of("claude") != "" || none.Of("codex") != "" {
		t.Error("no sign-in, no account")
	}
}
