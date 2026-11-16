package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// A connector of the runtime (internal/connectors, docs/connectors.md) hands its events for the hub's rules to this
// agent, which sends them on the links of the hubs that subscribed automations.event. The runtime keeps an event in
// its outbox until the agent answers 202, so an event is never lost while no hub link is up: 503 says try again.

// AutomationEventRoute is the local route the runtime posts a connector's event to.
const AutomationEventRoute = "/automations/event"

// ErrNoHubTook says no hub link took the event: none is up, or none subscribed automations.event (an older hub).
var ErrNoHubTook = errors.New("no hub link takes connector events")

// automationEventMax caps one event: a connector event is a short envelope without text.
const automationEventMax = 32 << 10

// evAutomationEvent is the frame on the link. The event goes under data: "event" names the frame.
type evAutomationEvent struct {
	Event       string          `json:"event"`
	Source      string          `json:"source"`
	Fingerprint string          `json:"fingerprint"`
	Data        json.RawMessage `json:"data"`
}

// automationEventRoute is POST /automations/event with {source, fingerprint, event}: 202 when a hub link that
// subscribed automations.event took it, 503 when none did.
func (a *Agent) automationEventRoute(w http.ResponseWriter, r *http.Request) {
	if !tokenMatches(r.Header.Get(a.cfg.LocalTokenHeader), a.localToken) {
		reply(w, 403, map[string]string{"error": "no local agent token (agent.json in the data directory)"})
		return
	}
	obj := readObject(r, automationEventMax)
	source, ok1 := field[string](obj, "source")
	fingerprint, ok2 := field[string](obj, "fingerprint")
	ev := obj["event"]
	if obj == nil || !ok1 || !ok2 || source == "" || fingerprint == "" || len(ev) == 0 || ev[0] != '{' {
		reply(w, 400, map[string]string{"error": "source, fingerprint and event (an object) are required"})
		return
	}
	if a.hub == nil || !a.hub.emitAutomationEvent(source, fingerprint, ev) {
		reply(w, 503, map[string]string{"error": ErrNoHubTook.Error()})
		return
	}
	reply(w, 202, map[string]bool{"ok": true})
}

// emitAutomationEvent sends a connector's event to every hub link that subscribed it, and says whether one did. A link
// that subscribed nothing (an older hub) is not counted: it would drop an event it does not know.
func (h *hub) emitAutomationEvent(source, fingerprint string, event json.RawMessage) bool {
	frame, err := marshal(evAutomationEvent{Event: hubproto.EventAutomationEvent, Source: source, Fingerprint: fingerprint, Data: event})
	if err != nil {
		return false
	}
	sent := false
	for _, l := range h.hubLinks() {
		if l.subscribed(hubproto.EventAutomationEvent) {
			l.sendEvent(hubproto.EventAutomationEvent, frame)
			sent = true
		}
	}
	return sent
}

// SendAutomationEvent hands one connector event to the machine agent whose agent.json lies in dataDir. nil: a hub took
// it; ErrNoHubTook: keep it and try again later.
func SendAutomationEvent(ctx context.Context, dataDir, tokenHeader, source, fingerprint string, event json.RawMessage) error {
	port, token, ok := localAgent(dataDir)
	if !ok {
		return errNoLocalAgent
	}
	body, err := json.Marshal(map[string]any{"source": source, "fingerprint": fingerprint, "event": event})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://127.0.0.1:"+port+AutomationEventRoute, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("content-type", "application/json")
	if tokenHeader == "" {
		tokenHeader = "x-agent-local-token"
	}
	req.Header.Set(tokenHeader, token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	switch res.StatusCode {
	case http.StatusAccepted:
		return nil
	case http.StatusServiceUnavailable:
		return ErrNoHubTook
	}
	return fmt.Errorf("the agent answered %d", res.StatusCode)
}

// opWebhook passes a request that came to a webhook's address on the hub to the runtime, which checks its signature by
// the connection's secret and keeps it (docs/connectors.md, «Incoming webhook»): served while the link may reach the
// runtime for automations (automationsServed). The answer is the status the hub gives the sender. Nothing of the
// request is logged here.
func (h *hub) opWebhook(req map[string]json.RawMessage, pol hubproto.Policy) (any, func(), error) {
	if !h.automationsServed(pol) {
		return nil, nil, h.allow(hubproto.OpConnectorsWebhook, false, "webhooks through the hub")
	}
	if h.a.cfg.RuntimePort == 0 {
		return nil, nil, hubproto.Errf(hubproto.CodeUnsupported, "no runtime on this machine")
	}
	res, err := h.a.callRuntime(h.a.cfg.RuntimeAPI+"/connector-webhook", body(req))
	if err != nil {
		return nil, nil, hubproto.Errf(hubproto.CodeFailed, "the runtime did not take the request")
	}
	return res, nil, nil
}
