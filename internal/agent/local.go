package agent

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Loopback routes for the CLIs and conversations of this Unix account. The port is also reachable through socket
// proxies that connect from 127.0.0.1, so the source address proves nothing; a token in a 0600 file under the
// account's data directory does: only processes of that user can read it.

const (
	docBodyMax    = 64 * 1024
	stateBodyMax  = 4 * 1024 * 1024
	reportBodyMax = 1024 * 1024
	stateResultTO = 5 * time.Second
)

// writeLocalState writes agent.json (port and token) and returns the token.
func writeLocalState(dir string, port int) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	token := hex.EncodeToString(buf)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	file := filepath.Join(dir, "agent.json")
	tmp := fmt.Sprintf("%s.%d.tmp", file, os.Getpid())
	raw, _ := marshal(map[string]any{"port": port, "token": token})
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o600); err != nil {
		return "", err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, file); err != nil {
		return "", err
	}
	return token, nil
}

func tokenMatches(given, expected string) bool {
	return given != "" && subtle.ConstantTimeCompare([]byte(given), []byte(expected)) == 1
}

func reply(w http.ResponseWriter, status int, v any) {
	raw, _ := marshal(v)
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(raw)
}

// readObject reads a JSON object body of at most max bytes, or nil.
func readObject(r *http.Request, max int64) map[string]json.RawMessage {
	raw, err := io.ReadAll(io.LimitReader(r.Body, max+1))
	if err != nil || int64(len(raw)) > max {
		return nil
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil || obj == nil {
		return nil
	}
	return obj
}

// str is a non-blank string cut to max characters, or nil.
func str(obj map[string]json.RawMessage, name string, max int) *string {
	s, ok := field[string](obj, name)
	if !ok || strings.TrimSpace(s) == "" {
		return nil
	}
	if r := []rune(s); len(r) > max {
		s = string(r[:max])
	}
	return &s
}

func cut(s string, max int) string {
	if r := []rune(s); len(r) > max {
		return string(r[:max])
	}
	return s
}

func (a *Agent) docRoute(w http.ResponseWriter, r *http.Request) {
	if !tokenMatches(r.Header.Get(a.cfg.LocalTokenHeader), a.localToken) {
		reply(w, 403, map[string]string{"error": "no local agent token (agent.json in the data directory)"})
		return
	}
	obj := readObject(r, docBodyMax)
	if obj == nil {
		reply(w, 400, map[string]string{"error": "bad json"})
		return
	}
	// Nothing is stored here: without a cluster link the call would silently vanish.
	if !a.connected() {
		reply(w, 503, map[string]string{"error": "the platform is not connected, try again later"})
		return
	}
	if r.URL.Path == "/docs/reply" {
		threadID := str(obj, "threadId", 64)
		text, _ := field[string](obj, "body")
		text = cut(strings.TrimSpace(text), 20_000)
		resolve, _ := field[bool](obj, "resolve")
		if threadID == nil || (text == "" && !resolve) {
			reply(w, 400, map[string]string{"error": "threadId and text (or resolve) are required"})
			return
		}
		a.broadcast(evDocReply{Event: "docs.reply", ThreadID: *threadID, Body: text, Resolve: resolve})
		reply(w, 202, map[string]bool{"ok": true})
		return
	}
	slug := str(obj, "slug", 128)
	kind := "artifact"
	if k, _ := field[string](obj, "kind"); k == "board" {
		kind = "board"
	}
	key := str(obj, "key", 128)
	title := str(obj, "title", 300)
	// A board is opened by its key; a closed document is opened by login, so its publisher has no key.
	if slug == nil || title == nil || (kind == "board" && key == nil) {
		msg := "slug and title are required"
		if kind == "board" {
			msg = "slug, key and title are required"
		}
		reply(w, 400, map[string]string{"error": msg})
		return
	}
	var resolved struct {
		TabID          *string `json:"tabId"`
		SessionName    *string `json:"sessionName"`
		AgentSessionID *string `json:"agentSessionId"`
		Provider       *string `json:"provider"`
		Cwd            *string `json:"cwd"`
	}
	if pane := str(obj, "tmuxPane", 32); pane != nil {
		req := map[string]string{"tmuxPane": *pane}
		if socket := str(obj, "tmuxSocket", 64); socket != nil {
			req["tmuxSocket"] = *socket
		}
		if raw, err := a.callRuntime(a.cfg.RuntimeAPI+"/docs/resolve-pane", req); err == nil {
			_ = json.Unmarshal(raw, &resolved)
		}
	}
	provider := "claude"
	if resolved.Provider != nil {
		provider = *resolved.Provider
	}
	cwd := str(obj, "cwd", 1024)
	if cwd == nil {
		cwd = resolved.Cwd
	}
	binding := DocBinding{
		Slug: *slug, Kind: kind, Key: key, Title: *title,
		TabID: resolved.TabID, SessionName: resolved.SessionName, AgentSessionID: resolved.AgentSessionID,
		Provider: provider, SourcePath: str(obj, "sourcePath", 1024), Cwd: cwd,
	}
	a.broadcast(evDocBound{Event: "docs.bound", Binding: binding})
	reply(w, 202, map[string]bool{"ok": true, "boundToTab": binding.TabID != nil})
}

func newRequestID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b[:])
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

// jsNumber is Number(value) || 0 for what a runtime sends as a base version.
func jsNumber(raw json.RawMessage) float64 {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return n
	case bool:
		if n {
			return 1
		}
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(n), 64); err == nil {
			return f
		}
	}
	return 0
}

// stateRoute: the runtime writes its layouts and workspace list through the cluster. It posts the change here, the
// agent sends it over its link and answers with the cluster's result. No link, or no answer in time: the runtime is
// told so and keeps the change on its own disk, marked for the cluster to take later.
func (a *Agent) stateRoute(w http.ResponseWriter, r *http.Request) {
	if !tokenMatches(r.Header.Get(a.cfg.LocalTokenHeader), a.localToken) {
		reply(w, 403, map[string]any{"ok": false, "error": "forbidden"})
		return
	}
	obj := readObject(r, stateBodyMax)
	name, ok := field[string](obj, "name")
	if obj == nil || !ok {
		reply(w, 400, map[string]any{"ok": false, "error": "bad request"})
		return
	}
	if !a.connected() {
		reply(w, 503, map[string]any{"ok": false, "error": "no link to the cluster"})
		return
	}
	id := newRequestID()
	var ev any
	if r.URL.Path == "/state/commit" {
		content := obj["content"]
		if content == nil {
			content = json.RawMessage("null")
		}
		ev = evStateCommit{Event: "state.commit", RequestID: id, Name: name, Base: jsNumber(obj["base"]), Content: content}
	} else {
		session, ok := field[string](obj, "sessionName")
		if !ok {
			reply(w, 502, map[string]any{"ok": false, "error": "bad request"})
			return
		}
		set, _ := field[map[string]json.RawMessage](obj, "set")
		if set == nil {
			set = map[string]json.RawMessage{}
		}
		var unset []string
		if items, ok := field[[]json.RawMessage](obj, "unset"); ok {
			for _, item := range items {
				var s string
				if json.Unmarshal(item, &s) == nil {
					unset = append(unset, s)
				}
			}
		}
		if unset == nil {
			unset = []string{}
		}
		ev = evStatePatch{Event: "state.patch", RequestID: id, Name: name, SessionName: session, Set: set, Unset: unset}
	}
	ch := make(chan StateResult, 1)
	a.mu.Lock()
	a.pendingStates[id] = ch
	a.mu.Unlock()
	a.broadcast(ev)
	var res StateResult
	select {
	case res = <-ch:
	case <-time.After(stateResultTO):
		msg := "the cluster did not answer"
		res = StateResult{OK: false, Error: &msg}
	}
	a.mu.Lock()
	delete(a.pendingStates, id)
	a.mu.Unlock()
	status := 200
	if !res.OK {
		status = 502
	}
	reply(w, status, res)
}

// automationReport relays a launched automation conversation's own report, after checking its single-use token.
func (a *Agent) automationReport(w http.ResponseWriter, r *http.Request) {
	obj := readObject(r, reportBodyMax)
	if obj == nil {
		w.WriteHeader(400)
		return
	}
	runID, ok1 := field[string](obj, "runId")
	resultToken, ok2 := field[string](obj, "resultToken")
	summary, ok3 := field[string](obj, "summary")
	if !ok1 || !ok2 || !ok3 {
		w.WriteHeader(400)
		return
	}
	a.mu.Lock()
	expected, had := a.automationTokens[runID]
	delete(a.automationTokens, runID) // single-use regardless of outcome
	a.mu.Unlock()
	if !had || subtle.ConstantTimeCompare([]byte(resultToken), []byte(expected)) != 1 {
		Log("warn", "automation report rejected: bad or already-used token", F{"runId", runID})
		w.WriteHeader(403)
		return
	}
	// Not delivered until a cluster link is up to relay it: a 503 tells the caller to retry.
	if !a.connected() {
		w.WriteHeader(503)
		return
	}
	ev := evAutomationResult{Event: "automations.result", RunID: runID, ResultToken: resultToken, Summary: summary}
	ev.BugIssueID, _ = field[string](obj, "bugIssueId")
	ev.MrURL, _ = field[string](obj, "mrUrl")
	a.broadcast(ev)
	Log("info", "automation report relayed", F{"runId", runID})
	reply(w, 200, map[string]bool{"ok": true})
}
