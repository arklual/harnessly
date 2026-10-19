package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const (
	maxRead       = 64 * 1024 * 1024
	machineEvery  = 15 * time.Second
	stateEvery    = time.Second
	runtimeCallTO = 15 * time.Second
	machineCallTO = 8 * time.Second
)

var evalRunID = regexp.MustCompile(`^[0-9a-f-]{36}$`)

// Agent is the machine end of the cluster link for one Unix account.
type Agent struct {
	cfg    Config
	secret []byte
	roots  Roots
	hello  Hello
	client *http.Client

	tunnels     *tunnels
	state       *stateFiles
	transcripts *transcriptWatch

	mu      sync.Mutex
	clients map[*link]struct{}
	// automationTokens: runId → its one result token, consumed on first use (valid or not) and lost on restart.
	automationTokens map[string]string
	// pendingStates: a runtime's state commit or patch waiting for the cluster's state.result.
	pendingStates map[string]chan StateResult

	statusMu sync.Mutex
	status   *websocket.Conn

	localToken string
}

// New prepares the agent; Run starts it.
func New(cfg Config) (*Agent, error) {
	secret, err := cfg.LoadSecret()
	if err != nil {
		return nil, err
	}
	username := ""
	if u, err := user.Current(); err == nil {
		username = u.Username
	}
	hostname, _ := os.Hostname()
	features := []string{"transcripts", "state", "machine", "automations", "docs"}
	if cfg.RuntimePort != 0 {
		features = append(features, "status", "sync", "tunnel")
	}
	a := &Agent{
		cfg:              cfg,
		secret:           secret,
		roots:            TranscriptRoots(cfg.Home),
		hello:            Hello{Protocol: Protocol, AgentVersion: cfg.Version, User: username, Home: cfg.Home, Hostname: hostname, Features: features},
		client:           &http.Client{},
		tunnels:          newTunnels(cfg.RuntimePort, cfg.FilesPort),
		clients:          map[*link]struct{}{},
		automationTokens: map[string]string{},
		pendingStates:    map[string]chan StateResult{},
	}
	a.state = newStateFiles(cfg.DataDir, cfg.ExtraStateFiles, a.broadcast, a.notifyRuntimeOfState)
	a.transcripts = newTranscriptWatch(a.roots, a.broadcast)
	return a, nil
}

func (a *Agent) broadcast(ev any) {
	frame, err := marshal(ev)
	if err != nil {
		Log("error", "event not encoded", F{"error", err.Error()})
		return
	}
	a.mu.Lock()
	links := make([]*link, 0, len(a.clients))
	for l := range a.clients {
		links = append(links, l)
	}
	a.mu.Unlock()
	for _, l := range links {
		l.sendText(frame)
	}
}

func (a *Agent) connected() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.clients) > 0
}

// serve runs one cluster session, whichever side dialled: the cluster asks, the agent answers and sends its events.
// It returns the error that ended the session; every frame read is signalled on heard, if given.
func (a *Agent) serve(conn *websocket.Conn, heard chan<- struct{}) error {
	conn.SetReadLimit(MaxPayload)
	l := newLink(conn)
	a.mu.Lock()
	a.clients[l] = struct{}{}
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		delete(a.clients, l)
		a.mu.Unlock()
		if n := a.tunnels.DropLink(l); n > 0 {
			Log("info", "tunnels closed with the link", F{"tunnels", n})
		}
		l.close(websocket.StatusNormalClosure, "")
	}()
	for {
		typ, data, err := conn.Read(context.Background())
		if err != nil {
			return err
		}
		if heard != nil {
			select {
			case heard <- struct{}{}:
			default:
			}
		}
		if typ == websocket.MessageBinary {
			a.tunnels.Frame(data)
			continue
		}
		var req map[string]json.RawMessage
		if json.Unmarshal(data, &req) != nil {
			continue
		}
		go a.answer(l, req)
	}
}

type okResponse struct {
	ID     json.RawMessage `json:"id"`
	OK     bool            `json:"ok"`
	Result any             `json:"result"`
}

type errResponse struct {
	ID    json.RawMessage `json:"id"`
	OK    bool            `json:"ok"`
	Error string          `json:"error"`
}

func (a *Agent) answer(l *link, req map[string]json.RawMessage) {
	id := req["id"]
	if id == nil {
		id = json.RawMessage("null")
	}
	result, after, err := a.handle(l, req)
	var frame []byte
	if encode, ok := result.(frameEncoder); ok && err == nil {
		l.sendText(encode(id))
		return
	}
	if err != nil {
		frame, _ = marshal(errResponse{ID: id, OK: false, Error: err.Error()})
	} else if frame, err = marshal(okResponse{ID: id, OK: true, Result: result}); err != nil {
		frame, _ = marshal(errResponse{ID: id, OK: false, Error: err.Error()})
		after = nil
	}
	l.sendText(frame)
	if after != nil {
		after()
	}
}

// frameEncoder is a result that writes its own response frame, for the one answer big enough to matter.
type frameEncoder func(id json.RawMessage) []byte

func field[T any](req map[string]json.RawMessage, name string) (T, bool) {
	var v T
	raw, ok := req[name]
	if !ok {
		return v, false
	}
	if json.Unmarshal(raw, &v) != nil {
		return v, false
	}
	return v, true
}

// body is the request without its op and id: what a relayed operation hands the runtime.
func body(req map[string]json.RawMessage, drop ...string) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(req))
	for k, v := range req {
		out[k] = v
	}
	for _, k := range append([]string{"op", "id"}, drop...) {
		delete(out, k)
	}
	return out
}

func clamp(v float64, lo, hi float64) float64 { return math.Min(math.Max(v, lo), hi) }

func (a *Agent) handle(l *link, req map[string]json.RawMessage) (any, func(), error) {
	op, _ := field[string](req, "op")
	route := func(name string) string { return a.cfg.RuntimeAPI + "/" + name }
	switch op {
	case "tunnel.open":
		spec, _ := field[TunnelOpen](req, "spec")
		start, err := a.tunnels.Open(l, spec)
		if err != nil {
			return nil, nil, err
		}
		return map[string]bool{"opened": true}, start, nil
	case "hello":
		return a.hello, nil, nil
	case "transcripts.list":
		files, err := ListTranscripts(a.roots)
		return files, nil, err
	case "state.list":
		return a.state.List(), nil, nil
	case "state.put":
		name, _ := field[string](req, "name")
		content, _ := field[string](req, "content")
		var version *float64
		if v, ok := field[float64](req, "version"); ok {
			version = &v
		}
		replacesDirty, _ := field[string](req, "replacesDirty")
		renumber, _ := field[bool](req, "renumber")
		res, err := a.state.Put(name, content, version, replacesDirty, renumber)
		return res, nil, err
	case "state.mode":
		owned, _ := field[bool](req, "owned")
		headless, _ := field[bool](req, "headless")
		goSlices, _ := field[[]string](req, "goSlices")
		res, err := a.state.WriteMode(owned, headless, goSlices)
		return res, nil, err
	case "state.result":
		requestID, _ := field[string](req, "requestId")
		var res StateResult
		res.OK, _ = field[bool](req, "ok")
		if v, ok := field[float64](req, "version"); ok {
			res.Version = &v
		}
		if raw, ok := req["content"]; ok {
			res.Content = raw
		}
		if e, ok := field[string](req, "error"); ok {
			res.Error = &e
		}
		a.settleState(requestID, res)
		return map[string]bool{"settled": true}, nil, nil
	case "transcripts.read":
		path, _ := field[string](req, "path")
		if !a.roots.IsTranscriptPath(path) {
			return nil, nil, errors.New("path is not a transcript of this account")
		}
		maxBytes, _ := field[float64](req, "maxBytes")
		offset, _ := field[float64](req, "offset")
		chunk, data, release, err := readLines(path, int64(math.Max(math.Trunc(offset), 0)), int64(clamp(math.Trunc(maxBytes), 1, maxRead)))
		if err != nil {
			return nil, nil, err
		}
		frame := encodeReadAnswer
		return frameEncoder(func(id json.RawMessage) []byte {
			defer release()
			return frame(id, chunk, data)
		}), nil, nil
	case "conversations.start":
		runID, _ := field[string](req, "runId")
		resultToken, _ := field[string](req, "resultToken")
		b := body(req, "runId", "resultToken")
		if runID != "" {
			// The conversation reports back to this process over loopback, so the port is the agent's own.
			env, _ := field[map[string]json.RawMessage](req, "env")
			if env == nil {
				env = map[string]json.RawMessage{}
			}
			port, _ := json.Marshal(strconv.Itoa(a.cfg.Port))
			env[a.cfg.ReportPortEnv] = port
			raw, _ := json.Marshal(env)
			b["env"] = raw
		}
		started, err := a.callRuntime(route("automations/start"), b)
		if err != nil {
			return nil, nil, err
		}
		// Kept in memory only, never written or logged: the one thing that authorizes this run's report.
		if runID != "" && resultToken != "" {
			a.mu.Lock()
			a.automationTokens[runID] = resultToken
			a.mu.Unlock()
		}
		return started, nil, nil
	case "docs.deliver":
		res, err := a.callRuntime(route("docs/deliver"), body(req))
		return res, nil, err
	case "docs.notify":
		res, err := a.callRuntime(route("docs/notify"), body(req))
		return res, nil, err
	case "eval.start":
		res, err := a.callRuntime(route("eval/start"), body(req))
		return res, nil, err
	case "eval.poll":
		// Read straight from the runner's files: read-only for the agent.
		ids, _ := field[[]string](req, "runIds")
		states := map[string]json.RawMessage{}
		taken := 0
		for _, id := range ids {
			if !evalRunID.MatchString(id) {
				continue
			}
			if taken++; taken > 50 {
				break
			}
			raw, err := os.ReadFile(filepath.Join(a.cfg.DataDir, "eval", "runs", id, "state.json"))
			if err != nil || !json.Valid(raw) {
				continue // caught between the runner's two writes; the next poll reads it whole
			}
			states[id] = bytes.TrimSpace(raw)
		}
		return states, nil, nil
	case "status.send":
		res, err := a.sendStatus(req["message"])
		return res, nil, err
	case "eval.stop":
		runID, _ := field[string](req, "runId")
		if !evalRunID.MatchString(runID) {
			return nil, nil, errors.New("bad run id")
		}
		res, err := a.callRuntime(route("eval/start")+"?stop="+runID, map[string]any{})
		return res, nil, err
	default:
		return nil, nil, fmt.Errorf("unknown operation %s", op)
	}
}

func (a *Agent) runtimeURL(path string) string {
	return "http://127.0.0.1:" + strconv.Itoa(a.cfg.RuntimePort) + path
}

func (a *Agent) runtimeCookie() string {
	return a.cfg.RuntimeCookie + "=" + SignHS256(a.secret, "machine-agent", "", 2*time.Minute)
}

// callRuntime posts to the runtime next door over loopback, signed with the pair's secret as a page session would be.
func (a *Agent) callRuntime(route string, payload any) (json.RawMessage, error) {
	if a.cfg.RuntimePort == 0 {
		return nil, errors.New("this runtime has no local API port configured")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), runtimeCallTO)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.runtimeURL(route), bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("cookie", a.runtimeCookie())
	req.Header.Set("content-type", "application/json")
	res, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	out, _ := io.ReadAll(res.Body)
	if !json.Valid(out) {
		out = []byte("{}")
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		var e struct {
			Error *string `json:"error"`
		}
		if json.Unmarshal(out, &e) == nil && e.Error != nil {
			return nil, errors.New(*e.Error)
		}
		return nil, fmt.Errorf("runtime answered %d", res.StatusCode)
	}
	return out, nil
}

// machineSnapshot asks the runtime to describe its machine (limits, forecasts, release): one implementation of that.
func (a *Agent) machineSnapshot() {
	ev := evMachine{Event: "machine", Snapshot: json.RawMessage("null")}
	fail := func(msg string) { ev.RuntimeError = &msg }
	ctx, cancel := context.WithTimeout(context.Background(), machineCallTO)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, a.runtimeURL(a.cfg.RuntimeAPI+"/machines"), nil)
	req.Header.Set("cookie", a.runtimeCookie())
	res, err := a.client.Do(req)
	switch {
	case err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded):
		fail("runtime did not answer in 8 s")
	case err != nil:
		fail(err.Error())
	default:
		raw, rerr := io.ReadAll(res.Body)
		res.Body.Close()
		switch {
		case res.StatusCode < 200 || res.StatusCode > 299:
			fail(fmt.Sprintf("runtime answered %d", res.StatusCode))
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			fail("runtime did not answer in 8 s")
		case rerr != nil || !json.Valid(raw):
			fail("runtime answered with no JSON")
		default:
			ev.Snapshot = bytes.TrimSpace(raw)
		}
	}
	a.broadcast(ev)
}

// notifyRuntimeOfState keeps the runtime's pages and tab bookkeeping in step with a layout the cluster changed.
func (a *Agent) notifyRuntimeOfState(name string) {
	if a.cfg.RuntimePort == 0 {
		return
	}
	token, err := os.ReadFile(filepath.Join(a.cfg.DataDir, "cli-token"))
	if err != nil || len(bytes.TrimSpace(token)) == 0 {
		return
	}
	raw, _ := json.Marshal(map[string]string{"name": name})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, a.runtimeURL(a.cfg.StateChangedPath), bytes.NewReader(raw))
	req.Header.Set("content-type", "application/json")
	for _, h := range a.cfg.CLITokenHeaders {
		req.Header.Set(h, string(bytes.TrimSpace(token)))
	}
	if res, err := a.client.Do(req); err == nil {
		_, _ = io.Copy(io.Discard, res.Body)
		res.Body.Close()
	}
}

func (a *Agent) settleState(requestID string, res StateResult) {
	a.mu.Lock()
	ch := a.pendingStates[requestID]
	a.mu.Unlock()
	if ch != nil {
		select {
		case ch <- res:
		default:
		}
	}
}

// Run starts the loops, the loopback server and, when configured, the call to the cluster. It returns when ctx ends.
func (a *Agent) Run(ctx context.Context) error {
	a.transcripts.WatchRoots()
	go every(ctx, a.cfg.ScanInterval, true, func() {
		a.transcripts.WatchRoots()
		if err := a.transcripts.Scan(); err != nil {
			Log("warn", "transcript scan failed", F{"error", err.Error()})
		}
	})
	go every(ctx, stateEvery, true, a.state.Sweep)
	if a.cfg.RuntimePort != 0 {
		go every(ctx, machineEvery, false, a.machineSnapshot)
		a.startRelays(ctx)
	}
	if a.cfg.ClusterURL != "" && a.cfg.RuntimeID != "" {
		go a.dialCluster(ctx)
	} else if a.cfg.ClusterURL != "" {
		Log("warn", "AGENT_CLUSTER_URL without AGENT_RUNTIME_ID, not dialling")
	}
	return a.listen(ctx)
}

func every(ctx context.Context, d time.Duration, now bool, f func()) {
	if now {
		f()
	}
	t := time.NewTicker(d)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			f()
		}
	}
}
