package agent

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

const evalTestRun = "0b3f1c7e-5a62-4d0e-9a41-2f7d3c5b8e10"

func evalStart(id string) map[string]any {
	return map[string]any{
		"runId": id,
		"task":  map[string]any{"key": "t", "repo": "app", "base": "abcdef1", "prompt": "p", "checks": []map[string]string{{"name": "c", "run": "true"}}},
		"arm":   map[string]any{"key": "a", "model": "m", "harness": "repo"},
	}
}

// A run spends the owner's subscription and is an agent session on their machine: nothing is offered or served until the
// operator opts in on the machine.
func TestEvalRunNeedsTheOptIn(t *testing.T) {
	t.Setenv(evalOptIn, "")
	r := startHubAgent(t, rigOptions{observe: true})
	c := r.connect()
	if slices.Contains(r.agent.hub.hello().Features, hubproto.FeatureEvalRun) {
		t.Fatal("the feature was offered without the opt-in")
	}
	for _, op := range []string{hubproto.OpEvalRunStart, hubproto.OpEvalRunPoll, hubproto.OpEvalRunStop} {
		if _, e := c.call(op, evalStart(evalTestRun)); !strings.HasPrefix(e, "policy_denied: ") {
			t.Errorf("%s without the opt-in: %q", op, e)
		}
	}
	// The closed cluster's names stay refused on a hub link.
	if _, e := c.call("eval.start", nil); !strings.HasPrefix(e, "policy_denied: ") {
		t.Errorf("eval.start: %q", e)
	}
}

func TestEvalRunServedWithTheOptInAndASandbox(t *testing.T) {
	t.Setenv(evalOptIn, "on")
	r := startHubAgent(t, rigOptions{observe: true})
	c := r.connect()
	run := r.agent.hub.evalRunnerOf()

	run.BwrapPath = ""
	if slices.Contains(r.agent.hub.hello().Features, hubproto.FeatureEvalRun) {
		t.Fatal("the feature was offered on a machine that cannot sandbox")
	}
	if _, e := c.call(hubproto.OpEvalRunPoll, map[string]any{"runIds": []string{}}); !strings.HasPrefix(e, "policy_denied: ") {
		t.Fatalf("poll without a sandbox: %q", e)
	}

	run.BwrapPath = "/bin/true"
	if !slices.Contains(r.agent.hub.hello().Features, hubproto.FeatureEvalRun) {
		t.Fatal("the feature was not offered")
	}
	if _, e := c.call(hubproto.OpEvalRunStart, evalStart("../x")); !strings.HasPrefix(e, "invalid: ") {
		t.Fatalf("a bad run id: %q", e)
	}
	if _, e := c.call(hubproto.OpEvalRunStart, map[string]any{"runId": evalTestRun, "task": 5, "arm": "x"}); !strings.HasPrefix(e, "invalid: ") {
		t.Fatalf("documents that are not objects: %q", e)
	}
	c.mustCall(hubproto.OpEvalRunStart, evalStart(evalTestRun))
	c.mustCall(hubproto.OpEvalRunStart, evalStart(evalTestRun)) // asking again is not an error

	var got struct {
		Runs map[string]struct{ State string } `json:"runs"`
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got = decode[struct {
			Runs map[string]struct{ State string } `json:"runs"`
		}](t, c.mustCall(hubproto.OpEvalRunPoll, map[string]any{"runIds": []string{evalTestRun, "0b3f1c7e-5a62-4d0e-9a41-2f7d3c5b8e11"}}))
		if got.Runs[evalTestRun].State == "skipped" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(got.Runs) != 1 || got.Runs[evalTestRun].State != "skipped" {
		t.Fatalf("poll: %+v", got)
	}

	too := make([]string, hubproto.EvalRunPollMax+1)
	for i := range too {
		too[i] = evalTestRun
	}
	if _, e := c.call(hubproto.OpEvalRunPoll, map[string]any{"runIds": too}); !strings.HasPrefix(e, "invalid: ") {
		t.Fatalf("too many runs in a poll: %q", e)
	}
	c.mustCall(hubproto.OpEvalRunStop, map[string]any{"runId": evalTestRun})
	if _, e := c.call(hubproto.OpEvalRunStop, map[string]any{"runId": "nope"}); !strings.HasPrefix(e, "invalid: ") {
		t.Fatalf("stop with a bad id: %q", e)
	}
}
