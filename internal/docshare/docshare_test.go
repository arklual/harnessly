package docshare

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
)

type fakeAgent struct {
	calls   []map[string]any
	status  int
	reply   string
	version int
	err     error
}

func (f *fakeAgent) call(route string, body any) (int, []byte, error) {
	if f.err != nil {
		return 0, nil, f.err
	}
	raw, _ := json.Marshal(body)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	m["route"] = route
	f.calls = append(f.calls, m)
	if f.status != 0 && f.status != 200 {
		return f.status, []byte(f.reply), nil
	}
	f.version++
	out, _ := json.Marshal(map[string]any{"status": 201, "body": map[string]any{"slug": "plan-x1", "version": f.version}})
	return 200, out, nil
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func setup(t *testing.T) (*Service, *fakeAgent, *clock, string) {
	t.Helper()
	dir := t.TempDir()
	real, _ := filepath.EvalSymlinks(dir)
	f := &fakeAgent{}
	c := &clock{t: time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)}
	s := New(Config{DataDir: real, Now: c.now, Call: f.call})
	return s, f, c, filepath.Join(real, "plan.md")
}

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPublishMakesAPrivateDocumentWithItsSource(t *testing.T) {
	s, f, _, path := setup(t)
	write(t, path, "intro\n\n# Backlog of features\n\ntext\n")
	st, fail := s.Publish(Request{Path: path, Conversation: "tab-7"})
	if fail != nil {
		t.Fatal(fail)
	}
	if !st.Shared || st.Slug != "plan-x1" || !st.Linked || st.Version != 1 {
		t.Errorf("state: %+v", st)
	}
	if len(f.calls) != 1 {
		t.Fatalf("calls: %d", len(f.calls))
	}
	c := f.calls[0]
	src, _ := c["source"].(map[string]any)
	if c["route"] != "/docs/publish" || c["visibility"] != "private" || c["title"] != "Backlog of features" || c["format"] != "md" || src["path"] != path || src["conversation"] != "tab-7" {
		t.Errorf("call: %v", c)
	}
	if _, has := c["slug"]; has {
		t.Error("a first publish names no slug")
	}
	// The same file again is the same document, without a second version.
	if again, fail := s.Publish(Request{Path: path}); fail != nil || again.Slug != "plan-x1" || len(f.calls) != 1 {
		t.Errorf("again: %+v %v calls %d", again, fail, len(f.calls))
	}
}

func TestTitleFallsBackToTheFileName(t *testing.T) {
	if got := titleOf("/w/docs/features-backlog.md", "no heading", "md"); got != "Features backlog" {
		t.Errorf("title: %q", got)
	}
	if got := titleOf("/w/a.html", "<h1>x</h1>", "html"); got != "A" {
		t.Errorf("html title: %q", got)
	}
}

func TestOnlyMarkdownAndHTMLAreShared(t *testing.T) {
	s, f, _, path := setup(t)
	txt := filepath.Join(filepath.Dir(path), "notes.txt")
	write(t, txt, "x")
	if _, fail := s.Publish(Request{Path: txt}); fail == nil || fail.Status != 415 || len(f.calls) != 0 {
		t.Errorf("txt: %v", fail)
	}
	if _, fail := s.Publish(Request{Path: filepath.Join(filepath.Dir(path), "missing.md")}); fail == nil || fail.Status != 404 {
		t.Errorf("missing: %v", fail)
	}
	bin := filepath.Join(filepath.Dir(path), "bin.md")
	write(t, bin, "a\x00b")
	if _, fail := s.Publish(Request{Path: bin}); fail == nil || fail.Status != 415 {
		t.Errorf("binary: %v", fail)
	}
}

func TestSecretRefusalNamesTheLine(t *testing.T) {
	s, f, _, path := setup(t)
	write(t, path, "# Plan\n")
	f.status, f.reply = 422, `{"error":"secret","kind":"github","line":12}`
	_, fail := s.Publish(Request{Path: path})
	if fail == nil || fail.Status != 422 || fail.Line != 12 || fail.Kind != "github" {
		t.Fatalf("failure: %+v", fail)
	}
	if st := s.State(path); st.Shared {
		t.Errorf("a refused file must not become a source: %+v", st)
	}
}

func TestNoAgentIsA503(t *testing.T) {
	s, f, _, path := setup(t)
	write(t, path, "# Plan\n")
	f.err = agent.ErrNoAgent
	if _, fail := s.Publish(Request{Path: path}); fail == nil || fail.Status != 503 {
		t.Errorf("failure: %+v", fail)
	}
}

func TestASaveBecomesANewVersionNotMoreOftenThanTheDebounce(t *testing.T) {
	s, f, c, path := setup(t)
	write(t, path, "# Plan\n\nv1\n")
	if _, fail := s.Publish(Request{Path: path}); fail != nil {
		t.Fatal(fail)
	}
	// Edited at once: nothing goes out before the debounce, and the state says a version waits.
	write(t, path, "# Plan\n\nv2\n")
	c.t = c.t.Add(10 * time.Second)
	s.Tick()
	if len(f.calls) != 1 {
		t.Fatalf("published inside the debounce: %d calls", len(f.calls))
	}
	if st := s.State(path); !st.Pending || st.Version != 1 {
		t.Errorf("pending: %+v", st)
	}
	// Edited again, then the debounce passes: one version with the last text.
	write(t, path, "# Plan\n\nv3\n")
	c.t = c.t.Add(25 * time.Second)
	s.Tick()
	if len(f.calls) != 2 {
		t.Fatalf("calls: %d", len(f.calls))
	}
	second := f.calls[1]
	if second["slug"] != "plan-x1" || second["content"] != "# Plan\n\nv3\n" || second["note"] == nil {
		t.Errorf("second call: %v", second)
	}
	if st := s.State(path); st.Pending || st.Version != 2 {
		t.Errorf("after: %+v", st)
	}
	// Unchanged: no version, however long it has been.
	c.t = c.t.Add(10 * time.Minute)
	s.Tick()
	if len(f.calls) != 2 {
		t.Errorf("unchanged file published: %d", len(f.calls))
	}
}

func TestUnlinkFreezesTheDocumentAndSharingAgainContinuesIt(t *testing.T) {
	s, f, c, path := setup(t)
	write(t, path, "# Plan\n\nv1\n")
	if _, fail := s.Publish(Request{Path: path}); fail != nil {
		t.Fatal(fail)
	}
	if st, ok := s.Unlink(path); !ok || st.Linked {
		t.Fatalf("unlink: %+v %v", st, ok)
	}
	write(t, path, "# Plan\n\nv2\n")
	c.t = c.t.Add(time.Hour)
	s.Tick()
	if len(f.calls) != 1 {
		t.Errorf("an unlinked file was published: %d", len(f.calls))
	}
	if _, ok := s.Unlink(filepath.Join(filepath.Dir(path), "other.md")); ok {
		t.Error("unlink of an unknown file")
	}
	if st, fail := s.Publish(Request{Path: path}); fail != nil || st.Slug != "plan-x1" || !st.Linked {
		t.Fatalf("share again: %+v %v", st, fail)
	}
	if len(f.calls) != 2 || f.calls[1]["slug"] != "plan-x1" {
		t.Errorf("calls: %v", f.calls)
	}
}

func TestASecretInASavedFileIsNotRetriedUntilTheFileChanges(t *testing.T) {
	s, f, c, path := setup(t)
	write(t, path, "# Plan\n")
	if _, fail := s.Publish(Request{Path: path}); fail != nil {
		t.Fatal(fail)
	}
	write(t, path, "# Plan\n\nleak\n")
	f.status, f.reply = 422, `{"error":"secret","kind":"aws","line":3}`
	c.t = c.t.Add(time.Minute)
	s.Tick()
	s.Tick()
	if len(f.calls) != 2 {
		t.Fatalf("a refused content is tried once: %d calls", len(f.calls))
	}
	if st := s.State(path); st.RefusedLine != 3 || st.Version != 1 {
		t.Errorf("state: %+v", st)
	}
	f.status = 0
	write(t, path, "# Plan\n\nclean\n")
	s.Tick()
	if st := s.State(path); st.RefusedLine != 0 || st.Version != 2 {
		t.Errorf("after the fix: %+v", st)
	}
}

func TestSourcesSurviveARestart(t *testing.T) {
	s, f, c, path := setup(t)
	write(t, path, "# Plan\n")
	if _, fail := s.Publish(Request{Path: path}); fail != nil {
		t.Fatal(fail)
	}
	again := New(Config{DataDir: s.cfg.DataDir, Now: c.now, Call: f.call})
	if st := again.State(path); !st.Shared || !st.Linked || st.Slug != "plan-x1" {
		t.Errorf("after restart: %+v", st)
	}
	info, err := os.Stat(filepath.Join(s.cfg.DataDir, FileName))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("registry: %v %v", info, err)
	}
}
