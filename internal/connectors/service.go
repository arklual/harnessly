package connectors

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Secrets is the machine's safe, by name (the same as internal/connections uses).
type Secrets interface {
	Put(name, value string) error
	Reveal(name string) (string, error)
	Remove(name string) error
}

// Conn is a kept connection as the page and the agent see it: never a secret value.
type Conn struct {
	ID        string            `json:"id"`
	Connector string            `json:"connector"`
	Kind      Kind              `json:"kind"`
	Name      string            `json:"name"`
	Config    map[string]string `json:"config"`
	// Secrets are the ids of the secret fields that have a value in the safe.
	Secrets []string `json:"secrets,omitempty"`
	Account string   `json:"account,omitempty"`
	// State of the last check or run: "ok" or "error", with Error in words.
	State     string    `json:"state"`
	Error     string    `json:"error,omitempty"`
	CheckedAt time.Time `json:"checkedAt,omitzero"`
	CreatedAt time.Time `json:"createdAt"`
	// LastEventAt is when its source last gave an event.
	LastEventAt time.Time `json:"lastEventAt,omitzero"`
	// Next is the step the person still has to do (a Stepper's), filled in by Views; never kept.
	Next *Step `json:"next,omitempty"`
	// Shown is what the person sees once: only in the answer of Connect and Reissue, never kept.
	Shown []Shown `json:"shown,omitempty"`
}

// State values.
const (
	StateOK    = "ok"
	StateError = "error"
)

// SecretName is the safe's name of a connection's secret fields (a JSON object).
func SecretName(id string) string { return "connector-" + id }

// Service keeps the connections of one runtime, runs their sources and their actions.
type Service struct {
	dir     string
	secrets Secrets
	http    *http.Client
	now     func() time.Time
	// OnChange is told when something the page shows moved: a connection, a proposed action. May be nil.
	OnChange func()

	watchMu sync.Mutex
	rev     uint64
	moved   chan struct{}

	mu      sync.Mutex
	pending map[string]*pending
	runner  *runner
}

// ErrUnknown: no such connector or connection.
var ErrUnknown = errors.New("unknown")

// New keeps connections under dir (dir/connections.json, dir/state, dir/sent.jsonl) and their secrets in the safe.
func New(dir string, secrets Secrets, client *http.Client) *Service {
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	s := &Service{dir: dir, secrets: secrets, http: client, now: time.Now, pending: map[string]*pending{}, rev: uint64(time.Now().UnixMilli())}
	s.runner = newRunner(s)
	return s
}

func (s *Service) changed() {
	s.watchMu.Lock()
	s.rev++
	if s.moved != nil {
		close(s.moved)
	}
	s.moved = make(chan struct{})
	s.watchMu.Unlock()
	if s.OnChange != nil {
		s.OnChange()
	}
}

// Rev moves with every change the page shows (a connection, a proposal): the page waits on it instead of asking every
// few seconds.
func (s *Service) Rev() uint64 {
	s.watchMu.Lock()
	defer s.watchMu.Unlock()
	return s.rev
}

// WaitRev answers the revision as soon as it is not after, or after d.
func (s *Service) WaitRev(ctx context.Context, after uint64, d time.Duration) uint64 {
	s.watchMu.Lock()
	if s.moved == nil {
		s.moved = make(chan struct{})
	}
	rev, ch := s.rev, s.moved
	s.watchMu.Unlock()
	if rev != after {
		return rev
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ch:
	case <-t.C:
	case <-ctx.Done():
	}
	return s.Rev()
}

func (s *Service) file() string { return filepath.Join(s.dir, "connections.json") }

// dataDir is a connection's own folder (Instance.Dir).
func (s *Service) dataDir(id string) string { return filepath.Join(s.dir, "data", id) }

type connsFile struct {
	Version     int    `json:"version"`
	Connections []Conn `json:"connections"`
}

func (s *Service) read() ([]Conn, error) {
	raw, err := os.ReadFile(s.file())
	if errors.Is(err, os.ErrNotExist) {
		return []Conn{}, nil
	}
	if err != nil {
		return nil, err
	}
	var f connsFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, err
	}
	if f.Connections == nil {
		f.Connections = []Conn{}
	}
	return f.Connections, nil
}

func (s *Service) write(list []Conn) error {
	raw, err := json.MarshalIndent(connsFile{Version: 1, Connections: list}, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(s.file(), raw)
}

// writeFile writes atomically: a crash leaves the old file or the new one, never half of one.
func writeFile(path string, raw []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// List is every connection.
func (s *Service) List() ([]Conn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.read()
}

// Get is one connection by id.
func (s *Service) Get(id string) (Conn, error) {
	list, err := s.List()
	if err != nil {
		return Conn{}, err
	}
	for _, c := range list {
		if c.ID == id {
			return c, nil
		}
	}
	return Conn{}, ErrUnknown
}

func (s *Service) update(id string, change func(*Conn)) (Conn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.read()
	if err != nil {
		return Conn{}, err
	}
	for i := range list {
		if list[i].ID == id {
			change(&list[i])
			return list[i], s.write(list)
		}
	}
	return Conn{}, ErrUnknown
}

func newID(prefix string) string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

// instance is a connection as its connector sees it. secret reads the safe unless values are given (a check before
// the connection is kept).
func (s *Service) instance(c Conn, values map[string]string) Instance {
	var once sync.Once
	var kept map[string]string
	var keptErr error
	in := Instance{
		ID: c.ID, Connector: c.Connector, Name: c.Name, Config: c.Config, HTTP: s.http, Dir: s.dataDir(c.ID),
		Load: func(v any) error {
			raw, err := os.ReadFile(s.memoFile(c.ID))
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			if err != nil {
				return err
			}
			return json.Unmarshal(raw, v)
		},
		Store: func(v any) error {
			raw, err := json.Marshal(v)
			if err != nil {
				return err
			}
			if err := writeFile(s.memoFile(c.ID), raw); err != nil {
				return err
			}
			s.changed()
			return nil
		},
		Secret: func(field string) (string, error) {
			if values != nil {
				return values[field], nil
			}
			once.Do(func() {
				raw, err := s.secrets.Reveal(SecretName(c.ID))
				if err != nil {
					keptErr = err
					return
				}
				keptErr = json.Unmarshal([]byte(raw), &kept)
			})
			if keptErr != nil {
				return "", keptErr
			}
			return kept[field], nil
		},
	}
	if conn, ok := Lookup(c.Connector); ok {
		if _, relay := conn.(Relay); relay {
			by := "relay:" + c.ID
			in.Answer = func(ctx context.Context, id string, approve bool) (Proposal, error) {
				return s.answer(ctx, id, approve, by)
			}
		}
	}
	return in
}

// memoFile is where a connection's own record is kept (Instance.Load, Instance.Store).
func (s *Service) memoFile(id string) string { return filepath.Join(s.dir, "state", id+".memo.json") }

// Views are the connections for the page: List, with the step each still waits for from the person.
func (s *Service) Views(ctx context.Context) ([]Conn, error) {
	list, err := s.List()
	if err != nil {
		return nil, err
	}
	for i := range list {
		s.withNext(ctx, &list[i])
	}
	return list, nil
}

// withNext fills in the step a connection still waits for from the person.
func (s *Service) withNext(ctx context.Context, c *Conn) {
	conn, ok := Lookup(c.Connector)
	if !ok {
		return
	}
	if st, ok := conn.(Stepper); ok {
		if step, has := st.Next(ctx, s.instance(*c, nil)); has {
			c.Next = &step
		}
	}
}

var numberRe = regexp.MustCompile(`^\d{1,9}$`)

// form checks the person's values against the spec: the non-secret ones and the secret ones, each by its field.
func form(sp Spec, values map[string]string) (config, secret map[string]string, err error) {
	config, secret = map[string]string{}, map[string]string{}
	for _, f := range sp.Fields {
		if f.Generate {
			secret[f.ID] = generated()
			continue
		}
		if !shown(sp, f, values) {
			continue
		}
		v := strings.TrimSpace(values[f.ID])
		if v == "" {
			v = f.Default
		}
		if v == "" {
			if !f.Optional {
				return nil, nil, Sayf("%s: required", f.Label.En)
			}
			continue
		}
		switch f.Type {
		case FieldURL:
			u, perr := url.Parse(v)
			if perr != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
				return nil, nil, Sayf("%s: an http(s) address", f.Label.En)
			}
		case FieldEmail:
			if _, perr := mail.ParseAddress(v); perr != nil {
				return nil, nil, Sayf("%s: an email address", f.Label.En)
			}
		case FieldNumber:
			if !numberRe.MatchString(v) {
				return nil, nil, Sayf("%s: a number", f.Label.En)
			}
		case FieldChoice:
			ok := false
			for _, c := range f.Choices {
				ok = ok || c.Value == v
			}
			if !ok && !f.Discover {
				return nil, nil, Sayf("%s: one of the choices", f.Label.En)
			}
		case FieldChoices:
			var picked []string
			for _, one := range strings.Split(v, ",") {
				one = strings.TrimSpace(one)
				if one == "" {
					continue
				}
				ok := f.Discover
				for _, c := range f.Choices {
					ok = ok || c.Value == one
				}
				if !ok {
					return nil, nil, Sayf("%s: one of the choices", f.Label.En)
				}
				picked = append(picked, one)
			}
			v = strings.Join(picked, ",")
			if v == "" {
				if !f.Optional {
					return nil, nil, Sayf("%s: required", f.Label.En)
				}
				continue
			}
		}
		if f.Secret {
			secret[f.ID] = v
		} else {
			config[f.ID] = v
		}
	}
	return config, secret, nil
}

// shown says whether a field is asked with these values: a field with When only while its choice has one of the values.
func shown(sp Spec, f Field, values map[string]string) bool {
	if f.When == nil {
		return true
	}
	v := strings.TrimSpace(values[f.When.Field])
	if v == "" {
		for _, g := range sp.Fields {
			if g.ID == f.When.Field {
				v = g.Default
			}
		}
	}
	for _, want := range f.When.Values {
		if v == want {
			return true
		}
	}
	return false
}

// Discover lists the choices of a field marked Discover from the values of the form filled in so far, without keeping
// anything: the calendars of an account before the person picks the ones to connect.
func (s *Service) Discover(ctx context.Context, connector, field string, values map[string]string) ([]Choice, error) {
	c, ok := Lookup(connector)
	if !ok {
		return nil, ErrUnknown
	}
	d, ok := c.(Discoverer)
	if !ok {
		return nil, ErrUnknownTool
	}
	sp := c.Spec()
	known := false
	for _, f := range sp.Fields {
		known = known || (f.ID == field && f.Discover)
	}
	if !known {
		return nil, ErrUnknownTool
	}
	// Only the fields before the discovered one are needed: the form asks before the person picked.
	var before []Field
	for _, f := range sp.Fields {
		if f.ID == field {
			break
		}
		before = append(before, f)
	}
	config, secret, err := form(Spec{ID: sp.ID, Fields: before}, values)
	if err != nil {
		return nil, err
	}
	conn := Conn{ID: "discover", Connector: sp.ID, Kind: sp.Kind, Config: config}
	choices, err := d.Discover(ctx, s.instance(conn, secret), field)
	if err != nil {
		return nil, err
	}
	if choices == nil {
		choices = []Choice{}
	}
	return choices, nil
}

// generated is a value of a Generate field: 32 random bytes, hex.
func generated() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// revealed is what the person sees once of a connection: the connector's Reveal, or its generated fields.
func revealed(c Connector, in Instance, secret map[string]string) []Shown {
	if r, ok := c.(Revealer); ok {
		out, err := r.Reveal(in)
		if err == nil {
			return out
		}
	}
	var out []Shown
	for _, f := range c.Spec().Fields {
		if v, ok := secret[f.ID]; ok && f.Generate {
			out = append(out, Shown{Label: f.Label, Value: v})
		}
	}
	return out
}

// Connect checks the values against the service and keeps the connection: its secret fields in the safe, the others
// in connections.json. Nothing is kept when the check fails.
func (s *Service) Connect(ctx context.Context, connector, name string, values map[string]string) (Conn, error) {
	c, ok := Lookup(connector)
	if !ok {
		return Conn{}, ErrUnknown
	}
	sp := c.Spec()
	config, secret, err := form(sp, values)
	if err != nil {
		return Conn{}, err
	}
	conn := Conn{ID: newID("cx"), Connector: sp.ID, Kind: sp.Kind, Name: strings.TrimSpace(name), Config: config, CreatedAt: s.now().UTC()}
	acc, err := c.Check(ctx, s.instance(conn, secret))
	if err != nil {
		return Conn{}, err
	}
	conn.Account, conn.State, conn.CheckedAt = acc.Label, StateOK, s.now().UTC()
	if conn.Name == "" {
		conn.Name = acc.Label
	}
	if conn.Name == "" {
		conn.Name = sp.Title.En
	}
	if len(secret) > 0 {
		raw, _ := json.Marshal(secret)
		if err := s.secrets.Put(SecretName(conn.ID), string(raw)); err != nil {
			return Conn{}, err
		}
		for _, f := range sp.Fields {
			if _, has := secret[f.ID]; has {
				conn.Secrets = append(conn.Secrets, f.ID)
			}
		}
	}
	s.mu.Lock()
	list, err := s.read()
	if err == nil {
		err = s.write(append(list, conn))
	}
	s.mu.Unlock()
	if err != nil {
		if len(secret) > 0 {
			_ = s.secrets.Remove(SecretName(conn.ID))
		}
		return Conn{}, err
	}
	s.runner.start(conn)
	s.changed()
	s.withNext(ctx, &conn)
	conn.Shown = revealed(c, s.instance(conn, secret), secret)
	return conn, nil
}

// Reissue makes new values of a connection's generated secrets (a webhook's key) and keeps the others: the old values
// stop working at once. The answer shows the new ones, once.
func (s *Service) Reissue(id string) (Conn, error) {
	conn, c, err := s.open(id)
	if err != nil {
		return Conn{}, err
	}
	secret := map[string]string{}
	if raw, rerr := s.secrets.Reveal(SecretName(id)); rerr == nil {
		_ = json.Unmarshal([]byte(raw), &secret)
	}
	has := false
	for _, f := range c.Spec().Fields {
		if f.Generate {
			secret[f.ID], has = generated(), true
		}
	}
	if !has {
		return Conn{}, Sayf("Nothing to reissue")
	}
	raw, _ := json.Marshal(secret)
	if err := s.secrets.Put(SecretName(id), string(raw)); err != nil {
		return Conn{}, err
	}
	s.runner.start(conn)
	s.changed()
	conn.Shown = revealed(c, s.instance(conn, secret), secret)
	return conn, nil
}

// Receive hands a request pushed to a connection's address to its connector (a Receiver) and keeps the events it
// makes, as a source's batch: the cursor stays, repeats are dropped, the envelopes wait in the outbox for delivery.
func (s *Service) Receive(ctx context.Context, id string, req Incoming) (int, error) {
	conn, c, err := s.open(id)
	if err != nil {
		return 0, err
	}
	rc, ok := c.(Receiver)
	if !ok {
		return 0, ErrUnknown
	}
	in := s.instance(conn, nil)
	events, err := rc.Receive(ctx, in, req)
	if err != nil {
		return 0, err
	}
	s.runner.stateMu.Lock()
	cur := s.runner.load(id).Cursor
	s.runner.stateMu.Unlock()
	if err := s.runner.commit(c.Spec(), in, conn.Account, events, cur); err != nil {
		return 0, err
	}
	if len(events) > 0 {
		s.changed()
	}
	return len(events), nil
}

// Check tries a kept connection again and keeps the outcome; an error of the service is the connection's state, not
// an error of the call.
func (s *Service) Check(ctx context.Context, id string) (Conn, error) {
	conn, c, err := s.open(id)
	if err != nil {
		return Conn{}, err
	}
	acc, cerr := c.Check(ctx, s.instance(conn, nil))
	out, err := s.update(id, func(x *Conn) {
		x.CheckedAt = s.now().UTC()
		if cerr != nil {
			x.State, x.Error = StateError, sayOf(cerr)
			return
		}
		x.State, x.Error, x.Account = StateOK, "", acc.Label
	})
	if err == nil {
		s.runner.start(out)
		s.changed()
		s.withNext(ctx, &out)
	}
	return out, err
}

// Rename names a connection.
func (s *Service) Rename(id, name string) (Conn, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Conn{}, Sayf("a name is required")
	}
	out, err := s.update(id, func(x *Conn) { x.Name = name })
	if err == nil {
		s.changed()
	}
	return out, err
}

// Remove stops a connection's source and forgets it, its secrets and its cursor. The journal of sends stays.
func (s *Service) Remove(id string) error {
	s.runner.stop(id)
	s.mu.Lock()
	list, err := s.read()
	if err != nil {
		s.mu.Unlock()
		return err
	}
	out, found := list[:0], false
	for _, c := range list {
		if c.ID == id {
			found = true
			continue
		}
		out = append(out, c)
	}
	if !found {
		s.mu.Unlock()
		return ErrUnknown
	}
	err = s.write(out)
	s.mu.Unlock()
	if err != nil {
		return err
	}
	_ = s.secrets.Remove(SecretName(id))
	_ = os.Remove(s.runner.stateFile(id))
	_ = os.Remove(s.memoFile(id))
	_ = os.RemoveAll(s.dataDir(id))
	s.changed()
	return nil
}

func (s *Service) open(id string) (Conn, Connector, error) {
	conn, err := s.Get(id)
	if err != nil {
		return Conn{}, nil, err
	}
	c, ok := Lookup(conn.Connector)
	if !ok {
		return Conn{}, nil, ErrUnknown
	}
	return conn, c, nil
}

// Read runs a read tool. What came from outside is marked as data, not instructions, before it reaches the agent.
func (s *Service) Read(ctx context.Context, id, tool string, args map[string]string) (string, error) {
	conn, c, err := s.open(id)
	if err != nil {
		return "", err
	}
	r, ok := c.(Reader)
	if !ok || !hasTool(c.Spec(), tool) {
		return "", ErrUnknownTool
	}
	res, err := r.Read(ctx, s.instance(conn, nil), tool, args)
	if err != nil {
		return "", err
	}
	return res.agentText(conn), nil
}

func (r Result) agentText(conn Conn) string {
	out := strings.TrimRight(r.Text, "\n")
	if r.Data != "" {
		from := r.From
		if from == "" {
			from = conn.Connector + ": " + conn.Name
		}
		if out != "" {
			out += "\n"
		}
		out += Untrusted(from, r.Data)
	}
	return out
}

func hasTool(sp Spec, name string) bool {
	for _, t := range sp.Tools {
		if t.Name == name {
			return true
		}
	}
	return false
}

func actionOf(sp Spec, name string) (Action, bool) {
	for _, a := range sp.Actions {
		if a.Name == name {
			return a, true
		}
	}
	return Action{}, false
}

// sayOf is an error in words for the person: a connector's Said as it is, anything else as a short line.
func sayOf(err error) string {
	var said *Said
	if errors.As(err, &said) {
		return said.Message
	}
	msg := err.Error()
	if len(msg) > 300 {
		msg = msg[:300]
	}
	return msg
}

// Say is an error of the Service in words for the person and the agent.
func Say(err error) string { return sayOf(err) }

// Start runs the sources of every kept connection until ctx ends, and delivers their events with deliver.
func (s *Service) Start(ctx context.Context, deliver ...Destination) {
	s.runner.run(ctx, deliver)
}

// WaitStopped waits, after the context given to Start is done, until every source it started has returned: a test that
// starts a second runtime on the same folder, or changes a connector's package state, waits for the first one's sources.
func (s *Service) WaitStopped() {
	s.runner.sources.Wait()
}
