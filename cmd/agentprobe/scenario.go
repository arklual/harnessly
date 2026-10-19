package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/coder/websocket"

	"harnessly/internal/agent"
)

// waitEvent returns the first event of the given kind within d, setting aside the others.
func (c *client) waitEvent(kind string, d time.Duration) map[string]json.RawMessage {
	deadline := time.After(d)
	for {
		select {
		case ev := <-c.events:
			var k string
			_ = json.Unmarshal(ev["event"], &k)
			if k == kind {
				return ev
			}
		case <-deadline:
			return map[string]json.RawMessage{"probeError": json.RawMessage(`"no ` + kind + ` event"`)}
		}
	}
}

var volatile = regexp.MustCompile(`"(requestId|n|time|at)":("[^"]*"|\d+)`)

func showEvent(label string, ev map[string]json.RawMessage) {
	raw, _ := json.Marshal(ev)
	fmt.Printf("%s\t%s\n", label, volatile.ReplaceAllString(canonical(raw), `"$1":"*"`))
}

type local struct {
	port  int
	token string
}

func readLocal(data string) local {
	raw, _ := os.ReadFile(filepath.Join(data, "agent.json"))
	var l local
	var v struct {
		Port  int    `json:"port"`
		Token string `json:"token"`
	}
	_ = json.Unmarshal(raw, &v)
	l.port, l.token = v.Port, v.Token
	return l
}

func (l local) post(path, header string, token string, body string) string {
	req, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:%d%s", l.port, path), strings.NewReader(body))
	if token != "" {
		req.Header.Set(header, token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "error " + err.Error()
	}
	defer res.Body.Close()
	out, _ := io.ReadAll(res.Body)
	return fmt.Sprintf("%d %s", res.StatusCode, canonical(out))
}

// withRuntime exercises everything that goes through the runtime and the loopback routes.
func withRuntime(c *client, data, header string) {
	_ = os.WriteFile(filepath.Join(data, "cli-token"), []byte("cli-secret\n"), 0o600)
	// Relays: the first status and sync messages, raw, to see escaping is left alone.
	for _, kind := range []string{"status.message", "sync.message"} {
		ev := c.waitEvent(kind, 5*time.Second)
		fmt.Printf("relay %s raw\t%s\n", kind, volatile.ReplaceAllString(string(ev["message"]), `"$1":"*"`))
	}
	show("status.send", c.call(map[string]any{"op": "status.send", "message": map[string]any{"type": "status:dismiss", "tabId": "t"}}))
	show("status.send bad", c.call(map[string]any{"op": "status.send", "message": map[string]any{"type": "bogus"}}))
	show("status.send no type", c.call(map[string]any{"op": "status.send", "message": "x"}))
	show("conversations.start run", c.call(map[string]any{"op": "conversations.start", "prompt": "p", "env": map[string]string{"A": "1"}, "runId": "r1", "resultToken": "tok-1", "cwd": nil}))
	show("conversations.start plain", c.call(map[string]any{"op": "conversations.start", "prompt": "q"}))
	show("docs.notify 500", c.call(map[string]any{"op": "docs.notify", "title": "t", "body": "b", "url": "u", "tag": "g"}))
	show("docs.deliver 502", c.call(map[string]any{"op": "docs.deliver", "message": "m"}))
	show("eval.start", c.call(map[string]any{"op": "eval.start", "x": 1}))
	show("eval.stop", c.call(map[string]any{"op": "eval.stop", "runId": "00000000-0000-0000-0000-000000000009"}))
	show("tunnel files", c.call(map[string]any{"op": "tunnel.open", "spec": map[string]any{"tunnelId": strings.Repeat("b", 32), "target": "files"}}))

	id := strings.Repeat("c", 32)
	show("tunnel open", c.call(map[string]any{"op": "tunnel.open", "spec": map[string]any{"tunnelId": id, "target": "runtime"}}))
	request := "GET /health HTTP/1.1\r\nHost: probe\r\nConnection: close\r\n\r\n"
	_ = c.conn.Write(context.Background(), websocket.MessageBinary, agent.EncodeFrame(agent.FrameData, id, []byte(request)))
	var got bytes.Buffer
	closed := false
	deadline := time.After(5 * time.Second)
	for !closed {
		select {
		case frame := <-c.binary:
			kind, fid, payload, ok := agent.DecodeFrame(frame)
			if !ok || fid != id {
				continue
			}
			if kind == agent.FrameClose {
				closed = true
			} else {
				got.Write(payload)
			}
		case <-deadline:
			closed = true
			got.WriteString(" [no close frame]")
		}
	}
	resp := regexp.MustCompile(`(?m)^Date: .*\r\n`).ReplaceAllString(got.String(), "")
	fmt.Printf("tunnel bytes\t%q\n", resp)
	show("tunnel reuse closed id", c.call(map[string]any{"op": "tunnel.open", "spec": map[string]any{"tunnelId": id, "target": "runtime"}}))

	l := readLocal(data)
	fmt.Printf("bind no token\t%s\n", l.post("/docs/bind", header, "", `{}`))
	fmt.Printf("bind bad json\t%s\n", l.post("/docs/bind", header, l.token, `{`))
	fmt.Printf("bind board no key\t%s\n", l.post("/docs/bind", header, l.token, `{"slug":"s","title":"t","kind":"board"}`))
	fmt.Printf("bind\t%s\n", l.post("/docs/bind", header, l.token, `{"slug":"doc-1","title":"  Title  ","tmuxPane":"%3","sourcePath":"/x.html"}`))
	showEvent("bind event", c.waitEvent("docs.bound", 3*time.Second))
	fmt.Printf("bind board\t%s\n", l.post("/docs/bind", header, l.token, `{"slug":"b","title":"B","kind":"board","key":"k","cwd":"/c"}`))
	showEvent("bind board event", c.waitEvent("docs.bound", 3*time.Second))
	fmt.Printf("reply empty\t%s\n", l.post("/docs/reply", header, l.token, `{"threadId":"t1","body":"   "}`))
	fmt.Printf("reply\t%s\n", l.post("/docs/reply", header, l.token, `{"threadId":"t1","body":"  answer  ","resolve":true}`))
	showEvent("reply event", c.waitEvent("docs.reply", 3*time.Second))

	done := make(chan string, 1)
	go func() {
		done <- l.post("/state/commit", header, l.token, `{"name":"workspaces.json","base":"4","content":{"a":[1]}}`)
	}()
	ev := c.waitEvent("state.commit", 3*time.Second)
	showEvent("commit event", ev)
	var reqID string
	_ = json.Unmarshal(ev["requestId"], &reqID)
	show("state.result", c.call(map[string]any{"op": "state.result", "requestId": reqID, "ok": true, "version": 7, "content": map[string]any{"a": 1}}))
	fmt.Printf("commit\t%s\n", <-done)
	go func() {
		done <- l.post("/state/patch", header, l.token, `{"name":"workspaces/w0/layout.json","sessionName":"s","set":{"x":1},"unset":["y",2]}`)
	}()
	ev = c.waitEvent("state.patch", 3*time.Second)
	showEvent("patch event", ev)
	_ = json.Unmarshal(ev["requestId"], &reqID)
	show("state.result fail", c.call(map[string]any{"op": "state.result", "requestId": reqID, "ok": false, "error": "conflict", "content": nil}))
	fmt.Printf("patch\t%s\n", <-done)
	fmt.Printf("patch no session\t%s\n", l.post("/state/patch", header, l.token, `{"name":"x"}`))
	fmt.Printf("commit no name\t%s\n", l.post("/state/commit", header, l.token, `{"base":1}`))

	fmt.Printf("report bad token\t%s\n", l.post("/automations/report", "", "", `{"runId":"r1","resultToken":"nope","summary":"s"}`))
	show("conversations.start run again", c.call(map[string]any{"op": "conversations.start", "prompt": "p", "runId": "r2", "resultToken": "tok-2"}))
	fmt.Printf("report\t%s\n", l.post("/automations/report", "", "", `{"runId":"r2","resultToken":"tok-2","summary":"done","mrUrl":"https://mr"}`))
	showEvent("report event", c.waitEvent("automations.result", 3*time.Second))
	fmt.Printf("report replay\t%s\n", l.post("/automations/report", "", "", `{"runId":"r2","resultToken":"tok-2","summary":"done"}`))
	fmt.Printf("report bad body\t%s\n", l.post("/automations/report", "", "", `{"runId":1}`))
	fmt.Printf("unknown route\t%s\n", l.post("/nope", "", "", `{}`))

	show("put layout notifies", c.call(map[string]any{"op": "state.put", "name": "workspaces.json", "content": `[{"id":"w0"},{"id":"w9"}]`, "version": 99}))
	showEvent("machine", c.waitEvent("machine", 20*time.Second))
}
