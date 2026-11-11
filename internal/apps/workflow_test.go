package apps

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A prototype of a mobile screen: drawn as a draft, reviewed, approved, then built, tested and shipped.
func prototypeWorkflow() *Workflow {
	return &Workflow{
		Statuses: []WorkflowStatus{
			{ID: "draft", Title: "Черновик", Edit: true},
			{ID: "review", Title: "На ревью", Tone: "info"},
			{ID: "approved", Title: "Согласован", Tone: "success"},
			{ID: "dev", Title: "В разработке"},
		},
		Transitions: []WorkflowTransition{
			{From: "draft", To: "review", Title: "Отправить на ревью"},
			{From: "review", To: "approved", Title: "Согласовать", Role: "manage"},
			{From: "review", To: "draft", Title: "Вернуть", Role: "manage"},
			{From: "approved", To: "dev", Title: "В разработку"},
		},
	}
}

func TestWorkflowFollowsTheDocument(t *testing.T) {
	w := prototypeWorkflow()
	now := time.UnixMilli(1000)
	var r WorkflowRecord
	if !w.Follow(&r, "h1", now) || r.Current() != (WorkflowVersion{N: 1, Hash: "h1", Status: "draft"}) {
		t.Fatalf("a new document starts at version 1 in the first status: %+v", r.Current())
	}
	// A draft follows its edits: the same version, the new hash.
	w.Follow(&r, "h2", now)
	if cur := r.Current(); cur.N != 1 || cur.Hash != "h2" {
		t.Fatalf("a draft's version follows its edits: %+v", cur)
	}
	if err := w.Move(&r, "review", "Анна", "edit", 1, "h2", "", now); err != nil {
		t.Fatal(err)
	}
	// A change of a version on review starts the next one, a draft; the one on review keeps its status.
	if !w.Follow(&r, "h3", now) {
		t.Fatal("a change of a version on review is a change of the record")
	}
	if len(r.Versions) != 2 || r.Versions[0].Status != "review" || r.Current() != (WorkflowVersion{N: 2, Hash: "h3", Status: "draft"}) {
		t.Fatalf("versions: %+v", r.Versions)
	}
	if last := r.Journal[len(r.Journal)-1]; last.Version != 2 || last.To != "draft" {
		t.Fatalf("the journal tells the new version: %+v", last)
	}
	if w.Follow(&r, "h3", now) {
		t.Fatal("nothing changed: the record stays")
	}
}

func TestWorkflowMoveChecksTheMoveTheRoleAndTheVersion(t *testing.T) {
	w := prototypeWorkflow()
	now := time.UnixMilli(1000)
	var r WorkflowRecord
	w.Follow(&r, "h1", now)
	if err := w.Move(&r, "approved", "Анна", "owner", 0, "", "", now); !errors.Is(err, ErrWorkflowMove) {
		t.Fatalf("no draft → approved move: %v", err)
	}
	if err := w.Move(&r, "review", "Гость", "comment", 0, "", "", now); !errors.Is(err, ErrWorkflowRole) {
		t.Fatalf("a commenter does not send to review (edit by default): %v", err)
	}
	if err := w.Move(&r, "review", "Анна", "edit", 1, "old", "", now); !errors.Is(err, ErrWorkflowStale) {
		t.Fatalf("a move of a version the person did not see: %v", err)
	}
	if err := w.Move(&r, "review", "Анна", "edit", 1, "h1", "готово", now); err != nil {
		t.Fatal(err)
	}
	if err := w.Move(&r, "approved", "Анна", "edit", 0, "", "", now); !errors.Is(err, ErrWorkflowRole) {
		t.Fatalf("approving takes manage: %v", err)
	}
	if err := w.Move(&r, "approved", "Олег", "manage", 0, "", "", now); err != nil {
		t.Fatal(err)
	}
	last := r.Journal[len(r.Journal)-1]
	if last.Who != "Олег" || last.From != "review" || last.To != "approved" || last.Version != 1 || last.Hash != "h1" {
		t.Fatalf("the journal: %+v", last)
	}
	st := w.State(r, "edit")
	if st.Status.ID != "approved" || len(st.Moves) != 1 || st.Moves[0].To != "dev" || !st.Moves[0].Allowed {
		t.Fatalf("state: %+v", st)
	}
	if w.State(r, "view").Moves[0].Allowed {
		t.Fatal("a viewer is offered the move, not allowed it")
	}
	if err := w.Move(&r, "dev", "", "edit", 0, "", strings.Repeat("я", MaxWorkflowNote+1), now); !errors.Is(err, ErrWorkflowNote) {
		t.Fatalf("a long note: %v", err)
	}
}

func TestWorkflowJournalIsBounded(t *testing.T) {
	w := prototypeWorkflow()
	var r WorkflowRecord
	now := time.UnixMilli(1000)
	w.Follow(&r, "h0", now)
	for i := 0; i < MaxWorkflowJournal; i++ {
		_ = w.Move(&r, "review", "", "manage", 0, "", "", now)
		_ = w.Move(&r, "draft", "", "manage", 0, "", "", now)
	}
	if len(r.Journal) != MaxWorkflowJournal {
		t.Fatalf("journal of %d lines", len(r.Journal))
	}
}

func TestWorkflowManifestCheck(t *testing.T) {
	m := validManifest()
	m.Targets = append(m.Targets, "sinopia.site")
	m.Workflow = prototypeWorkflow()
	for _, p := range m.Check() {
		if strings.HasPrefix(p.Path, "workflow") {
			t.Fatalf("a good workflow is refused: %+v", p)
		}
	}
	bad := validManifest()
	bad.Workflow = &Workflow{
		Statuses:    []WorkflowStatus{{ID: "Draft", Title: ""}, {ID: "done", Title: "Готово", Tone: "pink"}},
		Transitions: []WorkflowTransition{{From: "done", To: "nowhere", Title: "x", Role: "boss"}, {From: "done", To: "done", Title: "x"}},
	}
	bad.Agent = &Agent{Tools: &ToolPolicy{When: []ToolWhen{{Status: []string{"gone"}}}}}
	paths := map[string]bool{}
	for _, p := range bad.Check() {
		paths[p.Path] = true
	}
	for _, want := range []string{"workflow", "workflow.statuses[0].id", "workflow.statuses[0].title", "workflow.statuses[0].edit",
		"workflow.statuses[1].tone", "workflow.transitions[0].to", "workflow.transitions[0].role", "workflow.transitions[1]",
		"agent.tools.when[0].status", "agent.tools.when[0]"} {
		if !paths[want] {
			t.Errorf("no problem at %s: %v", want, paths)
		}
	}
	none := validManifest()
	none.Agent = &Agent{Tools: &ToolPolicy{When: []ToolWhen{{Status: []string{"draft"}, Deny: []string{"Edit"}}}}}
	found := false
	for _, p := range none.Check() {
		found = found || p.Path == "agent.tools.when"
	}
	if !found {
		t.Fatal("rules by status without a workflow are refused")
	}
}

func TestToolRulesByStatus(t *testing.T) {
	m := &Manifest{ID: "acme.proto", Agent: &Agent{Tools: &ToolPolicy{
		Allow: []string{"Edit"},
		When: []ToolWhen{
			{Status: []string{"review", "approved"}, Deny: []string{"Edit"}},
			{Status: []string{"dev"}, Confirm: []string{"Bash(git push*)"}},
		},
	}}}
	edit := Call{Tool: "Edit", Input: map[string]any{"file_path": "/w/a.html"}, Cwd: "/w"}
	if v := Decide([]*Manifest{m}, edit); v.Decision != DecideAllow {
		t.Fatalf("no document known: the rules by status do not hold: %+v", v)
	}
	edit.Status = map[string][]string{"acme.proto": {"draft"}}
	if v := Decide([]*Manifest{m}, edit); v.Decision != DecideAllow {
		t.Fatalf("a draft is edited: %+v", v)
	}
	edit.Status = map[string][]string{"acme.proto": {"approved"}}
	if v := Decide([]*Manifest{m}, edit); v.Decision != DecideDeny || v.Rule != "Edit" {
		t.Fatalf("an approved version is not edited: %+v", v)
	}
	push := Call{Tool: "Bash", Input: map[string]any{"command": "git push origin main"}, Status: map[string][]string{"acme.proto": {"dev"}}}
	if v := Decide([]*Manifest{m}, push); v.Decision != DecideConfirm {
		t.Fatalf("a push in development waits for the person: %+v", v)
	}
	// Two documents of the app in the chat: a draft does not lift the rules of the approved one.
	edit.Status = map[string][]string{"acme.proto": {"draft", "approved"}}
	if v := Decide([]*Manifest{m}, edit); v.Decision != DecideDeny {
		t.Fatalf("the strictest status decides: %+v", v)
	}
	// The snapshot's status (read by the guard beside the launch) decides a call that names none.
	snap := Snapshot{Apps: []SnapshotApp{{ID: "acme.proto", Name: "Прототип", Tools: m.Agent.Tools}}, Status: map[string][]string{"acme.proto": {"review"}}}
	if v, _ := snap.Decide(Call{Tool: "Edit", Input: map[string]any{"file_path": "/w/a.html"}, Cwd: "/w"}); v.Decision != DecideDeny {
		t.Fatalf("the snapshot's status: %+v", v)
	}
	// Claude's own rules are unconditional: the rules by status are not among them.
	deny, ask := ClaudeRules(snap.Apps)
	for _, r := range append(deny, ask...) {
		if strings.Contains(r, "Edit") || strings.Contains(r, "git push") {
			t.Fatalf("a rule by status became Claude's own: %v", r)
		}
	}
}

func TestChatStatusReachesTheChatsLaunches(t *testing.T) {
	root := t.TempDir()
	r := &Registry{Root: filepath.Join(root, "apps")}
	launch := filepath.Join(root, "app-launch")
	for _, s := range []string{"aaaa", "bbbb"} {
		if err := os.MkdirAll(filepath.Join(launch, s), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeGrant(filepath.Join(launch, "aaaa"), []string{"acme.proto"}, "tab-1"); err != nil {
		t.Fatal(err)
	}
	if err := writeGrant(filepath.Join(launch, "bbbb"), []string{"acme.proto"}, "tab-2"); err != nil {
		t.Fatal(err)
	}
	s := ChatDocStatus{App: "acme.proto", Doc: "d1", Version: 2, Status: "review", At: 1}
	if changed, err := r.NoteChatStatus("tab-1", s); err != nil || !changed {
		t.Fatalf("noted: %v %v", changed, err)
	}
	if changed, _ := r.NoteChatStatus("tab-1", s); changed {
		t.Fatal("the same status again changes nothing")
	}
	// Another document of the same app in the chat keeps its own status beside it.
	if _, err := r.NoteChatStatus("tab-1", ChatDocStatus{App: "acme.proto", Doc: "d2", Version: 1, Status: "draft", At: 2}); err != nil {
		t.Fatal(err)
	}
	if got := ReadChatStatus(filepath.Join(launch, "aaaa", ChatStatusFile)); strings.Join(got["acme.proto"], ",") != "draft,review" {
		t.Fatalf("the chat's launch: %v", got)
	}
	if got := ReadChatStatus(filepath.Join(launch, "bbbb", ChatStatusFile)); len(got) != 0 {
		t.Fatalf("another chat's launch: %v", got)
	}
	if got := r.ChatStatuses("tab-1"); got["acme.proto/d1"] != s {
		t.Fatalf("kept for later launches: %v", got)
	}
	for i := 0; i < MaxChatDocs+5; i++ {
		_, _ = r.NoteChatStatus("tab-3", ChatDocStatus{App: "acme.proto", Doc: "doc" + strconv.Itoa(i), Version: 1, Status: "draft", At: int64(i)})
	}
	if got := r.ChatStatuses("tab-3"); len(got) != MaxChatDocs || got["acme.proto/doc0"].Doc != "" {
		t.Fatalf("a chat keeps the last %d documents: %d", MaxChatDocs, len(got))
	}
	if changed, _ := r.NoteChatStatus("../x", s); changed {
		t.Fatal("a chat id that is not one is ignored")
	}
	m := &Manifest{ID: "acme.proto", Name: "Прототип", Workflow: prototypeWorkflow(),
		Agent: &Agent{Tools: &ToolPolicy{When: []ToolWhen{{Status: []string{"review"}, Deny: []string{"Edit"}}}}}}
	text := ChatStatusNotice(map[string]ChatDocStatus{"acme.proto/d1": s}, func(string) *Manifest { return m })
	for _, want := range []string{"Прототип", "d1", "version 2", "На ревью", "next version", "Edit"} {
		if !strings.Contains(text, want) {
			t.Errorf("the notice says %q: %s", want, text)
		}
	}
}

func validManifest() *Manifest {
	return &Manifest{ID: "acme.proto", Name: "Прототип", Description: "Прототип мобильного экрана", Version: "1.0.0",
		Harnessly: ">=0.17", Trust: TrustSandboxed, Targets: []string{"chat"}}
}

func TestWorkflowDecisionKeepsItsVersion(t *testing.T) {
	w := prototypeWorkflow()
	now := time.UnixMilli(1_000)
	var r WorkflowRecord
	w.Follow(&r, "h1", now)
	if err := w.Move(&r, "review", "Анна", "edit", 1, "h1", "", now); err != nil {
		t.Fatal(err)
	}
	if !r.MarkRev(now.UnixMilli(), 1, "abc123") {
		t.Fatal("the move's entry was not found")
	}
	if r.MarkRev(now.UnixMilli(), 1, "def456") {
		t.Fatal("an entry's version was replaced")
	}
	st := w.State(r, "edit")
	st.Journal[len(st.Journal)-1].Link = "/a/doc/r/abc123/page.html"
	if got := r.Journal[len(r.Journal)-1]; got.Rev != "abc123" || got.Link != "" {
		t.Fatalf("kept entry: %+v", got)
	}
}
