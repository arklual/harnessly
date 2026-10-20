// Command agentprobe talks to a machine agent the way the cluster does. `compat` runs a fixed script of requests and
// prints every answer, normalized, so two agents can be diffed; `bench` measures listing, reading and event latency.
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/ly-solutions/harnessly/internal/agent"
)

type client struct {
	conn    *websocket.Conn
	next    atomic.Int64
	mu      sync.Mutex
	pending map[int64]chan map[string]json.RawMessage
	events  chan map[string]json.RawMessage
	binary  chan []byte
}

func newClient(conn *websocket.Conn) *client {
	conn.SetReadLimit(agent.MaxPayload * 4)
	c := &client{conn: conn, pending: map[int64]chan map[string]json.RawMessage{}, events: make(chan map[string]json.RawMessage, 100000), binary: make(chan []byte, 10000)}
	go c.read()
	return c
}

func dial(addr, secretFile, audience string) (*client, error) {
	raw, err := os.ReadFile(secretFile)
	if err != nil {
		return nil, err
	}
	token := agent.SignHS256([]byte(strings.TrimSpace(string(raw))), "probe", audience, 5*time.Minute)
	h := http.Header{}
	h.Set("Authorization", "Bearer "+token)
	conn, _, err := websocket.Dial(context.Background(), "ws://"+addr+"/v1", &websocket.DialOptions{HTTPHeader: h, CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return nil, err
	}
	return newClient(conn), nil
}

func (c *client) read() {
	for {
		typ, data, err := c.conn.Read(context.Background())
		if err != nil {
			return
		}
		if typ != websocket.MessageText {
			select {
			case c.binary <- data:
			default:
			}
			continue
		}
		var m map[string]json.RawMessage
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		if idRaw, ok := m["id"]; ok {
			var id int64
			_ = json.Unmarshal(idRaw, &id)
			c.mu.Lock()
			ch := c.pending[id]
			delete(c.pending, id)
			c.mu.Unlock()
			if ch != nil {
				ch <- m
			}
			continue
		}
		select {
		case c.events <- m:
		default:
		}
	}
}

func (c *client) call(req map[string]any) map[string]json.RawMessage {
	id := c.next.Add(1)
	req["id"] = id
	ch := make(chan map[string]json.RawMessage, 1)
	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()
	raw, _ := json.Marshal(req)
	if err := c.conn.Write(context.Background(), websocket.MessageText, raw); err != nil {
		return map[string]json.RawMessage{"probeError": json.RawMessage(fmt.Sprintf("%q", err.Error()))}
	}
	select {
	case m := <-ch:
		return m
	case <-time.After(60 * time.Second):
		return map[string]json.RawMessage{"probeError": json.RawMessage(`"timeout"`)}
	}
}

// canonical re-encodes JSON with sorted keys, so two agents' answers compare as data.
func canonical(raw json.RawMessage) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return string(raw)
	}
	out, _ := json.Marshal(v)
	return string(out)
}

func show(label string, m map[string]json.RawMessage) {
	delete(m, "id")
	raw, _ := json.Marshal(m)
	fmt.Printf("%s\t%s\n", label, canonical(raw))
}

func compat(c *client, home, data string) {
	hello := c.call(map[string]any{"op": "hello"})
	var h map[string]any
	_ = json.Unmarshal(hello["result"], &h)
	delete(h, "agentVersion")
	hr, _ := json.Marshal(h)
	hello["result"] = hr
	show("hello", hello)

	list := c.call(map[string]any{"op": "transcripts.list"})
	var files []map[string]any
	_ = json.Unmarshal(list["result"], &files)
	sort.Slice(files, func(i, j int) bool { return files[i]["path"].(string) < files[j]["path"].(string) })
	lr, _ := json.Marshal(files)
	list["result"] = lr
	show("transcripts.list", list)

	for i, f := range files {
		if i%7 != 0 {
			continue
		}
		path := f["path"].(string)
		size := int64(f["size"].(float64))
		for _, window := range []int64{1, 4096, 1 << 20} {
			for _, off := range []int64{0, size / 3, size - 1, size, size + 10} {
				show(fmt.Sprintf("read %s @%d/%d", filepath.Base(path), off, window), c.call(map[string]any{"op": "transcripts.read", "path": path, "offset": off, "maxBytes": window}))
			}
		}
	}
	for _, bad := range []map[string]any{
		{"op": "transcripts.read", "path": "/etc/passwd", "offset": 0, "maxBytes": 10},
		{"op": "transcripts.read", "path": filepath.Join(home, ".claude", "projects", "..", "x.jsonl"), "offset": 0, "maxBytes": 10},
		{"op": "transcripts.read", "path": filepath.Join(home, ".claude", "projects", "nope", "missing.jsonl"), "offset": 0, "maxBytes": 10},
		{"op": "transcripts.read", "path": "relative.jsonl", "offset": 0, "maxBytes": 10},
		{"op": "transcripts.read", "path": filepath.Join(home, ".claude", "projects", "a.txt"), "offset": -5, "maxBytes": 1e12},
	} {
		show("bad read", c.call(bad))
	}

	show("state.list", c.call(map[string]any{"op": "state.list"}))
	show("put owned", c.call(map[string]any{"op": "state.put", "name": "quick-prompts.json", "content": `{"a":1}`}))
	show("put not owned", c.call(map[string]any{"op": "state.put", "name": "../x.json", "content": `{}`}))
	show("put bad json", c.call(map[string]any{"op": "state.put", "name": "keybindings.json", "content": `{`}))
	_ = os.MkdirAll(filepath.Join(data, "workspaces", "w1"), 0o700)
	_ = os.WriteFile(filepath.Join(data, "workspaces", "w1", "layout.json"), []byte(`{"stateVersion":5}`), 0o600)
	show("put layout older", c.call(map[string]any{"op": "state.put", "name": "workspaces/w1/layout.json", "content": `{"stateVersion":4}`, "version": 4}))
	show("put layout same", c.call(map[string]any{"op": "state.put", "name": "workspaces/w1/layout.json", "content": `{"stateVersion":5}`, "version": 5}))
	show("put layout newer", c.call(map[string]any{"op": "state.put", "name": "workspaces/w1/layout.json", "content": `{"stateVersion":6}`, "version": 6}))
	show("put layout renumber", c.call(map[string]any{"op": "state.put", "name": "workspaces/w1/layout.json", "content": `{"stateVersion":1}`, "version": 1, "renumber": true}))
	_ = os.WriteFile(filepath.Join(data, "workspaces", "w1", "layout.json"), []byte(`{"stateVersion":9,"stateDirty":true}`), 0o600)
	show("put layout dirty", c.call(map[string]any{"op": "state.put", "name": "workspaces/w1/layout.json", "content": `{"stateVersion":10}`, "version": 10}))
	show("put layout dirty match", c.call(map[string]any{"op": "state.put", "name": "workspaces/w1/layout.json", "content": `{"stateVersion":10}`, "version": 10,
		"replacesDirty": "1f7b1a0e8d2b8a8d0c0f2f0a0a7c6f1d9bd7e6c3b1d9d6f9f0f8c2e8d6a3b1c4"}))
	show("state.list after", c.call(map[string]any{"op": "state.list"}))
	m := c.call(map[string]any{"op": "state.mode", "owned": true, "headless": true})
	show("state.mode", m)
	show("state.result unknown", c.call(map[string]any{"op": "state.result", "requestId": "x", "ok": true}))
	_ = os.MkdirAll(filepath.Join(data, "eval", "runs", "00000000-0000-0000-0000-000000000001"), 0o700)
	_ = os.WriteFile(filepath.Join(data, "eval", "runs", "00000000-0000-0000-0000-000000000001", "state.json"), []byte("{\n  \"phase\": \"done\" }\n"), 0o600)
	show("eval.poll", c.call(map[string]any{"op": "eval.poll", "runIds": []string{"00000000-0000-0000-0000-000000000001", "bad", "00000000-0000-0000-0000-000000000002"}}))
	show("eval.stop bad", c.call(map[string]any{"op": "eval.stop", "runId": "nope"}))
	show("eval.stop", c.call(map[string]any{"op": "eval.stop", "runId": "00000000-0000-0000-0000-000000000001"}))
	show("docs.deliver", c.call(map[string]any{"op": "docs.deliver", "message": "x"}))
	show("conversations.start", c.call(map[string]any{"op": "conversations.start", "prompt": "x"}))
	show("status.send", c.call(map[string]any{"op": "status.send", "message": map[string]any{"type": "status:ack"}}))
	show("tunnel bad id", c.call(map[string]any{"op": "tunnel.open", "spec": map[string]any{"tunnelId": "zz", "target": "runtime"}}))
	show("tunnel bad target", c.call(map[string]any{"op": "tunnel.open", "spec": map[string]any{"tunnelId": strings.Repeat("a", 32), "target": "disk"}}))
	show("tunnel no runtime", c.call(map[string]any{"op": "tunnel.open", "spec": map[string]any{"tunnelId": strings.Repeat("a", 32), "target": "runtime"}}))
	show("unknown", c.call(map[string]any{"op": "frobnicate"}))
}

func bench(c *client, home string, readBytes int64) {
	start := time.Now()
	var files []agent.TranscriptFile
	for i := 0; i < 5; i++ {
		list := c.call(map[string]any{"op": "transcripts.list"})
		_ = json.Unmarshal(list["result"], &files)
	}
	fmt.Printf("list\t%d files\t%.1f ms per call\n", len(files), float64(time.Since(start).Microseconds())/5000)

	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	var total int64
	start = time.Now()
	for _, f := range files {
		if total >= readBytes {
			break
		}
		var off int64
		window := int64(1 << 20)
		for off < f.Size {
			res := c.call(map[string]any{"op": "transcripts.read", "path": f.Path, "offset": off, "maxBytes": window})
			var chunk agent.ReadChunk
			if json.Unmarshal(res["result"], &chunk) != nil {
				break
			}
			if chunk.LineTooLong {
				window *= 4
				continue
			}
			if chunk.Next == off {
				break
			}
			total += chunk.Next - off
			off = chunk.Next
		}
	}
	el := time.Since(start)
	fmt.Printf("read\t%d MB\t%.2f s\t%.0f MB/s\n", total>>20, el.Seconds(), float64(total)/(1<<20)/el.Seconds())

	// Event latency: lines appended to a fresh transcript, time until the change event carrying them arrives.
	dir := filepath.Join(home, ".claude", "projects", "probe-latency")
	_ = os.MkdirAll(dir, 0o700)
	file := filepath.Join(dir, fmt.Sprintf("probe-%d.jsonl", time.Now().UnixNano()))
	f, _ := os.Create(file)
	defer f.Close()
	_, _ = f.WriteString("{\"seed\":true}\n")
	time.Sleep(500 * time.Millisecond)
	for len(c.events) > 0 {
		<-c.events
	}
	var lat []time.Duration
	withLines := 0
	for i := 0; i < 40; i++ {
		t0 := time.Now()
		_, _ = fmt.Fprintf(f, "{\"n\":%d,\"pad\":%q}\n", i, strings.Repeat("x", 200))
		deadline := time.After(5 * time.Second)
	wait:
		for {
			select {
			case ev := <-c.events:
				var e struct {
					Event string                 `json:"event"`
					Files []agent.TranscriptFile `json:"files"`
				}
				_ = json.Unmarshal(mustJSON(ev), &e)
				for _, tf := range e.Files {
					if tf.Path == file {
						lat = append(lat, time.Since(t0))
						if tf.Appended != nil {
							withLines++
						}
						break wait
					}
				}
			case <-deadline:
				lat = append(lat, 5*time.Second)
				break wait
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
	fmt.Printf("event\tp50 %.1f ms\tp95 %.1f ms\tmax %.1f ms\twith lines %d/%d\n", ms(lat[len(lat)/2]), ms(lat[len(lat)*95/100]), ms(lat[len(lat)-1]), withLines, len(lat))
	_ = os.RemoveAll(dir)
}

func mustJSON(m map[string]json.RawMessage) []byte { raw, _ := json.Marshal(m); return raw }
func ms(d time.Duration) float64                   { return float64(d.Microseconds()) / 1000 }

func main() {
	addr := flag.String("addr", "127.0.0.1:28099", "agent address")
	secret := flag.String("secret", "", "file with the pair's secret")
	audience := flag.String("aud", "agent", "audience the agent expects on /v1")
	home := flag.String("home", os.Getenv("HOME"), "the agent's HOME")
	data := flag.String("data", "", "the agent's data directory")
	mode := flag.String("mode", "compat", "compat | bench")
	readMB := flag.Int64("read-mb", 500, "bench: how much to read")
	listen := flag.String("listen", "", "runtime | ingest: address to serve on")
	logFile := flag.String("log", "runtime.log", "runtime: where to record the calls")
	header := flag.String("header", "x-agent-local-token", "the agent's local token header")
	runtimeID := flag.String("runtime-id", "rt-1", "ingest: the runtime id the agent must sign")
	api := flag.String("api", "/api/agent", "runtime: the prefix of the routes the agent calls (AGENT_RUNTIME_API)")
	flag.Parse()
	raw, _ := os.ReadFile(*secret)
	key := []byte(strings.TrimSpace(string(raw)))
	switch *mode {
	case "runtime":
		serveRuntime(*listen, *api, key, *logFile)
		return
	case "ingest":
		serveIngest(*listen, key, *audience, *runtimeID, func(c *client) {
			compat(c, *home, *data)
			withRuntime(c, *data, *header)
		})
		return
	}
	c, err := dial(*addr, *secret, *audience)
	if err != nil {
		fmt.Fprintln(os.Stderr, "dial:", err)
		os.Exit(1)
	}
	switch *mode {
	case "compat":
		compat(c, *home, *data)
	case "bench":
		bench(c, *home, *readMB<<20)
	case "scenario":
		withRuntime(c, *data, *header)
	}
}

func b64url(s string) ([]byte, error) { return base64.RawURLEncoding.DecodeString(s) }
