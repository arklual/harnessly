package providers

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

// Relay is the Responses endpoint a Codex chat on a relayed profile (xAI) talks to, on the runtime's own address. It
// puts the credential on each request (the saved key, else the vendor CLI's sign-in), so Codex holds none, and it
// turns what Codex sends into what the vendor takes:
//
//   - Codex sends the tools of an MCP server, an app or its sub-agents as one `namespace` tool each, which xAI refuses
//     (422). Each tool of a namespace goes up as a plain function `<namespace>__<tool>`, the namespace's description
//     joins the instructions, and the calls coming back are given their namespace again, so Codex sees what it sent.
//   - `web_search` goes without `external_web_access`, which xAI does not take (400).
//   - a reasoning item of the history goes without its null fields: xAI reads `content: null` as a broken
//     compaction blob (400).
type Relay struct {
	// Upstream is the vendor's `/v1` address.
	Upstream string
	// Prefix is the path the relay is mounted on, taken off before the rest is put after Upstream.
	Prefix string
	// Credential is the bearer token of one request.
	Credential func(ctx context.Context) (string, error)
	// Client sends the requests up; nil is one without a total timeout (a turn streams for minutes) that follows no
	// redirect.
	Client *http.Client
}

// RelayBodyMax bounds a request Codex sends: a long chat with every tool runs to a few megabytes.
const RelayBodyMax = 64 << 20

// flatSep joins a namespace and a tool name.
const flatSep = "__"

// forwarded are the headers of Codex's request that go up with it.
var forwarded = []string{"Content-Type", "Accept", "OpenAI-Beta"}

func (rl *Relay) client() *http.Client {
	if rl.Client != nil {
		return rl.Client
	}
	return &http.Client{
		Transport:     &http.Transport{Proxy: http.ProxyFromEnvironment, ResponseHeaderTimeout: 5 * time.Minute, TLSHandshakeTimeout: 15 * time.Second},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func (rl *Relay) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, rl.Prefix)
	if rest == r.URL.Path && rl.Prefix != "" || strings.Contains(rest, "..") {
		http.NotFound(w, r)
		return
	}
	var body []byte
	var names map[string]flatTool
	if r.Method == http.MethodPost {
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, RelayBodyMax))
		if err != nil {
			http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
			return
		}
		body = raw
		if strings.HasSuffix(rest, "/responses") {
			if out, n, err := RewriteRequest(raw); err == nil {
				body, names = out, n
			}
		}
	}
	token, err := rl.Credential(r.Context())
	if err != nil {
		relayError(w, http.StatusUnauthorized, "no credential for the vendor: "+err.Error())
		return
	}
	url := strings.TrimRight(rl.Upstream, "/") + rest
	if r.URL.RawQuery != "" {
		url += "?" + r.URL.RawQuery
	}
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(r.Context(), r.Method, url, rd)
	if err != nil {
		relayError(w, http.StatusBadGateway, err.Error())
		return
	}
	for _, h := range forwarded {
		if v := r.Header.Get(h); v != "" {
			req.Header.Set(h, v)
		}
	}
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := rl.client().Do(req)
	if err != nil {
		relayError(w, http.StatusBadGateway, "the vendor did not answer")
		return
	}
	defer res.Body.Close()
	ct := res.Header.Get("Content-Type")
	for _, h := range []string{"Content-Type", "Retry-After", "X-Request-Id"} {
		if v := res.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.WriteHeader(res.StatusCode)
	if !strings.Contains(ct, "event-stream") {
		data, _ := io.ReadAll(res.Body)
		if len(names) > 0 && res.StatusCode < 300 {
			data = restoreJSON(data, names)
		}
		_, _ = w.Write(data)
		return
	}
	flusher, _ := w.(http.Flusher)
	br := bufio.NewReaderSize(res.Body, 64<<10)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			if len(names) > 0 && bytes.HasPrefix(line, []byte("data: ")) {
				line = restoreEvent(line, names)
			}
			if _, werr := w.Write(line); werr != nil {
				return
			}
			if flusher != nil && (len(line) == 1 || bytes.Equal(line, []byte("\r\n"))) {
				flusher.Flush()
			}
		}
		if err != nil {
			if flusher != nil {
				flusher.Flush()
			}
			return
		}
	}
}

func relayError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"message": msg, "type": "relay"}})
}

// flatTool is the namespace and name a flat function stands for.
type flatTool struct{ Namespace, Name string }

// FlatName is the function name of a namespace's tool: `<namespace>__<tool>`, cut to the 64 characters a function name
// may have and told apart by a hash where it is cut.
func FlatName(namespace, name string) string {
	n := strings.TrimRight(namespace, "_") + flatSep + name
	if len(n) <= 64 {
		return n
	}
	sum := sha1.Sum([]byte(n))
	return n[:55] + "_" + hex.EncodeToString(sum[:])[:8]
}

// RewriteRequest turns a Responses request of Codex into one xAI takes (see Relay) and answers which flat function
// names stand for which namespaced tools.
func RewriteRequest(raw []byte) ([]byte, map[string]flatTool, error) {
	var b map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&b); err != nil {
		return nil, nil, err
	}
	if b == nil {
		return nil, nil, errors.New("not an object")
	}
	names := map[string]flatTool{}
	var notes []string
	if tools, ok := b["tools"].([]any); ok {
		out := make([]any, 0, len(tools))
		for _, t := range tools {
			tool, ok := t.(map[string]any)
			if !ok {
				out = append(out, t)
				continue
			}
			switch tool["type"] {
			case "web_search":
				out = append(out, map[string]any{"type": "web_search"})
			case "namespace":
				ns, _ := tool["name"].(string)
				if d, _ := tool["description"].(string); strings.TrimSpace(d) != "" {
					d = strings.TrimSpace(d)
					if len(d) > 4000 {
						d = strings.ToValidUTF8(d[:4000], "")
					}
					notes = append(notes, "## Tools "+strings.TrimRight(ns, "_")+flatSep+"*\n"+d)
				}
				inner, _ := tool["tools"].([]any)
				for _, x := range inner {
					fn, ok := x.(map[string]any)
					if !ok || fn["type"] != "function" {
						continue
					}
					name, _ := fn["name"].(string)
					flat := make(map[string]any, len(fn))
					for k, v := range fn {
						flat[k] = v
					}
					fname := FlatName(ns, name)
					flat["name"] = fname
					names[fname] = flatTool{ns, name}
					out = append(out, flat)
				}
			default:
				out = append(out, tool)
			}
		}
		b["tools"] = out
	}
	if len(notes) > 0 {
		ins, _ := b["instructions"].(string)
		b["instructions"] = ins + "\n\n" + strings.Join(notes, "\n\n")
	}
	back := make(map[flatTool]string, len(names))
	for k, v := range names {
		back[v] = k
	}
	if input, ok := b["input"].([]any); ok {
		for _, it := range input {
			item, ok := it.(map[string]any)
			if !ok {
				continue
			}
			switch item["type"] {
			case "function_call":
				ns, _ := item["namespace"].(string)
				if ns == "" {
					break
				}
				name, _ := item["name"].(string)
				if flat, ok := back[flatTool{ns, name}]; ok {
					item["name"] = flat
				} else {
					item["name"] = FlatName(ns, name)
				}
				delete(item, "namespace")
			case "reasoning":
				for k, v := range item {
					if v == nil {
						delete(item, k)
					}
				}
			}
		}
	}
	delete(b, "client_metadata")
	out, err := json.Marshal(b)
	return out, names, err
}

// restoreItem gives a call of a flat function its namespace again.
func restoreItem(v any, names map[string]flatTool) bool {
	item, ok := v.(map[string]any)
	if !ok || item["type"] != "function_call" {
		return false
	}
	name, _ := item["name"].(string)
	t, ok := names[name]
	if !ok {
		return false
	}
	item["namespace"], item["name"] = t.Namespace, t.Name
	return true
}

// restoreObject restores the calls of an event (`item`) or of a whole response (`response.output`, `output`).
func restoreObject(ev map[string]any, names map[string]flatTool) bool {
	changed := restoreItem(ev["item"], names)
	outputs := []any{ev["output"]}
	if resp, ok := ev["response"].(map[string]any); ok {
		outputs = append(outputs, resp["output"])
	}
	for _, o := range outputs {
		list, _ := o.([]any)
		for _, it := range list {
			if restoreItem(it, names) {
				changed = true
			}
		}
	}
	return changed
}

func decodeObject(raw []byte) (map[string]any, bool) {
	var ev map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if dec.Decode(&ev) != nil || ev == nil {
		return nil, false
	}
	return ev, true
}

// restoreEvent is a `data: ` line of the stream with its calls restored; the line itself when it has none.
func restoreEvent(line []byte, names map[string]flatTool) []byte {
	payload := bytes.TrimRight(line[len("data: "):], "\r\n")
	ev, ok := decodeObject(payload)
	if !ok || !restoreObject(ev, names) {
		return line
	}
	out, err := json.Marshal(ev)
	if err != nil {
		return line
	}
	return append(append([]byte("data: "), out...), '\n')
}

func restoreJSON(data []byte, names map[string]flatTool) []byte {
	ev, ok := decodeObject(data)
	if !ok || !restoreObject(ev, names) {
		return data
	}
	out, err := json.Marshal(ev)
	if err != nil {
		return data
	}
	return out
}

// RelayPrefix is where the runtime mounts the relays: <RelayPrefix>/<profile id>/v1/….
const RelayPrefix = "/api/cli/providers"

// Relays serves the relay of every Relayed preset under prefix, each over the store's credential of its profile.
func Relays(store *Store, prefix string) http.Handler {
	mux := map[string]*Relay{}
	for _, p := range presets {
		if !p.Relayed {
			continue
		}
		id := p.ID
		mux[id] = &Relay{Upstream: p.BaseURL, Prefix: RelayPath(prefix, id),
			Credential: func(ctx context.Context) (string, error) { return store.Credential(ctx, id) }}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, _, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, prefix+"/"), "/")
		rl, ok := mux[id]
		if !ok || !strings.HasPrefix(r.URL.Path, RelayPath(prefix, id)+"/") {
			http.NotFound(w, r)
			return
		}
		rl.ServeHTTP(w, r)
	})
}
