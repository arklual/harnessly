package connections

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// Secrets is where credentials are kept: the safe, by name.
type Secrets interface {
	Put(name, value string) error
	Reveal(name string) (string, error)
	Remove(name string) error
}

// EnvConn is a connection the deployment configures in its environment (the tracker of RUNTIME_TRACKER_URL): listed and
// used like the others, never written or removed from the page.
type EnvConn struct {
	Conn Conn
	// Cred reads its credentials on every use, so a rotated token is taken at once.
	Cred func() Credentials
}

// Service keeps the connections of one runtime.
type Service struct {
	file    string
	secrets Secrets
	http    *HTTP
	env     []EnvConn
	now     func() time.Time

	mu sync.Mutex
}

// ErrReadOnly: the connection is the deployment's.
var ErrReadOnly = errors.New("read-only")

// New keeps connections in dir/connections.json and their credentials in secrets.
func New(dir string, secrets Secrets, h *HTTP, env ...EnvConn) *Service {
	if h == nil {
		h = NewHTTP(nil)
	}
	return &Service{file: filepath.Join(dir, "connections.json"), secrets: secrets, http: h, env: env, now: time.Now}
}

// HTTP is the client connections call through.
func (s *Service) HTTP() *HTTP { return s.http }

type stored struct {
	Version     int    `json:"version"`
	Connections []Conn `json:"connections"`
	// EnvPrimary are the deployment's connections the person made primary (they are not kept, their mark is).
	EnvPrimary []string `json:"envPrimary,omitempty"`
}

func (s *Service) readAll() (stored, error) {
	raw, err := os.ReadFile(s.file)
	if errors.Is(err, os.ErrNotExist) {
		return stored{}, nil
	}
	if err != nil {
		return stored{}, err
	}
	var f stored
	if err := json.Unmarshal(raw, &f); err != nil {
		return stored{}, fmt.Errorf("connections.json: %w", err)
	}
	return f, nil
}

func (s *Service) read() ([]Conn, error) {
	f, err := s.readAll()
	return f.Connections, err
}

// write keeps list as the person's connections; the other marks of the file stay.
func (s *Service) write(list []Conn) error {
	f, err := s.readAll()
	if err != nil {
		return err
	}
	f.Connections = list
	return s.writeAll(f)
}

func (s *Service) writeAll(f stored) error {
	if err := os.MkdirAll(filepath.Dir(s.file), 0o700); err != nil {
		return err
	}
	f.Version = 1
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.file + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.file)
}

// List is every connection: the person's, then the deployment's. A capability with no primary connection of the
// person's gets the first one that has it.
func (s *Service) List() ([]Conn, error) {
	s.mu.Lock()
	f, err := s.readAll()
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	list := f.Connections
	for _, e := range s.env {
		c := e.Conn
		c.Env = true
		c.Primary = slices.Contains(f.EnvPrimary, c.ID)
		if c.State == "" {
			c.State = "ok"
		}
		// An instance the person connected themselves wins over the deployment's copy of it.
		if slices.ContainsFunc(list, func(x Conn) bool { return x.Kind == c.Kind && x.BaseURL == c.BaseURL }) {
			continue
		}
		list = append(list, c)
	}
	return list, nil
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
	return Conn{}, fmt.Errorf("%w: connection %s", ErrNotFound, id)
}

// Primary is the connection used for a capability when none is named: the one marked primary, else the first of the
// person's, else the deployment's. ok is false when no connection has it.
func (s *Service) Primary(cap Capability) (Conn, bool) {
	list, err := s.List()
	if err != nil {
		return Conn{}, false
	}
	var first *Conn
	for i := range list {
		c := list[i]
		if !c.Has(cap) {
			continue
		}
		if c.Primary {
			return c, true
		}
		if first == nil {
			first = &list[i]
		}
	}
	if first == nil {
		return Conn{}, false
	}
	return *first, true
}

// Detect finds what a link is.
func (s *Service) Detect(ctx context.Context, raw string) (*Detection, error) {
	return Detect(ctx, s.http, raw)
}

// ConnectInput is what the page sends to connect: a detection (as Detect answered it, possibly edited) and the
// typed credentials.
type ConnectInput struct {
	Detection Detection   `json:"detection"`
	Method    string      `json:"method"`
	Cred      Credentials `json:"credentials"`
	Name      string      `json:"name,omitempty"`
}

func newID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return "c" + hex.EncodeToString(b)
}

func secretName(id string) string { return "conn-" + id }

// methodOf finds a method of a detection.
func methodOf(d Detection, id string) (Method, bool) {
	for _, m := range d.Methods {
		if m.ID == id {
			return m, true
		}
	}
	return Method{}, false
}

// Connect checks the credentials against the instance and keeps the connection. A connection to the same instance
// with the same login is replaced, so connecting again is how a token is rotated.
func (s *Service) Connect(ctx context.Context, in ConnectInput) (Conn, error) {
	d := in.Detection
	p := ProviderOf(d.Kind)
	if p == nil {
		return Conn{}, fmt.Errorf("%w: %s", ErrUnknown, d.Kind)
	}
	if _, err := url.Parse(d.BaseURL); err != nil || d.BaseURL == "" {
		return Conn{}, fmt.Errorf("%w: base address", ErrUnknown)
	}
	m, ok := methodOf(d, in.Method)
	if !ok {
		return Conn{}, fmt.Errorf("%w: sign-in method %q", ErrUnsupported, in.Method)
	}
	cred := Credentials{}
	for _, f := range m.Fields {
		v := strings.TrimSpace(in.Cred[f.ID])
		if v == "" && !f.Optional {
			return Conn{}, fmt.Errorf("%w: %s is empty", ErrDenied, f.ID)
		}
		if v != "" {
			cred[f.ID] = v
		}
	}
	now := s.now().UTC()
	c := Conn{
		ID: newID(), Kind: d.Kind, BaseURL: strings.TrimRight(d.BaseURL, "/"), APIURL: d.APIURL,
		Deployment: d.Deployment, Version: d.Version, Edition: d.Edition, Method: m.ID,
		Login: cred[FieldEmail], Capabilities: d.Capabilities, Extra: d.Extra, CreatedAt: now,
	}
	if c.Login == "" {
		c.Login = cred[FieldUsername]
	}
	c.Name = strings.TrimSpace(in.Name)
	if c.Name == "" {
		c.Name = d.Title
	}
	if c.Name == "" {
		if u, err := url.Parse(c.BaseURL); err == nil {
			c.Name = u.Host
		}
	}
	me, err := p.Open(c, cred, s.http).Me(ctx)
	if err != nil {
		return Conn{}, err
	}
	c.Me, c.State, c.CheckedAt = me, "ok", now
	raw, _ := json.Marshal(cred)

	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.read()
	if err != nil {
		return Conn{}, err
	}
	for i, old := range list {
		if old.Kind == c.Kind && old.BaseURL == c.BaseURL && old.Login == c.Login {
			c.ID, c.CreatedAt, c.Primary = old.ID, old.CreatedAt, old.Primary
			if in.Name == "" {
				c.Name = old.Name
			}
			list = slices.Delete(list, i, i+1)
			break
		}
	}
	if err := s.secrets.Put(secretName(c.ID), string(raw)); err != nil {
		return Conn{}, err
	}
	list = append(list, c)
	if err := s.write(list); err != nil {
		return Conn{}, err
	}
	return c, nil
}

// update changes a kept connection under the lock.
func (s *Service) update(id string, change func(*Conn) error) (Conn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.read()
	if err != nil {
		return Conn{}, err
	}
	for i := range list {
		if list[i].ID == id {
			if err := change(&list[i]); err != nil {
				return Conn{}, err
			}
			return list[i], s.write(list)
		}
	}
	for _, e := range s.env {
		if e.Conn.ID == id {
			return Conn{}, fmt.Errorf("%w: %s comes from the deployment", ErrReadOnly, id)
		}
	}
	return Conn{}, fmt.Errorf("%w: connection %s", ErrNotFound, id)
}

// Rename names a connection.
func (s *Service) Rename(id, name string) (Conn, error) {
	name = strings.TrimSpace(name)
	return s.update(id, func(c *Conn) error {
		if name != "" {
			c.Name = name
		}
		return nil
	})
}

// SetPrimary makes a connection the one used for its capabilities, and no other of the same capabilities. A
// deployment's connection may be made primary too: its mark is kept, the connection itself stays read-only.
func (s *Service) SetPrimary(id string) (Conn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.readAll()
	if err != nil {
		return Conn{}, err
	}
	var target *Conn
	for i := range f.Connections {
		if f.Connections[i].ID == id {
			target = &f.Connections[i]
		}
	}
	envTarget := false
	if target == nil {
		for _, e := range s.env {
			if e.Conn.ID == id {
				c := e.Conn
				c.Env = true
				target, envTarget = &c, true
			}
		}
	}
	if target == nil {
		return Conn{}, fmt.Errorf("%w: connection %s", ErrNotFound, id)
	}
	caps := slices.Clone(target.Capabilities)
	shares := func(c Conn) bool { return slices.ContainsFunc(caps, c.Has) }
	for i := range f.Connections {
		if f.Connections[i].ID == id {
			f.Connections[i].Primary = true
		} else if shares(f.Connections[i]) {
			f.Connections[i].Primary = false
		}
	}
	var keep []string
	for _, envID := range f.EnvPrimary {
		for _, e := range s.env {
			if e.Conn.ID == envID && envID != id && !shares(e.Conn) {
				keep = append(keep, envID)
			}
		}
	}
	if envTarget {
		keep = append(keep, id)
	}
	f.EnvPrimary = keep
	if err := s.writeAll(f); err != nil {
		return Conn{}, err
	}
	out := *target
	out.Primary = true
	if envTarget && out.State == "" {
		out.State = "ok"
	}
	return out, nil
}

// Remove forgets a connection and its credentials.
func (s *Service) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.read()
	if err != nil {
		return err
	}
	i := slices.IndexFunc(list, func(c Conn) bool { return c.ID == id })
	if i < 0 {
		for _, e := range s.env {
			if e.Conn.ID == id {
				return fmt.Errorf("%w: %s comes from the deployment", ErrReadOnly, id)
			}
		}
		return fmt.Errorf("%w: connection %s", ErrNotFound, id)
	}
	list = slices.Delete(list, i, i+1)
	if err := s.write(list); err != nil {
		return err
	}
	if err := s.secrets.Remove(secretName(id)); err != nil && !errors.Is(err, ErrNotFound) {
		// The connection is gone either way; a leftover secret is visible in the safe and can be removed there.
		return nil
	}
	return nil
}

// credentials of a connection.
func (s *Service) credentials(c Conn) (Credentials, error) {
	for _, e := range s.env {
		if e.Conn.ID == c.ID {
			if e.Cred == nil {
				return Credentials{}, nil
			}
			return e.Cred(), nil
		}
	}
	raw, err := s.secrets.Reveal(secretName(c.ID))
	if err != nil {
		return nil, fmt.Errorf("%w: credentials of %s: %v", ErrDenied, c.ID, err)
	}
	var cred Credentials
	if err := json.Unmarshal([]byte(raw), &cred); err != nil {
		return nil, fmt.Errorf("%w: credentials of %s are not readable", ErrDenied, c.ID)
	}
	return cred, nil
}

// Open is a client for a connection.
func (s *Service) Open(c Conn) (Client, error) {
	p := ProviderOf(c.Kind)
	if p == nil {
		return nil, fmt.Errorf("%w: %s", ErrUnknown, c.Kind)
	}
	cred, err := s.credentials(c)
	if err != nil {
		return nil, err
	}
	return p.Open(c, cred, s.http), nil
}

// Check signs in again and keeps the result.
func (s *Service) Check(ctx context.Context, id string) (Conn, error) {
	c, err := s.Get(id)
	if err != nil {
		return Conn{}, err
	}
	cl, err := s.Open(c)
	var me *Person
	if err == nil {
		me, err = cl.Me(ctx)
	}
	state := "ok"
	if err != nil {
		state = Reason(err)
		if state != "denied" {
			state = "unreachable"
		}
	}
	if c.Env {
		c.State, c.CheckedAt = state, s.now().UTC()
		if me != nil {
			c.Me = me
		}
		return c, nil
	}
	return s.update(id, func(x *Conn) error {
		x.State, x.CheckedAt = state, s.now().UTC()
		if me != nil {
			x.Me = me
		}
		return nil
	})
}

// Issues opens a connection's issue source.
func (s *Service) Issues(c Conn) (IssueSource, error) {
	cl, err := s.Open(c)
	if err != nil {
		return nil, err
	}
	src, ok := cl.(IssueSource)
	if !ok {
		return nil, fmt.Errorf("%w: %s has no issues", ErrUnsupported, c.Kind)
	}
	return src, nil
}

// Code opens a connection's code source: it needs the code capability.
func (s *Service) Code(c Conn) (CodeSource, error) {
	if !c.Has(Code) {
		return nil, fmt.Errorf("%w: %s has no code", ErrUnsupported, c.Kind)
	}
	cl, err := s.Open(c)
	if err != nil {
		return nil, err
	}
	src, ok := cl.(CodeSource)
	if !ok {
		return nil, fmt.Errorf("%w: %s has no merge requests", ErrUnsupported, c.Kind)
	}
	return src, nil
}

// Pages opens a connection's page source.
func (s *Service) Pages(c Conn) (PageSource, error) {
	cl, err := s.Open(c)
	if err != nil {
		return nil, err
	}
	src, ok := cl.(PageSource)
	if !ok {
		return nil, fmt.Errorf("%w: %s has no pages", ErrUnsupported, c.Kind)
	}
	return src, nil
}

// Resolve finds which connection a link belongs to and what it points at. ok is false for a link on no connection.
func (s *Service) Resolve(raw string) (Conn, LinkTarget, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return Conn{}, LinkTarget{}, false
	}
	list, err := s.List()
	if err != nil {
		return Conn{}, LinkTarget{}, false
	}
	for _, c := range list {
		base, err := url.Parse(c.BaseURL)
		if err != nil || !strings.EqualFold(base.Host, u.Host) {
			continue
		}
		p := ProviderOf(c.Kind)
		if p == nil {
			continue
		}
		if t, ok := p.Link(c, u); ok {
			return c, t, true
		}
	}
	return Conn{}, LinkTarget{}, false
}
