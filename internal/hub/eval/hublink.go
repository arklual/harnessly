package eval

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/link"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// Linker reaches a machine over the agent link.
type Linker interface {
	Get(machineID string) *link.Link
	Do(ctx context.Context, machineID, op string, params any, timeout time.Duration) (json.RawMessage, error)
}

// callTimeout bounds one request to a machine; starting a run only registers it, the session goes on in the background.
const callTimeout = 20 * time.Second

// HubLink is the Link that goes over the hub's links to the machines' agents (hubproto eval.run.*).
type HubLink struct {
	Links Linker
	// Offers says whether the machine's agent said the feature in its hello.
	Offers func(machineID, feature string) bool
}

// Connected: the machine is linked now and its agent serves evaluation runs (the operator opted in there).
func (l *HubLink) Connected(machineID string) bool {
	if l.Links == nil || l.Links.Get(machineID) == nil {
		return false
	}
	return l.Offers != nil && l.Offers(machineID, hubproto.FeatureEvalRun)
}

// notDelivered says the error means the request never reached a runner that took it: the machine is not linked, the link
// dropped before an answer, or the agent refused the request for what it is (not served, not allowed, busy, not valid).
func notDelivered(err error) bool {
	var he *httpx.Error
	if !errors.As(err, &he) {
		return errors.Is(err, link.ErrClosed)
	}
	switch he.Code {
	case "machine_offline", "machine_policy", "provider_off", "busy", "invalid_request":
		return true
	}
	return he.Status == http.StatusConflict
}

// Start hands one run to the machine's agent.
func (l *HubLink) Start(ctx context.Context, machineID string, req RunRequest) error {
	_, err := l.Links.Do(ctx, machineID, hubproto.OpEvalRunStart, hubproto.EvalRunStart{RunID: req.RunID, Task: req.Task, Arm: req.Arm}, callTimeout)
	if err != nil && notDelivered(err) {
		return errors.Join(ErrNotDelivered, err)
	}
	return err
}

// Poll asks for the state of the runs; a run the agent does not know is missing from its answer.
func (l *HubLink) Poll(ctx context.Context, machineID string, runIDs []string) (map[string]RunState, error) {
	out := map[string]RunState{}
	for len(runIDs) > 0 {
		n := min(len(runIDs), hubproto.EvalRunPollMax)
		raw, err := l.Links.Do(ctx, machineID, hubproto.OpEvalRunPoll, map[string]any{"runIds": runIDs[:n]}, callTimeout)
		if err != nil {
			return nil, err
		}
		var ans struct {
			Runs map[string]RunState `json:"runs"`
		}
		if err := json.Unmarshal(raw, &ans); err != nil {
			return nil, err
		}
		for id, st := range ans.Runs {
			out[id] = st
		}
		runIDs = runIDs[n:]
	}
	return out, nil
}

// Stop asks the agent to end a run.
func (l *HubLink) Stop(ctx context.Context, machineID, runID string) error {
	_, err := l.Links.Do(ctx, machineID, hubproto.OpEvalRunStop, map[string]string{"runId": runID}, callTimeout)
	return err
}
