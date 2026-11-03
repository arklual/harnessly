package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/convo"
	"github.com/ly-solutions/harnessly/internal/sources"
	"github.com/ly-solutions/harnessly/internal/wire"
)

type fakeMadeLib struct {
	visible map[string]madeDesign
	tied    []string
}

func (f fakeMadeLib) Visible() map[string]madeDesign  { return f.visible }
func (f fakeMadeLib) Tied(wire.Conversation) []string { return f.tied }

func madeFixture(t *testing.T, lines ...string) (v2Made, wire.Conversation) {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, ".claude", "projects", "p")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "s.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := wire.Conversation{ID: "tab-1", TranscriptPath: path}
	rt := &sources.Route{Links: &sources.Links{DocsHost: "docs.example.com"}, Home: home}
	m := v2Made{record: func(id string) (wire.Conversation, bool) { return rec, id == rec.ID }, read: rt.ReadBudget}
	return m, rec
}

func pairLines(n int, name, input, result string) []string {
	ts := "2026-10-12T10:00:0" + string(rune('0'+n))
	return []string{
		`{"type":"assistant","timestamp":"` + ts + `.000Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"t` + string(rune('0'+n)) + `","name":"` + name + `","input":` + input + `}]}}`,
		`{"type":"user","timestamp":"` + ts + `.500Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t` + string(rune('0'+n)) + `","content":"` + result + `"}]}}`,
	}
}

func TestMadeAnswersEmptyAndUnknown(t *testing.T) {
	m, _ := madeFixture(t, `{"type":"user","timestamp":"2026-10-12T10:00:00.000Z","message":{"role":"user","content":"hi"}}`)
	got, err := m.Made(context.Background(), "tab-1")
	if err != nil || !got.Complete || got.Items == nil || len(got.Items) != 0 {
		t.Fatalf("empty: %+v %v", got, err)
	}
	if _, err := m.Made(context.Background(), "gone"); !errors.Is(err, convo.ErrNotFound) {
		t.Fatalf("unknown: %v", err)
	}
	if err := got.Validate(); err != nil {
		t.Fatal(err)
	}
}

// Documents and designs come together, newest first; a design the person may not see is dropped, one the transcript
// does not date is dated by its last change, and the answer is out of contract for none of it.
func TestMadeJoinsDocsAndVisibleDesigns(t *testing.T) {
	var lines []string
	lines = append(lines, pairLines(1, "Bash", `{"command":"harnessly open https://docs.example.com/a/spec"}`, "opened")...)
	lines = append(lines, pairLines(3, "Write", `{"file_path":"/w/design-artifacts/touched/project/index.html","content":"x"}`, "ok")...)
	m, _ := madeFixture(t, lines...)
	cover := "/c.png"
	m.designs = func() madeDesigns {
		return fakeMadeLib{
			visible: map[string]madeDesign{
				"touched": {Title: "Touched", Kind: wire.DesignSite, UpdatedAt: 1},
				"tied":    {Title: "Tied", Kind: wire.DesignSlides, Cover: &cover, UpdatedAt: 1900000000000},
			},
			tied: []string{"tied", "hidden"},
		}
	}
	got, err := m.Made(context.Background(), "tab-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := got.Validate(); err != nil {
		t.Fatal(err)
	}
	var refs []string
	for _, it := range got.Items {
		refs = append(refs, string(it.Kind)+":"+it.Ref)
	}
	want := "design:tied design:touched doc:https://docs.example.com/a/spec"
	if strings.Join(refs, " ") != want {
		t.Fatalf("items %v, want %s", refs, want)
	}
	if got.Items[0].Title != "Tied" || got.Items[0].Cover == nil || *got.Items[0].DesignKind != wire.DesignSlides {
		t.Fatalf("tied design: %+v", got.Items[0])
	}
}

func TestMadeIsCapped(t *testing.T) {
	var lines []string
	for i := 0; i < 5; i++ {
		lines = append(lines, pairLines(i, "Bash", `{"command":"x"}`, "ok")...)
	}
	m, _ := madeFixture(t, lines...)
	lib := fakeMadeLib{visible: map[string]madeDesign{}}
	for i := 0; i < wire.MadeMax+10; i++ {
		id := "d" + string(rune('a'+i%26)) + string(rune('a'+i/26))
		lib.visible[id] = madeDesign{Title: id, Kind: wire.DesignSite, UpdatedAt: wire.Ms(i)}
		lib.tied = append(lib.tied, id)
	}
	m.designs = func() madeDesigns { return lib }
	got, err := m.Made(context.Background(), "tab-1")
	if err != nil || len(got.Items) != wire.MadeMax {
		t.Fatalf("cap: %d %v", len(got.Items), err)
	}
	if got.Items[0].At < got.Items[len(got.Items)-1].At {
		t.Fatal("not newest first")
	}
}

func madeTasksFixture(t *testing.T, lines ...string) v2Made {
	t.Helper()
	m, rec := madeFixture(t, lines...)
	home := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(rec.TranscriptPath))))
	rt := &sources.Route{Home: home, Links: &sources.Links{
		TrackerURL: "https://tracker.example.com", TrackerHosts: []string{"tracker.example.com"}, IssueKeys: []string{"HRN"},
	}}
	m.read = rt.ReadBudget
	return m
}

func TestMadeListsTrackerTasksOnlyForAPageThatAsksAndGroupsThemByTheTracker(t *testing.T) {
	lines := append(
		pairLines(1, "mcp__plugin_tracker_youtrack__create_issue", `{"project":"HRN","summary":"Epic"}`, `{\"id\":\"HRN-7\",\"url\":\"https://tracker.example.com/issue/HRN-7\"}`),
		pairLines(2, "mcp__plugin_tracker_youtrack__update_issue", `{"issueId":"HRN-8","summary":"x"}`, "ok")...,
	)
	lines = append(lines, pairLines(3, "mcp__plugin_tracker_youtrack__search_issues", `{"query":"HRN-9"}`, "none")...)
	m := madeTasksFixture(t, lines...)
	m.task = func(_ context.Context, key string) (string, wire.MadeTaskInfo, bool) {
		if key == "HRN-7" {
			return "Epic", wire.MadeTaskInfo{Key: key, Type: "Epic", Children: []wire.MadeTaskRef{{Key: "HRN-8", Title: "Sub"}}}, true
		}
		return "", wire.MadeTaskInfo{}, false
	}

	old, err := m.Made(context.Background(), "tab-1")
	if err != nil || len(old.Items) != 0 {
		t.Fatalf("a page that did not ask: %+v %v", old, err)
	}

	ctx := wire.WithMadeTasks(context.Background())
	got, err := m.Made(ctx, "tab-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 2 {
		t.Fatalf("tasks: %+v", got.Items)
	}
	if err := got.Validate(); err != nil {
		t.Fatal(err)
	}
	by := map[string]wire.MadeItem{}
	for _, it := range got.Items {
		if it.Kind != wire.MadeTask {
			t.Fatalf("kind: %+v", it)
		}
		by[it.Ref] = it
	}
	epic := by["https://tracker.example.com/issue/HRN-7"]
	if epic.Title != "Epic" || epic.Task == nil || epic.Task.Type != "Epic" || epic.Op != wire.OutputCreated {
		t.Fatalf("epic: %+v", epic)
	}
	if epic.Task.Children[0].URL != "https://tracker.example.com/issue/HRN-8" {
		t.Fatalf("child url: %+v", epic.Task.Children)
	}
	if lone := by["https://tracker.example.com/issue/HRN-8"]; lone.Title != "" || lone.Task != nil {
		t.Fatalf("a task the tracker did not answer keeps only its key: %+v", lone)
	}
}
