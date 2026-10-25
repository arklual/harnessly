package workflows

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// testdata/conv is a scrubbed copy of what Claude Code 2.1.310 keeps of two runs beside a conversation: one at work
// (no final state yet; a reviewer returned an object, one a text, one failed, the fix phase has begun) and one over.
const (
	running = "wf_3c9e1a7b-5d2"
	over    = "wf_8f04b6d2-a71"
)

func TestParseMeta(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "conv", "workflows", "scripts", "checkout-review-"+running+".js"))
	if err != nil {
		t.Fatal(err)
	}
	got := ParseMeta(string(raw))
	want := Meta{Name: "checkout-review", Description: "Review the checkout flow, then fix what the reviewers found",
		Phases: []string{"Review", "Fix", "Verify"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("meta %+v", got)
	}
	if m := ParseMeta("const x = 1"); !reflect.DeepEqual(m, Meta{}) {
		t.Fatalf("no meta %+v", m)
	}
	if m := ParseMeta("export const meta = { name: 'it\\'s', phases: [] }\n"); m.Name != "it's" {
		t.Fatalf("escaped %+v", m)
	}
}

func TestParseLaunch(t *testing.T) {
	text := "Workflow launched in background. Task ID: wq7m2kx4d\nSummary: Review the flow\nTranscript dir: /x\nRun ID: wf_3c9e1a7b-5d2\n"
	l, ok := ParseLaunch(text)
	if !ok || l != (Launch{TaskID: "wq7m2kx4d", RunID: running, Summary: "Review the flow"}) {
		t.Fatalf("launch %+v %v", l, ok)
	}
	if _, ok := ParseLaunch("Workflow failed: syntax error"); ok {
		t.Fatal("a failed launch is no run")
	}
}

func TestReadRunning(t *testing.T) {
	var r Reader
	v, ok := r.Read(filepath.Join("testdata", "conv.jsonl"), running)
	if !ok {
		t.Fatal("no run")
	}
	if v.Over() || v.Name != "checkout-review" {
		t.Fatalf("view %+v", v)
	}
	var titles []string
	for _, p := range v.Phases {
		titles = append(titles, p.Title)
	}
	if !reflect.DeepEqual(titles, []string{"Review", "Fix", "Verify"}) {
		t.Fatalf("phases %v", titles)
	}
	review := v.Phases[0].Agents
	if len(review) != 3 || len(v.Phases[1].Agents) != 1 || len(v.Phases[2].Agents) != 0 {
		t.Fatalf("agents %+v", v.Phases)
	}
	pay, forms, copyA := review[0], review[1], review[2]
	if pay.Label != "review:payments" || pay.State != Done || pay.Model != "claude-sonnet-5-5" ||
		pay.Result != "Two places read the widget answer without checking its status." || pay.StartedAt != 1792922406000 || pay.EndedAt == 0 {
		t.Fatalf("payments %+v", pay)
	}
	if forms.State != Done || forms.Result != "The form posts `orderId` while the widget sends `order_id`. Details follow." {
		t.Fatalf("forms %+v", forms)
	}
	if copyA.State != Failed {
		t.Fatalf("copy %+v", copyA)
	}
	fix := v.Phases[1].Agents[0]
	if fix.State != Running || fix.EndedAt != 0 || fix.Last != "Edit · acme/web/pay/form.ts" {
		t.Fatalf("fix %+v", fix)
	}
	settled := v.Settle("killed")
	if settled.Status != "killed" || settled.Phases[1].Agents[0].State != Stopped || v.Phases[1].Agents[0].State != Running {
		t.Fatalf("settled %+v", settled)
	}
}

func TestReadOver(t *testing.T) {
	var r Reader
	v, ok := r.Read(filepath.Join("testdata", "conv.jsonl"), over)
	if !ok || !v.Over() || v.Status != "completed" || v.Name != "pricing-research" {
		t.Fatalf("view %+v", v)
	}
	if v.StartedAt != 1792918806000 || v.EndedAt != 1792918806000+1194000 {
		t.Fatalf("times %v %v", v.StartedAt, v.EndedAt)
	}
	agents := v.Phases[0].Agents
	if len(v.Phases) != 1 || len(agents) != 2 || agents[0].Result != "Plans start at $9 per seat." || agents[1].State != Done {
		t.Fatalf("agents %+v", v.Phases)
	}
	if agents[0].StartedAt != 1792918807000 {
		t.Fatalf("the final state's start wins: %v", agents[0].StartedAt)
	}
}

func TestReadFollowsTheJournal(t *testing.T) {
	dir := t.TempDir()
	transcript := filepath.Join(dir, "s.jsonl")
	run := filepath.Join(dir, "s", "subagents", "workflows", running)
	if err := os.MkdirAll(run, 0o755); err != nil {
		t.Fatal(err)
	}
	journal := filepath.Join(run, "journal.jsonl")
	write := func(s string) {
		f, err := os.OpenFile(journal, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if _, err := f.WriteString(s); err != nil {
			t.Fatal(err)
		}
	}
	var r Reader
	write(`{"type":"launched"}` + "\n" + `{"type":"started","agentId":"a1","label":"one","phase":"Go"}` + "\n" + `{"type":"res`)
	v, ok := r.Read(transcript, running)
	if !ok || len(v.Phases) != 1 || v.Phases[0].Title != "Go" || v.Phases[0].Agents[0].State != Running {
		t.Fatalf("first %+v", v)
	}
	write(`ult","agentId":"a1","result":"fine"}` + "\n")
	v, _ = r.Read(transcript, running)
	if a := v.Phases[0].Agents[0]; a.State != Done || a.Result != "fine" {
		t.Fatalf("a line written in two goes is read once whole: %+v", a)
	}
	if _, ok := r.Read(transcript, "../../etc"); ok {
		t.Fatal("a run id is a run's name")
	}
	if _, ok := r.Read(transcript, "wf_00000000-000"); ok {
		t.Fatal("a run the conversation does not have")
	}
}

func TestResultOf(t *testing.T) {
	for raw, want := range map[string]string{
		`"plain words"`:                    "plain words",
		`{"verdict":"keep","x":1}`:         "keep",
		`{"done":["a"],"notes":"written"}`: "written",
		`{"n":1}`:                          "",
		`null`:                             "",
	} {
		if got := resultOf([]byte(raw)); got != want {
			t.Errorf("%s: %q", raw, got)
		}
	}
}
