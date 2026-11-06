package memstore

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/ly-solutions/harnessly/internal/files"
	"github.com/ly-solutions/harnessly/internal/hub/ext"
)

// Protocol is what a memory adapter speaks. The adapter is an HTTP service of the extension frame (internal/hub/ext):
// GET /v1/describe, then POST calls with a JSON body, the same way as the connection adapters.
//
//	POST /v1/list   {}                           -> {notes: [Entry]}
//	POST /v1/get    {name}                       -> Note, or 404
//	POST /v1/put    {name, text, ifVersion}      -> Entry; 409 when ifVersion is not the current one
//	POST /v1/search {query, limit}               -> {hits: [Hit]}
//
// The capabilities describe lists are `read` (list and get; required), `write` and `search`. A call of a capability
// that is not listed is not sent.
const Protocol = "harnessly.memory/v1"

var nameRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$`)

// ExtStore is a Store backed by an adapter.
type ExtStore struct {
	cl   *ext.Client
	desc ext.Description
}

// NewExtStore is the store of an adapter that has described itself.
func NewExtStore(c *ext.Client, d ext.Description) (*ExtStore, error) {
	if !nameRE.MatchString(d.Name) {
		return nil, fmt.Errorf("memstore: the adapter's name %q is not a name (lowercase letters, digits, dashes)", d.Name)
	}
	if !d.Has("read") {
		return nil, errors.New("memstore: the adapter does not list the read capability")
	}
	return &ExtStore{cl: c, desc: d}, nil
}

// Register asks the adapter to describe itself and, when it speaks the memory protocol, makes a store of it.
func Register(ctx context.Context, c *ext.Client) (*ExtStore, error) {
	d, err := c.Describe(ctx, Protocol)
	if err != nil {
		return nil, err
	}
	return NewExtStore(c, d)
}

// Name is the adapter's name.
func (s *ExtStore) Name() string { return s.desc.Name }

// Can reports whether the adapter lists a capability.
func (s *ExtStore) Can(capability string) bool { return s.desc.Has(capability) }

func reasonOf(err error) error {
	var e *ext.Error
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ext.ErrUnsupported):
		return ErrUnsupported
	case errors.Is(err, ext.ErrNotFound):
		return ErrNotFound
	case errors.As(err, &e) && e.Status == 409:
		return ErrConflict
	}
	return fmt.Errorf("%w: %v", ErrUnreachable, err)
}

func (s *ExtStore) call(ctx context.Context, capability, path string, in any, mutating bool, out any) error {
	if !s.desc.Has(capability) {
		return ErrUnsupported
	}
	return reasonOf(s.cl.Do(ctx, "POST", path, in, mutating, out))
}

func (s *ExtStore) List(ctx context.Context) ([]Entry, error) {
	var out struct {
		Notes []Entry `json:"notes"`
	}
	if err := s.call(ctx, "read", "/v1/list", map[string]any{}, false, &out); err != nil {
		return nil, err
	}
	return out.Notes, nil
}

func (s *ExtStore) Get(ctx context.Context, name string) (Note, error) {
	if !files.MemoryName(name) {
		return Note{}, ErrNotFound
	}
	var n Note
	if err := s.call(ctx, "read", "/v1/get", map[string]any{"name": name}, false, &n); err != nil {
		return Note{}, err
	}
	n.Name = name
	return n, nil
}

func (s *ExtStore) Put(ctx context.Context, name, text, ifVersion string) (Entry, error) {
	if !files.MemoryName(name) {
		return Entry{}, ErrBadName
	}
	var e Entry
	err := s.call(ctx, "write", "/v1/put", map[string]any{"name": name, "text": text, "ifVersion": ifVersion}, true, &e)
	return e, err
}

// Search uses the adapter's own search when it lists one, and reads the notes otherwise.
func (s *ExtStore) Search(ctx context.Context, query string, limit int) ([]Hit, error) {
	if !s.desc.Has("search") {
		return searchIn(ctx, s, query, limit)
	}
	var out struct {
		Hits []Hit `json:"hits"`
	}
	if err := s.call(ctx, "search", "/v1/search", map[string]any{"query": query, "limit": limit}, false, &out); err != nil {
		return nil, err
	}
	return out.Hits, nil
}

// LoadAdapters makes a store of every memory adapter the file declares (the file of ext.Spec, kind "memory"). Entries
// of other protocols are skipped; one that does not answer is an error and the rest go on.
func LoadAdapters(ctx context.Context, path string) ([]*ExtStore, []error) {
	if path == "" {
		return nil, nil
	}
	specs, err := ext.LoadSpecs(path)
	if err != nil {
		return nil, []error{err}
	}
	var out []*ExtStore
	var errs []error
	for _, sp := range specs {
		if sp.Protocol != Protocol {
			continue
		}
		c, err := ext.NewClient(ext.Config{BaseURL: sp.URL, TokenFile: sp.TokenFile, Timeout: time.Duration(sp.TimeoutSeconds) * time.Second})
		if err == nil {
			var st *ExtStore
			if st, err = Register(ctx, c); err == nil {
				out = append(out, st)
				continue
			}
		}
		errs = append(errs, fmt.Errorf("memory adapter %s: %w", sp.Name, err))
	}
	return out, errs
}
