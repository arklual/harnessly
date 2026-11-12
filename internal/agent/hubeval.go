package agent

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ly-solutions/harnessly/internal/evalrun"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// The hub's evaluation of a harness (docs/hub/eval.md): the hub hands this machine one golden task at a time and
// the agent runs it in a sandbox, paid from the owner's own subscription. A run is an agent session on the machine with an
// access-only copy of the owner's login, so it is served only when the operator opted in on this machine, and never on a
// machine that cannot sandbox it.
const (
	// evalOptIn switches the evaluation on for this machine: AGENT_HUB_EVAL=on.
	evalOptIn = "AGENT_HUB_EVAL"
	// evalWorkspaceVar names the folder of the person's source checkouts (default: <home>/workspace).
	evalWorkspaceVar = "AGENT_EVAL_WORKSPACE"
	// evalToolDirsVar lists, colon-separated, folders with tools the run may read (compilers, package stores).
	evalToolDirsVar = "AGENT_EVAL_TOOL_DIRS"
)

// evalRunner is the machine's runner, made on the first call.
type evalState struct {
	once   sync.Once
	runner *evalrun.Runner
}

// evalRunnerOf is the runner of this machine.
func (h *hub) evalRunnerOf() *evalrun.Runner {
	h.eval.once.Do(func() {
		cfg := h.a.cfg
		workspace := os.Getenv(evalWorkspaceVar)
		if workspace == "" {
			workspace = filepath.Join(cfg.Home, "workspace")
		}
		var tools []string
		for _, d := range strings.Split(os.Getenv(evalToolDirsVar), ":") {
			if d = strings.TrimSpace(d); d != "" {
				tools = append(tools, d)
			}
		}
		h.eval.runner = evalrun.New(cfg.Home, filepath.Join(h.dir, "eval"), workspace, tools)
	})
	return h.eval.runner
}

// evalServed says this machine takes evaluation runs: the operator opted in, the kill switch is not on, and the machine
// can sandbox a run (Linux with bubblewrap).
func (h *hub) evalServed() bool {
	if h.policy.Disabled() || os.Getenv(evalOptIn) != "on" {
		return false
	}
	return h.evalRunnerOf().BwrapPath != ""
}

func (h *hub) opEval(op string, req map[string]json.RawMessage) (any, func(), error) {
	if err := h.allow(op, h.evalServed(), "evaluation runs"); err != nil {
		return nil, nil, err
	}
	r := h.evalRunnerOf()
	switch op {
	case hubproto.OpEvalRunStart:
		var q evalrun.Request
		q.RunID, _ = field[string](req, "runId")
		task, _ := field[json.RawMessage](req, "task")
		arm, _ := field[json.RawMessage](req, "arm")
		if json.Unmarshal(task, &q.Task) != nil || json.Unmarshal(arm, &q.Arm) != nil {
			return nil, nil, invalid("task and arm must be objects")
		}
		if err := r.Start(q); err != nil {
			if errors.Is(err, evalrun.ErrBusy) {
				return nil, nil, hubproto.Errf(hubproto.CodeBusy, "%s", err.Error())
			}
			return nil, nil, invalid("%s", err.Error())
		}
		h.audit.Record(op, true, map[string]any{"runId": q.RunID, "task": q.Task.Key, "arm": q.Arm.Key})
		return empty(), nil, nil
	case hubproto.OpEvalRunPoll:
		ids, _ := field[[]string](req, "runIds")
		if len(ids) > hubproto.EvalRunPollMax {
			return nil, nil, invalid("at most %d runs in one poll", hubproto.EvalRunPollMax)
		}
		return map[string]any{"runs": r.Poll(ids)}, nil, nil
	default: // hubproto.OpEvalRunStop
		id, _ := field[string](req, "runId")
		if !evalrun.RunID.MatchString(id) {
			return nil, nil, invalid("runId must be a UUID")
		}
		r.Stop(id)
		h.audit.Record(op, true, map[string]any{"runId": id})
		return empty(), nil, nil
	}
}
