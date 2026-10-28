package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// design.created and design.claim travel agent → gateway as events: the runtime that made (or owns the files of) a
// project tells its own machine agent over the loopback, the agent passes it on.

const (
	DesignCreated = "design.created"
	DesignClaim   = "design.claim"
	DesignDeleted = "design.deleted"

	designEventTO = 3 * time.Second
)

var designEventKinds = map[string]bool{DesignCreated: true, DesignClaim: true, DesignDeleted: true}

// designEventRoute is POST /design/event {kind, project}: the event goes to every cluster link. With no link it is
// dropped (the gateway learns of the project when it lists them), and the answer says so.
func (a *Agent) designEventRoute(w http.ResponseWriter, r *http.Request) {
	if !tokenMatches(r.Header.Get(a.cfg.LocalTokenHeader), a.localToken) {
		reply(w, 403, map[string]string{"error": "no local agent token (agent.json in the data directory)"})
		return
	}
	obj := readObject(r, docBodyMax)
	kind, _ := field[string](obj, "kind")
	project := str(obj, "project", 128)
	if obj == nil || !designEventKinds[kind] || project == nil {
		reply(w, 400, map[string]string{"error": "kind (design.created, design.claim or design.deleted) and project are required"})
		return
	}
	sent := a.connected()
	if sent {
		a.broadcast(evDesign{Event: kind, Project: *project})
	}
	reply(w, 202, map[string]bool{"ok": true, "sent": sent})
}

// EmitDesignEvent asks the machine agent whose agent.json lies in dataDir to send a design event to the cluster. It
// never blocks the caller and never reports a failure: no agent, no event.
func EmitDesignEvent(dataDir, tokenHeader, kind, project string) {
	if dataDir == "" || !designEventKinds[kind] || project == "" {
		return
	}
	go emitDesignEvent(dataDir, tokenHeader, kind, project)
}

func emitDesignEvent(dataDir, tokenHeader, kind, project string) {
	port, token, ok := localAgent(dataDir)
	if !ok {
		return
	}
	body, _ := json.Marshal(map[string]string{"kind": kind, "project": project})
	ctx, cancel := context.WithTimeout(context.Background(), designEventTO)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://127.0.0.1:"+port+"/design/event", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("content-type", "application/json")
	if tokenHeader == "" {
		tokenHeader = "x-agent-local-token"
	}
	req.Header.Set(tokenHeader, token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		Log("warn", "design event not handed to the agent", F{"kind", kind}, F{"error", err.Error()})
		return
	}
	res.Body.Close()
}

// localAgent reads where the machine agent whose agent.json lies in dataDir listens on the loopback and the token that
// opens its local routes.
func localAgent(dataDir string) (port, token string, ok bool) {
	raw, err := os.ReadFile(filepath.Join(dataDir, "agent.json"))
	if err != nil {
		return "", "", false
	}
	var info struct {
		Port  any    `json:"port"`
		Token string `json:"token"`
	}
	if json.Unmarshal(raw, &info) != nil || info.Token == "" {
		return "", "", false
	}
	switch p := info.Port.(type) {
	case float64:
		if p != 0 {
			port = strconv.FormatFloat(p, 'f', -1, 64)
		}
	case string:
		port = p
	}
	return port, info.Token, port != ""
}
