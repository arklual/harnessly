package agent

import (
	"encoding/json"
	"net/http"
	"time"
)

// The shared Telegram bot lives in the cluster, its token never on the machine: the runtime asks the cluster
// for the person's Telegram link and sends the agents' messages through it with the event "notify.call"
// {requestId, action, ...}, answered by the op "notify.result" {requestId, status, body}. Loopback route, same token
// as the doc routes:
//
//	POST /notify/call {action: "status"|"link"|"unlink"|"instruction"|"send", instruction?, parts?, silent?}
//
// It answers {status, body}: the cluster's status and body as they were. The agent itself answers 503 with no link and
// 504 when the cluster does not answer (an older cluster does not know the event).

const (
	notifyRelayTO  = 20 * time.Second
	notifyBodyMax  = 256 * 1024
	notifySendWait = 45 * time.Second
)

var notifyActions = map[string]bool{"status": true, "link": true, "unlink": true, "instruction": true, "send": true}

type evNotifyCall struct {
	Event       string          `json:"event"`
	RequestID   string          `json:"requestId"`
	Action      string          `json:"action"`
	Instruction *string         `json:"instruction,omitempty"`
	Parts       json.RawMessage `json:"parts,omitempty"`
	Silent      bool            `json:"silent,omitempty"`
}

// notifyRoute serves /notify/call.
func (a *Agent) notifyRoute(w http.ResponseWriter, r *http.Request) {
	if !tokenMatches(r.Header.Get(a.cfg.LocalTokenHeader), a.localToken) {
		reply(w, 403, map[string]string{"error": "no local agent token (agent.json in the data directory)"})
		return
	}
	obj := readObject(r, notifyBodyMax)
	if obj == nil {
		reply(w, 400, map[string]string{"error": "bad json"})
		return
	}
	action, _ := field[string](obj, "action")
	if !notifyActions[action] {
		reply(w, 400, map[string]string{"error": "unknown action"})
		return
	}
	if !a.connected() {
		reply(w, 503, map[string]string{"error": "the platform is not connected, try again later"})
		return
	}
	id := newRequestID()
	ev := evNotifyCall{Event: "notify.call", RequestID: id, Action: action}
	wait := notifyRelayTO
	switch action {
	case "instruction":
		text, _ := field[string](obj, "instruction")
		ev.Instruction = &text
	case "send":
		ev.Parts = obj["parts"]
		ev.Silent, _ = field[bool](obj, "silent")
		wait = notifySendWait
	}
	reply(w, 200, a.askCluster(id, ev, wait))
}
