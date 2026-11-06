// Command machine-adapter is a stand-in "cloud" that speaks the Hub's machine adapter protocol v1
// (docs/hub/provider.md) and, on the same port, the older protocol without a version (v0). It is a separate process:
// the Hub reaches it over HTTP only, as it would reach an adapter for a real cloud. Servers are records; an operation
// finishes a moment after it is asked for. With -state the records are kept in a file, so the adapter can be restarted.
//
//	machine-adapter -listen 127.0.0.1:8099 -token-file /run/secrets/adapter-token -state /var/lib/adapter/cloud.json
//
// Besides the protocol it serves GET /stub/machines/{id}/bootstrap, which returns what create was given for the
// machine's agent (hub address and registration code). A real adapter would start the agent with it; here a test or a
// person plays the agent.
package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const protocol = "harnessly.machine-provider/v1"

type server struct {
	version string
	token   string
	finish  time.Duration
	file    string

	mu       sync.Mutex
	machines map[string]*machine
	ops      map[string]*op
	idem     map[string]string // Idempotency-Key -> op id
}

// stored is the state file.
type stored struct {
	Machines map[string]*machine `json:"machines"`
	Ops      map[string]*op      `json:"ops"`
	Idem     map[string]string   `json:"idem"`
}

func (s *server) load() error {
	b, err := os.ReadFile(s.file)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var st stored
	if err := json.Unmarshal(b, &st); err != nil {
		return err
	}
	if st.Machines != nil {
		s.machines = st.Machines
	}
	if st.Ops != nil {
		s.ops = st.Ops
	}
	if st.Idem != nil {
		s.idem = st.Idem
	}
	return nil
}

// save writes the state; callers hold s.mu.
func (s *server) save() {
	if s.file == "" {
		return
	}
	b, _ := json.Marshal(stored{s.machines, s.ops, s.idem})
	tmp := s.file + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err == nil {
		_ = os.Rename(tmp, s.file)
	}
}

type machine struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Status     string          `json:"status"`
	CPU        int             `json:"cpu"`
	RAMGB      int             `json:"ramGB"`
	DiskGB     int             `json:"diskGB"`
	HubMachine string          `json:"hubMachine,omitempty"`
	Bootstrap  json.RawMessage `json:"bootstrap,omitempty"`
}

// view is a machine as the protocol shows it: without the bootstrap secret.
func (m *machine) view() machine {
	c := *m
	c.Bootstrap = nil
	return c
}

// op is an operation; Kind says what happens to the machine when it finishes.
type op struct {
	State   string    `json:"state"`
	Machine string    `json:"machine"`
	ReadyAt time.Time `json:"readyAt"`
	// Kind is "run" (status becomes Status), "resize" (the sizes become CPU, RAMGB, DiskGB) or "" (nothing).
	Kind   string `json:"kind"`
	Status string `json:"status,omitempty"`
	CPU    int    `json:"cpu,omitempty"`
	RAMGB  int    `json:"ramGB,omitempty"`
	DiskGB int    `json:"diskGB,omitempty"`
}

func main() {
	listen := flag.String("listen", "127.0.0.1:8099", "address to listen on")
	tokenFile := flag.String("token-file", "", "file with the bearer token; empty means no token")
	version := flag.String("version", "1.0.0", "version the adapter reports in describe")
	stateFile := flag.String("state", "", "file the servers are kept in; empty means memory only")
	finish := flag.Duration("op-delay", 200*time.Millisecond, "how long an operation runs")
	flag.Parse()
	s := &server{version: *version, finish: *finish, machines: map[string]*machine{}, ops: map[string]*op{}, idem: map[string]string{}, file: *stateFile}
	if s.file != "" {
		if err := s.load(); err != nil {
			log.Fatal(err)
		}
	}
	if *tokenFile != "" {
		b, err := os.ReadFile(*tokenFile)
		if err != nil {
			log.Fatal(err)
		}
		s.token = strings.TrimSpace(string(b))
	}
	log.Printf("machine-adapter %s on %s", s.version, *listen)
	log.Fatal(http.ListenAndServe(*listen, s.routes()))
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	// Protocol v1.
	mux.HandleFunc("GET /v1/describe", s.describe)
	mux.HandleFunc("POST /v1/machines", s.create)
	mux.HandleFunc("POST /v1/machines/{id}/attach", s.attach)
	mux.HandleFunc("GET /v1/machines/{id}", s.state)
	mux.HandleFunc("POST /v1/machines/{id}/power", s.power)
	mux.HandleFunc("POST /v1/machines/{id}/resize", s.resize)
	mux.HandleFunc("GET /v1/machines/{id}/plans", s.v0Plans)
	mux.HandleFunc("GET /v1/ops/{op}", s.opStatus)
	// The protocol without a version (v0): what a Hub released before v1 speaks. It is kept so that one adapter serves
	// the old and the new Hub while the Hub is being updated.
	mux.HandleFunc("GET /machines/{id}/plans", s.v0Plans)
	mux.HandleFunc("POST /machines/{id}/resize", s.v0Resize)
	mux.HandleFunc("POST /machines/{id}/reboot", s.v0Power("running"))
	mux.HandleFunc("POST /machines/{id}/power-off", s.v0Power("stopped"))
	mux.HandleFunc("POST /machines/{id}/power-on", s.v0Power("running"))
	mux.HandleFunc("GET /ops/{op}", s.v0Op)
	mux.HandleFunc("GET /stub/machines/{id}/bootstrap", s.bootstrap)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.token != "" && subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+s.token)) != 1 {
			fail(w, http.StatusUnauthorized, "unauthorized", "bad token")
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func send(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, c, msg string) {
	send(w, code, map[string]string{"error": c, "message": msg})
}

func randID(prefix string) string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return prefix + hex.EncodeToString(b[:])
}

func (s *server) describe(w http.ResponseWriter, _ *http.Request) {
	send(w, http.StatusOK, map[string]any{
		"protocol": protocol, "name": "example-cloud", "version": s.version,
		"capabilities": []string{"create", "attach", "state", "power", "resize", "plans"},
		"config_schema": map[string]any{
			"type": "object", "properties": map[string]any{
				"cpu":    map[string]any{"type": "integer", "minimum": 1, "default": 2},
				"ramGB":  map[string]any{"type": "integer", "minimum": 1, "default": 4},
				"diskGB": map[string]any{"type": "integer", "minimum": 10, "default": 40},
			},
		},
	})
}

// startOp records an operation under the request's Idempotency-Key: a repeat of the key answers the same operation.
// Callers hold s.mu.
func (s *server) startOp(r *http.Request, o *op) (string, bool) {
	key := r.Header.Get("Idempotency-Key")
	if key != "" {
		if id, ok := s.idem[key]; ok {
			return id, true
		}
	}
	id := randID("op-")
	o.State, o.ReadyAt = "pending", time.Now().Add(s.finish)
	s.ops[id] = o
	if key != "" {
		s.idem[key] = id
	}
	s.save()
	return id, false
}

func (s *server) create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name      string          `json:"name"`
		Config    map[string]any  `json:"config"`
		Bootstrap json.RawMessage `json:"bootstrap"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || req.Name == "" {
		fail(w, http.StatusBadRequest, "bad_request", "name is required")
		return
	}
	num := func(k string, def int) int {
		if v, ok := req.Config[k].(float64); ok && v > 0 {
			return int(v)
		}
		return def
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if key := r.Header.Get("Idempotency-Key"); key != "" {
		if id, ok := s.idem[key]; ok {
			send(w, http.StatusAccepted, map[string]string{"op": id, "machine": s.ops[id].Machine})
			return
		}
	}
	m := &machine{ID: randID("srv-"), Name: req.Name, Status: "creating", CPU: num("cpu", 2), RAMGB: num("ramGB", 4), DiskGB: num("diskGB", 40), Bootstrap: req.Bootstrap}
	s.machines[m.ID] = m
	id, _ := s.startOp(r, &op{Machine: m.ID, Kind: "run", Status: "running"})
	send(w, http.StatusAccepted, map[string]string{"op": id, "machine": m.ID})
}

// lookup finds a server by its own id or by the id of the hub machine it was attached to.
func (s *server) lookup(ref string) *machine {
	if m := s.machines[ref]; m != nil {
		return m
	}
	for _, m := range s.machines {
		if m.HubMachine != "" && m.HubMachine == ref {
			return m
		}
	}
	return nil
}

func (s *server) find(w http.ResponseWriter, r *http.Request) *machine {
	m := s.lookup(r.PathValue("id"))
	if m == nil {
		fail(w, http.StatusNotFound, "not_found", "no such server")
	}
	return m
}

func (s *server) attach(w http.ResponseWriter, r *http.Request) {
	var req struct {
		HubMachine string `json:"hubMachine"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || req.HubMachine == "" {
		fail(w, http.StatusBadRequest, "bad_request", "hubMachine is required")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if m := s.find(w, r); m != nil {
		m.HubMachine = req.HubMachine
		s.save()
		send(w, http.StatusOK, m.view())
	}
}

// settle finishes the operations whose time has come; callers hold s.mu.
func (s *server) settle() {
	now := time.Now()
	changed := false
	for _, o := range s.ops {
		if o.State == "done" {
			continue
		}
		if now.Before(o.ReadyAt) {
			if o.State == "pending" {
				o.State, changed = "running", true
			}
			continue
		}
		if m := s.machines[o.Machine]; m != nil {
			switch o.Kind {
			case "run":
				m.Status = o.Status
			case "resize":
				m.CPU, m.RAMGB, m.DiskGB = o.CPU, o.RAMGB, o.DiskGB
			}
		}
		o.State, changed = "done", true
	}
	if changed {
		s.save()
	}
}

func (s *server) state(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.settle()
	if m := s.find(w, r); m != nil {
		send(w, http.StatusOK, m.view())
	}
}

func (s *server) power(w http.ResponseWriter, r *http.Request) {
	var req struct{ Action string }
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		fail(w, http.StatusBadRequest, "bad_request", "bad body")
		return
	}
	status := map[string]string{"on": "running", "off": "stopped", "reboot": "running"}[req.Action]
	if status == "" {
		fail(w, http.StatusBadRequest, "bad_request", "action is on, off or reboot")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if m := s.find(w, r); m != nil {
		id, _ := s.startOp(r, &op{Machine: m.ID, Kind: "run", Status: status})
		send(w, http.StatusAccepted, map[string]string{"op": id})
	}
}

// startResize checks and starts a resize; it answers the error itself and returns "" then.
func (s *server) startResize(w http.ResponseWriter, r *http.Request, m *machine, cpu, ram, disk int) string {
	if cpu < 1 || ram < 1 || disk < 1 {
		fail(w, http.StatusBadRequest, "bad_request", "cpu, ramGB and diskGB are required")
		return ""
	}
	if disk < m.DiskGB {
		fail(w, http.StatusConflict, "disk_shrink", "a disk can only grow")
		return ""
	}
	id, _ := s.startOp(r, &op{Machine: m.ID, Kind: "resize", CPU: cpu, RAMGB: ram, DiskGB: disk})
	return id
}

func (s *server) resize(w http.ResponseWriter, r *http.Request) {
	var req struct{ CPU, RAMGB, DiskGB int }
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		fail(w, http.StatusBadRequest, "bad_request", "bad body")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if m := s.find(w, r); m != nil {
		if id := s.startResize(w, r, m, req.CPU, req.RAMGB, req.DiskGB); id != "" {
			send(w, http.StatusAccepted, map[string]string{"op": id})
		}
	}
}

func (s *server) opStatus(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.settle()
	o := s.ops[r.PathValue("op")]
	if o == nil {
		fail(w, http.StatusNotFound, "not_found", "no such operation")
		return
	}
	send(w, http.StatusOK, map[string]string{"state": o.State, "machine": o.Machine})
}

// Protocol v0 -----------------------------------------------------------------------------------------------------

func (s *server) v0Plans(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.settle()
	if m := s.find(w, r); m != nil {
		send(w, http.StatusOK, map[string]any{"plans": []map[string]any{
			{"id": fmt.Sprintf("%dc-%dg", m.CPU, m.RAMGB), "name": "current", "cpu": m.CPU, "ramGB": m.RAMGB, "diskGB": m.DiskGB, "current": true},
		}})
	}
}

func (s *server) v0Resize(w http.ResponseWriter, r *http.Request) {
	var req struct{ CPU, RAMGB, DiskGB int }
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		fail(w, http.StatusBadRequest, "bad_request", "bad body")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if m := s.find(w, r); m != nil {
		if id := s.startResize(w, r, m, req.CPU, req.RAMGB, req.DiskGB); id != "" {
			send(w, http.StatusAccepted, map[string]string{"opId": id})
		}
	}
}

func (s *server) v0Power(status string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if m := s.find(w, r); m != nil {
			id, _ := s.startOp(r, &op{Machine: m.ID, Kind: "run", Status: status})
			send(w, http.StatusAccepted, map[string]string{"opId": id})
		}
	}
}

func (s *server) v0Op(w http.ResponseWriter, r *http.Request) {
	s.opStatus(w, r)
}

func (s *server) bootstrap(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m := s.find(w, r); m != nil {
		w.Header().Set("Content-Type", "application/json")
		if len(m.Bootstrap) == 0 {
			_, _ = w.Write([]byte("{}"))
			return
		}
		_, _ = w.Write(m.Bootstrap)
	}
}
