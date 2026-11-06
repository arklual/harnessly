package secrets

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// KV is the key/value engine of a Vault-compatible server, one mount (internal/secrets/vaultkv).
type KV interface {
	Read(ctx context.Context, path string) (map[string]string, error)
	Write(ctx context.Context, path string, data map[string]string) error
	Delete(ctx context.Context, path string) error
	List(ctx context.Context, path string) ([]string, error)
}

// ValueKey is the key a secret's value is kept under at its path.
const ValueKey = "value"

// remoteTimeout bounds one call of the safe to the server.
const remoteTimeout = 20 * time.Second

// listFresh is how long the list of names is reused (Has, the ask's check); values are never kept.
const listFresh = 10 * time.Second

// Remote keeps the person's secrets on a Vault-compatible server, one path each under Prefix (<prefix>/<name>, the
// value under the key "value"). Only the value is there; when it was put and who used it stay on this machine, in a
// ledger without values (<dir>/remote-ledger.json), as the file safe keeps them.
type Remote struct {
	KV KV
	// Prefix is the folder of the person's secrets in the mount, such as harnessly/users/alice.
	Prefix string
	// RefPrefixes are the folders vault: references may read; "*" any path the login may read; none refuses them.
	RefPrefixes []string
	Now         func() time.Time

	dir string
	mu  sync.Mutex
	// names is the last list, with when it was taken.
	names   map[string]bool
	namesAt time.Time
}

// NewRemote is a server safe under prefix, its ledger in dir.
func NewRemote(kv KV, prefix, dir string) *Remote {
	return &Remote{KV: kv, Prefix: strings.Trim(prefix, "/"), dir: dir, Now: time.Now}
}

var _ Provider = (*Remote)(nil)

func (r *Remote) path(name string) string {
	if r.Prefix == "" {
		return name
	}
	return r.Prefix + "/" + name
}

func ctxCall() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), remoteTimeout)
}

type ledgerFile struct {
	Version int              `json:"version"`
	Secrets map[string]*View `json:"secrets"`
}

func (r *Remote) ledgerPath() string { return filepath.Join(r.dir, "remote-ledger.json") }

func (r *Remote) readLedger() *ledgerFile {
	f := &ledgerFile{Version: 1, Secrets: map[string]*View{}}
	raw, err := os.ReadFile(r.ledgerPath())
	if err == nil {
		_ = json.Unmarshal(raw, f)
	}
	if f.Secrets == nil {
		f.Secrets = map[string]*View{}
	}
	return f
}

// writeLedger keeps the ledger; it holds no value, and losing it loses only dates and uses, so a failure is ignored.
func (r *Remote) writeLedger(f *ledgerFile) {
	if err := os.MkdirAll(r.dir, 0o700); err != nil {
		return
	}
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return
	}
	tmp, err := os.CreateTemp(r.dir, ".remote-ledger-*.json")
	if err != nil {
		return
	}
	name := tmp.Name()
	_, werr := tmp.Write(raw)
	cerr := tmp.Close()
	if werr != nil || cerr != nil || os.Rename(name, r.ledgerPath()) != nil {
		_ = os.Remove(name)
	}
}

func (r *Remote) now() int64 { return r.Now().UnixMilli() }

// listNames asks the server for the names under Prefix (fresh: not the remembered list).
func (r *Remote) listNames(fresh bool) (map[string]bool, error) {
	r.mu.Lock()
	if !fresh && r.names != nil && r.Now().Sub(r.namesAt) < listFresh {
		names := r.names
		r.mu.Unlock()
		return names, nil
	}
	r.mu.Unlock()
	ctx, cancel := ctxCall()
	defer cancel()
	keys, err := r.KV.List(ctx, r.Prefix)
	if err != nil {
		return nil, err
	}
	names := make(map[string]bool, len(keys))
	for _, k := range keys {
		// A folder (k/) or a key the safe could not name is not one of its secrets.
		if ValidName(k) {
			names[k] = true
		}
	}
	r.mu.Lock()
	r.names, r.namesAt = names, r.Now()
	r.mu.Unlock()
	return names, nil
}

func (r *Remote) forgetList() {
	r.mu.Lock()
	r.names = nil
	r.mu.Unlock()
}

// List is the secrets under Prefix with what the ledger knows of them.
func (r *Remote) List() ([]View, error) {
	names, err := r.listNames(true)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	led := r.readLedger()
	r.mu.Unlock()
	out := make([]View, 0, len(names))
	for name := range names {
		v := View{Name: name, UsedBy: []Use{}}
		if e := led.Secrets[name]; e != nil {
			v = *e
			v.Name = name
			v.UsedBy = append([]Use{}, e.UsedBy...)
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}

// Has says whether the server keeps a secret of that name (by a list at most 10 s old).
func (r *Remote) Has(name string) bool {
	names, err := r.listNames(false)
	return err == nil && names[name]
}

// Put writes the value to the server.
func (r *Remote) Put(name, value string) (View, bool, error) {
	return r.put(name, value, nil)
}

// PutAsked writes a value a card asked for: a secret of another project is not overwritten, and a new one is limited
// to the project that asked, as in the file.
func (r *Remote) PutAsked(a Ask, value string) (View, bool, error) {
	if a.NeedsGrant() {
		return View{}, false, refuse(ReasonNotFound, nil)
	}
	return r.put(a.Name, value, &a)
}

func (r *Remote) put(name, value string, asked *Ask) (View, bool, error) {
	if !ValidName(name) {
		return View{}, false, refuse(ReasonBadName, nil)
	}
	value = Clean(value)
	if value == "" {
		return View{}, false, refuse(ReasonEmpty, nil)
	}
	if len(value) > MaxValue {
		return View{}, false, refuse(ReasonTooLarge, nil)
	}
	created := !r.Has(name)
	if asked != nil && !created {
		r.mu.Lock()
		e := r.readLedger().Secrets[name]
		r.mu.Unlock()
		if e != nil && !e.Allowed(asked.Project) {
			return View{}, false, refuse(ReasonNotFound, nil)
		}
	}
	ctx, cancel := ctxCall()
	defer cancel()
	if err := r.KV.Write(ctx, r.path(name), map[string]string{ValueKey: value}); err != nil {
		return View{}, false, err
	}
	r.forgetList()
	r.mu.Lock()
	defer r.mu.Unlock()
	led := r.readLedger()
	now := r.now()
	e := led.Secrets[name]
	if e == nil || created {
		e = &View{Name: name, CreatedAt: now, UsedBy: []Use{}}
		led.Secrets[name] = e
		if asked != nil && asked.Project != "" {
			e.Projects = []string{asked.Project}
		}
	}
	if asked != nil && asked.Origin != "" && !slices.Contains(e.Origins, asked.Origin) {
		e.Origins = append(e.Origins, asked.Origin)
	}
	e.UpdatedAt = now
	r.writeLedger(led)
	return *e, created, nil
}

// entry is what the ledger knows of a secret the server keeps; nil when the server has no such name.
func (r *Remote) entry(name string) *View {
	if !ValidName(name) || !r.Has(name) {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if e := r.readLedger().Secrets[name]; e != nil {
		v := *e
		v.Name = name
		return &v
	}
	return &View{Name: name, UsedBy: []Use{}}
}

// Lookup is the public view of a secret for a project's chat; a foreign one looks absent. Its limits live in the
// ledger of this machine, beside its dates: the server keeps only the value.
func (r *Remote) Lookup(name, project string) (View, error) {
	e := r.entry(name)
	if e == nil || !e.Allowed(project) {
		return View{}, refuse(ReasonNotFound, nil)
	}
	return *e, nil
}

// Metadata changes the limits of a secret, never its value.
func (r *Remote) Metadata(name string, origins, projects []string) (View, error) {
	sites, scopes, err := cleanAccess(origins, projects)
	if err != nil {
		return View{}, err
	}
	if r.entry(name) == nil {
		return View{}, refuse(ReasonNotFound, nil)
	}
	return r.change(name, func(e *View) { e.Origins, e.Projects = sites, scopes }), nil
}

// Grant adds a project and a site after a person answered a card.
func (r *Remote) Grant(name, project, origin string) error {
	return r.grant(name, project, origin, true)
}

// GrantOrigin adds only a site; the project must already be allowed.
func (r *Remote) GrantOrigin(name, project, origin string) error {
	if origin == "" {
		return refuse("bad-origin", nil)
	}
	return r.grant(name, project, origin, false)
}

func (r *Remote) grant(name, project, origin string, projectGrant bool) error {
	if projectGrant && project == "" {
		return refuse(ReasonNotFound, nil)
	}
	if origin != "" {
		if origin = Origin(origin); origin == "" {
			return refuse("bad-origin", nil)
		}
	}
	e := r.entry(name)
	if e == nil || !projectGrant && !e.Allowed(project) {
		return refuse(ReasonNotFound, nil)
	}
	r.change(name, func(e *View) {
		if projectGrant && len(e.Projects) > 0 && !slices.Contains(e.Projects, project) {
			e.Projects = append(e.Projects, project)
		}
		if origin != "" && !slices.Contains(e.Origins, origin) {
			e.Origins = append(e.Origins, origin)
		}
	})
	return nil
}

// change edits the ledger's entry of a name and gives it back.
func (r *Remote) change(name string, edit func(*View)) View {
	r.mu.Lock()
	defer r.mu.Unlock()
	led := r.readLedger()
	e := led.Secrets[name]
	if e == nil {
		e = &View{Name: name, UsedBy: []Use{}}
		led.Secrets[name] = e
	}
	edit(e)
	e.UpdatedAt = r.now()
	r.writeLedger(led)
	return *e
}

// Remove deletes the secret on the server.
func (r *Remote) Remove(name string) error {
	if !ValidName(name) {
		return refuse(ReasonBadName, nil)
	}
	names, err := r.listNames(true)
	if err != nil {
		return err
	}
	if !names[name] {
		return refuse(ReasonNotFound, nil)
	}
	ctx, cancel := ctxCall()
	defer cancel()
	if err := r.KV.Delete(ctx, r.path(name)); err != nil {
		return err
	}
	r.forgetList()
	r.mu.Lock()
	defer r.mu.Unlock()
	led := r.readLedger()
	delete(led.Secrets, name)
	r.writeLedger(led)
	return nil
}

// valueOf is the secret at a path: its "value", or the only key there is.
func valueOf(data map[string]string, key string) (string, bool) {
	if v, ok := data[key]; ok {
		return v, v != ""
	}
	if key == ValueKey && len(data) == 1 {
		for _, v := range data {
			return v, v != ""
		}
	}
	return "", false
}

// Reveal reads the values from the server and records the use in the ledger.
func (r *Remote) Reveal(names []string, use Use) (map[string]string, error) {
	out := make(map[string]string, len(names))
	r.mu.Lock()
	limits := r.readLedger()
	r.mu.Unlock()
	for _, name := range names {
		if !ValidName(name) {
			return nil, refuse(ReasonBadName, errors.New(name))
		}
		if e := limits.Secrets[name]; e != nil {
			if !e.Allowed(use.Project) {
				return nil, refuse(ReasonNotFound, errors.New(name))
			}
			if use.Origin != "" && !slices.Contains(e.Origins, use.Origin) {
				return nil, refuse("origin-denied", nil)
			}
		} else if use.Origin != "" {
			return nil, refuse("origin-denied", nil)
		}
	}
	ctx, cancel := ctxCall()
	defer cancel()
	for _, name := range names {
		data, err := r.KV.Read(ctx, r.path(name))
		if err != nil {
			if ReasonOf(err) == ReasonNotFound {
				return nil, refuse(ReasonNotFound, errors.New(name))
			}
			return nil, err
		}
		v, ok := valueOf(data, ValueKey)
		if !ok {
			return nil, refuse(ReasonNotFound, errors.New(name))
		}
		out[name] = v
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	led := r.readLedger()
	use.At = r.now()
	for _, name := range names {
		e := led.Secrets[name]
		if e == nil {
			e = &View{Name: name, UsedBy: []Use{}}
			led.Secrets[name] = e
		}
		recordUse(e, use)
	}
	r.writeLedger(led)
	return out, nil
}

// ReadRef reads key at path, when path lies under one of RefPrefixes.
func (r *Remote) ReadRef(path, key string) (string, error) {
	if !r.refAllowed(path) {
		return "", refuse(ReasonDenied, errors.New("the path is outside the references this safe may read"))
	}
	ctx, cancel := ctxCall()
	defer cancel()
	data, err := r.KV.Read(ctx, path)
	if err != nil {
		return "", err
	}
	v, ok := valueOf(data, key)
	if !ok {
		return "", refuse(ReasonNotFound, errors.New("no such key"))
	}
	return v, nil
}

func (r *Remote) refAllowed(path string) bool {
	for _, p := range r.RefPrefixes {
		p = strings.Trim(strings.TrimSpace(p), "/")
		if p == "*" {
			return true
		}
		if p != "" && (path == p || strings.HasPrefix(path, p+"/")) {
			return true
		}
	}
	return false
}
