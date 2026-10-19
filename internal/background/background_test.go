package background

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func asData(v any) any {
	raw, _ := json.Marshal(v)
	var out any
	_ = json.Unmarshal(raw, &out)
	return out
}

// testdata/ts-golden.json: the previous runtime's own parser and footer rules run under node on generated transcripts.
// TS_GOLDEN points the test at a larger set made the same way.
func TestMatchesTheOldRuntime(t *testing.T) {
	path := os.Getenv("TS_GOLDEN")
	if path == "" {
		path = "testdata/ts-golden.json"
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var g struct {
		Parse []struct {
			Lines []string        `json:"lines"`
			Now   float64         `json:"now"`
			Tasks json.RawMessage `json:"tasks"`
		} `json:"parse"`
		Footer []struct {
			Pane   string  `json:"pane"`
			Counts *Counts `json:"counts"`
		} `json:"footer"`
		Reconcile []struct {
			Tasks  []Task          `json:"tasks"`
			Counts *Counts         `json:"counts"`
			Result json.RawMessage `json:"result"`
		} `json:"reconcile"`
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	for i, c := range g.Parse {
		var want any
		_ = json.Unmarshal(c.Tasks, &want)
		if got := asData(ParseTasks(c.Lines, c.Now)); !reflect.DeepEqual(got, want) {
			t.Errorf("parse #%d:\n got  %v\n want %v\n lines %q", i, got, want, c.Lines)
		}
	}
	for _, c := range g.Footer {
		if got := ParseFooter(c.Pane); !reflect.DeepEqual(got, c.Counts) {
			t.Errorf("footer %q: got %v want %v", c.Pane, got, c.Counts)
		}
	}
	for i, c := range g.Reconcile {
		var want any
		_ = json.Unmarshal(c.Result, &want)
		if got := asData(Reconcile(c.Tasks, c.Counts)); !reflect.DeepEqual(got, want) {
			t.Errorf("reconcile #%d: got %v want %v", i, got, want)
		}
	}
}

func (c *Counts) UnmarshalJSON(b []byte) error {
	var v struct{ Shell, Monitor int }
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*c = Counts{Shell: v.Shell, Monitor: v.Monitor}
	return nil
}

func line(content string, ts string) string {
	return `{"type":"assistant","timestamp":"` + ts + `","message":{"content":[` + content + `]}}`
}

func result(id, text, ts string) string {
	raw, _ := json.Marshal(text)
	return `{"type":"user","timestamp":"` + ts + `","message":{"content":[{"type":"tool_result","tool_use_id":"` + id + `","content":` + string(raw) + `}]}}`
}

func ids(tasks []Task) []string {
	out := []string{}
	for _, t := range tasks {
		out = append(out, t.ID)
	}
	return out
}

func TestReaderReadsOnlyAppendedLines(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "session.jsonl")
	ts := time.Now().UTC().Format(time.RFC3339)
	r := &Reader{}
	write := func(s string, appendTo bool) {
		flag := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
		if appendTo {
			flag = os.O_CREATE | os.O_WRONLY | os.O_APPEND
		}
		f, _ := os.OpenFile(file, flag, 0o600)
		f.WriteString(s)
		f.Close()
	}
	write(line(`{"type":"tool_use","id":"t1","name":"Agent","input":{"description":"Research"}}`, ts)+"\n"+result("t1", "Async agent launched successfully. agentId: a1aaaaaaa (internal ID)", ts)+"\n", false)
	if got := ids(r.Read(file)); !reflect.DeepEqual(got, []string{"a1aaaaaaa"}) {
		t.Fatal(got)
	}
	second := result("t2", "Async agent launched. agentId: a2bbbbbbb", ts)
	write(line(`{"type":"tool_use","id":"t2","name":"Agent","input":{"description":"Review"}}`, ts)+"\n"+second[:40], true)
	if got := ids(r.Read(file)); !reflect.DeepEqual(got, []string{"a1aaaaaaa"}) {
		t.Fatalf("a line still being written was read: %v", got)
	}
	write(second[40:]+"\n", true)
	if got := ids(r.Read(file)); !reflect.DeepEqual(got, []string{"a1aaaaaaa", "a2bbbbbbb"}) {
		t.Fatal(got)
	}
	write(`{"type":"attachment","attachment":{"prompt":"<task-notification><task-id>a1aaaaaaa</task-id><status>completed</status></task-notification>"}}`+"\n", true)
	var wg sync.WaitGroup
	results := make([][]string, 2)
	for i := range results {
		wg.Add(1)
		go func(i int) { defer wg.Done(); results[i] = ids(r.Read(file)) }(i)
	}
	wg.Wait()
	if !reflect.DeepEqual(results[0], []string{"a2bbbbbbb"}) || !reflect.DeepEqual(results[0], results[1]) {
		t.Fatalf("concurrent reads: %v", results)
	}
	write(line(`{"type":"text","text":"fresh"}`, ts)+"\n", false)
	if got := r.Read(file); len(got) != 0 {
		t.Fatalf("a rewritten transcript kept old tasks: %v", got)
	}
}

func TestReaderStartsALongTranscriptAtAWholeLine(t *testing.T) {
	file := filepath.Join(t.TempDir(), "long.jsonl")
	ts := time.Now().UTC().Format(time.RFC3339)
	body := line(`{"type":"text","text":"`+strings.Repeat("x", 7<<20)+`"}`, ts) + "\n" +
		line(`{"type":"tool_use","id":"t1","name":"Agent","input":{"description":"Research"}}`, ts) + "\n" +
		result("t1", "Async agent launched successfully. agentId: a1aaaaaaa (internal ID)", ts) + "\n"
	_ = os.WriteFile(file, []byte(body), 0o600)
	if got := ids((&Reader{}).Read(file)); !reflect.DeepEqual(got, []string{"a1aaaaaaa"}) {
		t.Fatal(got)
	}
}

func TestReaderDropsShellsWhoseOutputIsGone(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "-srv-app", "0b8f5c2e.jsonl")
	_ = os.MkdirAll(filepath.Dir(file), 0o755)
	ts := time.Now().UTC().Format(time.RFC3339)
	_ = os.WriteFile(file, []byte(
		line(`{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"a","run_in_background":true}}`, ts)+"\n"+
			result("t1", "Command running in background with ID: b1aaaaaaa.", ts)+"\n"+
			line(`{"type":"tool_use","id":"t2","name":"Bash","input":{"command":"b","run_in_background":true}}`, ts)+"\n"+
			result("t2", "Command running in background with ID: b2bbbbbbb.", ts)+"\n"), 0o600)
	tasks := filepath.Join(dir, "tmp", "claude-7", "-srv-app", "0b8f5c2e", "tasks")
	_ = os.MkdirAll(tasks, 0o755)
	_ = os.WriteFile(filepath.Join(tasks, "b2bbbbbbb.output"), nil, 0o600)
	if got := ids((&Reader{TempDir: filepath.Join(dir, "tmp"), UID: 7}).Read(file)); !reflect.DeepEqual(got, []string{"b2bbbbbbb"}) {
		t.Fatal(got)
	}
}

type feedProbe struct {
	mu        sync.Mutex
	tasks     []Task
	pane      string
	reads     int
	panes     int
	published [][]Task
}

func (p *feedProbe) deps(reconcile func([]Task, string, bool) []Task) Deps {
	return Deps{
		ReadTasks: func(string) []Task { p.mu.Lock(); defer p.mu.Unlock(); p.reads++; return p.tasks },
		ReadPane:  func(string) (string, bool) { p.mu.Lock(); defer p.mu.Unlock(); p.panes++; return p.pane, p.pane != "" },
		Reconcile: reconcile,
		Publish:   func(_ string, ts []Task) { p.mu.Lock(); defer p.mu.Unlock(); p.published = append(p.published, ts) },
	}
}

func (p *feedProbe) get() (int, int, [][]Task) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.reads, p.panes, append([][]Task(nil), p.published...)
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); !cond(); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
	}
}

var fast = Timings{MinInterval: 100 * time.Millisecond, PaneRefresh: 400 * time.Millisecond, Recheck: 150 * time.Millisecond}

func shell(id string) Task { return Task{ID: id, Kind: Shell, Label: id, StartedAt: 1} }

func TestFeedPublishesOnlyChangesAndThrottlesABurst(t *testing.T) {
	p := &feedProbe{tasks: []Task{shell("s1")}}
	f := NewFeed(p.deps(func(ts []Task, _ string, _ bool) []Task { return ts }), fast, nil)
	defer f.Stop()
	src := Source{TmuxSession: "s", Transcript: "/x.jsonl", Provider: "claude"}
	f.Touch("tab-a", src, true)
	waitFor(t, func() bool { _, _, pub := p.get(); return len(pub) == 1 })
	for i := 0; i < 20; i++ {
		f.Touch("tab-a", src, false)
	}
	time.Sleep(50 * time.Millisecond)
	if reads, _, _ := p.get(); reads != 1 {
		t.Fatalf("read %d times inside the interval", reads)
	}
	waitFor(t, func() bool { reads, _, _ := p.get(); return reads == 2 })
	if _, panes, pub := p.get(); panes != 1 || len(pub) != 1 {
		t.Fatalf("a transcript burst with no change captured the footer (%d) or published (%d)", panes, len(pub))
	}
	if snap := f.Snapshot(); len(snap["tab-a"]) != 1 {
		t.Fatalf("snapshot %v", snap)
	}
	p.mu.Lock()
	p.tasks = []Task{}
	p.mu.Unlock()
	f.Touch("tab-a", src, false)
	waitFor(t, func() bool { _, _, pub := p.get(); return len(pub) == 2 && len(pub[1]) == 0 })
	if len(f.Snapshot()) != 0 {
		t.Fatal("an idle tab stayed in the snapshot")
	}
}

func TestFeedRechecksTabsWithWorkAndNoticesTheFooter(t *testing.T) {
	p := &feedProbe{tasks: []Task{shell("s1"), shell("s2")}, pane: "⏵⏵ bypass permissions on · 2 shells"}
	f := NewFeed(p.deps(ReconcileWithFooter), fast, nil)
	defer f.Stop()
	f.Touch("tab-a", Source{TmuxSession: "s", Transcript: "/x.jsonl"}, true)
	waitFor(t, func() bool { _, _, pub := p.get(); return len(pub) == 1 && len(pub[0]) == 2 })
	p.mu.Lock()
	p.pane = "⏵⏵ bypass permissions on · 1 shell"
	p.mu.Unlock()
	waitFor(t, func() bool { _, _, pub := p.get(); return len(pub) == 2 && len(pub[1]) == 1 })
	p.mu.Lock()
	p.tasks, p.pane = []Task{}, ""
	p.mu.Unlock()
	waitFor(t, func() bool { _, _, pub := p.get(); return len(pub) == 3 && len(pub[2]) == 0 })
	settled, _, _ := p.get()
	time.Sleep(4 * fast.Recheck)
	if reads, _, _ := p.get(); reads != settled {
		t.Fatalf("an idle tab was read again: %d → %d", settled, reads)
	}
}

func TestFeedClearsARemovedTab(t *testing.T) {
	p := &feedProbe{tasks: []Task{shell("s1")}}
	f := NewFeed(p.deps(func(ts []Task, _ string, _ bool) []Task { return ts }), fast, nil)
	defer f.Stop()
	f.Touch("tab-a", Source{Transcript: "/x.jsonl"}, true)
	waitFor(t, func() bool { _, _, pub := p.get(); return len(pub) == 1 })
	f.Remove("tab-a")
	if _, _, pub := p.get(); len(pub) != 2 || len(pub[1]) != 0 || len(f.Snapshot()) != 0 {
		t.Fatalf("%v", pub)
	}
}

func TestCodexTabsReadNoTranscript(t *testing.T) {
	p := &feedProbe{tasks: []Task{shell("s1")}}
	f := NewFeed(p.deps(func(ts []Task, _ string, _ bool) []Task { return ts }), fast, nil)
	defer f.Stop()
	f.Touch("tab-c", Source{Transcript: "/x.jsonl", Provider: "codex"}, true)
	waitFor(t, func() bool { _, panes, _ := p.get(); return panes == 1 })
	if reads, _, pub := p.get(); reads != 0 || len(pub) != 0 {
		t.Fatalf("reads %d published %v", reads, pub)
	}
}
