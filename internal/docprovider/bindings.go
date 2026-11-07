package docprovider

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/ext"
	"github.com/ly-solutions/harnessly/internal/integrations"
)

// Conversation bindings (GitHub issue 26). A document a conversation published is bound to it, so the conversation's
// list of documents (/api/integrations/artifacts/documents?session=) shows it after a reload and a restart of the
// runtime. The built-in catalog keeps that link itself (it lists by session); for an adapter the runtime keeps it, in
// its data directory, one record per document of a conversation:
//
//   - ID is the runtime's own stable id of the binding;
//   - Slug is the alias the reader opens (Alias of the external ID);
//   - ExternalID is the adapter's id; ViewURL is the adapter's official viewer address, as it gave it.
//
// A publish carries an idempotency key: a repeat with the same key after a successful publish answers the binding
// made before and publishes nothing again (the key also reaches the adapter as the Idempotency-Key header, so a repeat
// after a runtime restart is one operation there too). A failure to publish and a failure to bind are told apart:
// *PublishError and *BindError; a provider that does not publish answers ErrUnsupported.

// BindingsFile is the store's file in the runtime's data directory.
const BindingsFile = "doc-bindings.json"

// Binding is one document of one conversation.
type Binding struct {
	ID           string    `json:"id"`
	Conversation string    `json:"conversation"`
	Provider     string    `json:"provider"`
	Slug         string    `json:"slug"`
	ExternalID   string    `json:"externalId"`
	ViewURL      string    `json:"viewUrl,omitempty"`
	Title        string    `json:"title"`
	Version      int       `json:"version"`
	Key          string    `json:"key,omitempty"`
	BoundAt      time.Time `json:"boundAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// PublishError: the provider did not publish; nothing was made and nothing was bound.
type PublishError struct{ Err error }

func (e *PublishError) Error() string { return "docprovider: publish failed: " + e.Err.Error() }
func (e *PublishError) Unwrap() error { return e.Err }

// BindError: the document was published but the binding was not saved. A repeat with the same key binds it without
// publishing again.
type BindError struct {
	Published Published
	Err       error
}

func (e *BindError) Error() string {
	return "docprovider: published " + e.Published.Slug + " but not bound to the conversation: " + e.Err.Error()
}
func (e *BindError) Unwrap() error { return e.Err }

// Bindings is the store. One per data directory in a process (BindingsAt), so the routes that publish and the routes
// that list share it.
type Bindings struct {
	Path string
	mu   sync.Mutex
	// pending are publishes whose binding did not save, by key: a repeat binds them without publishing again.
	pending map[string]Binding
}

var stores sync.Map

// BindingsAt is the store of a data directory.
func BindingsAt(dataDir string) *Bindings {
	path := filepath.Join(dataDir, BindingsFile)
	s, _ := stores.LoadOrStore(path, &Bindings{Path: path})
	return s.(*Bindings)
}

func (b *Bindings) load() ([]Binding, error) {
	raw, err := os.ReadFile(b.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Binding
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("docprovider: %s: %w", b.Path, err)
	}
	return out, nil
}

// saveFile writes the list and is a variable so a test can fail it; nothing else replaces it.
var saveFile = func(path string, list []Binding) error {
	raw, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Of is the bindings of a conversation with one provider, the newest first.
func (b *Bindings) Of(conversation, provider string) ([]Binding, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	all, err := b.load()
	if err != nil {
		return nil, err
	}
	out := []Binding{}
	for _, x := range all {
		if x.Conversation == conversation && x.Provider == provider {
			out = append(out, x)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

func (b *Bindings) byKey(conversation, provider, key string) (Binding, bool, error) {
	if key == "" {
		return Binding{}, false, nil
	}
	all, err := b.load()
	if err != nil {
		return Binding{}, false, err
	}
	for _, x := range all {
		if x.Conversation == conversation && x.Provider == provider && x.Key == key {
			return x, true, nil
		}
	}
	return Binding{}, false, nil
}

// bind saves x: the binding of the same document in the same conversation is updated (its ID and BoundAt kept).
func (b *Bindings) bind(x Binding, now time.Time) (Binding, error) {
	all, err := b.load()
	if err != nil {
		return Binding{}, err
	}
	x.UpdatedAt = now
	found := false
	for i := range all {
		if all[i].Conversation == x.Conversation && all[i].Provider == x.Provider && all[i].ExternalID == x.ExternalID {
			x.ID, x.BoundAt = all[i].ID, all[i].BoundAt
			all[i], found = x, true
			break
		}
	}
	if !found {
		x.ID, x.BoundAt = newBindingID(), now
		all = append(all, x)
	}
	if err := saveFile(b.Path, all); err != nil {
		return Binding{}, err
	}
	return x, nil
}

func newBindingID() string {
	var p [8]byte
	_, _ = rand.Read(p[:])
	return hex.EncodeToString(p[:])
}

// PublishToConversation publishes req through p and binds the document to req.Conversation. key makes a repeat the
// same operation (empty: every call publishes).
func PublishToConversation(ctx context.Context, p Provider, store *Bindings, req PublishRequest, key string) (Binding, error) {
	if req.Conversation == "" {
		return Binding{}, &PublishError{Err: errors.New("no conversation")}
	}
	caps := p.Capabilities()
	if !caps.Publish {
		return Binding{}, ErrUnsupported
	}
	pkey := req.Conversation + "\x00" + key
	store.mu.Lock()
	if prev, ok, err := store.byKey(req.Conversation, caps.Provider, key); err != nil {
		store.mu.Unlock()
		return Binding{}, err
	} else if ok {
		store.mu.Unlock()
		return prev, nil
	}
	if x, ok := store.pending[pkey]; ok && key != "" {
		saved, err := store.bind(x, time.Now())
		if err != nil {
			store.mu.Unlock()
			return Binding{}, &BindError{Published: Published{Slug: x.Slug, ExternalID: x.ExternalID, Version: x.Version, URL: x.ViewURL}, Err: err}
		}
		delete(store.pending, pkey)
		store.mu.Unlock()
		return saved, nil
	}
	store.mu.Unlock()

	if key != "" {
		ctx = ext.WithIdempotencyKey(ctx, "doc-publish:"+req.Conversation+":"+key)
	}
	pub, err := p.Publish(ctx, req)
	switch {
	case errors.Is(err, ErrUnsupported):
		return Binding{}, ErrUnsupported
	case err != nil:
		return Binding{}, &PublishError{Err: err}
	case pub.Slug == "":
		return Binding{}, &PublishError{Err: errors.New("the provider named no document")}
	}
	x := Binding{Conversation: req.Conversation, Provider: caps.Provider, Slug: pub.Slug, ExternalID: pub.ExternalID,
		ViewURL: pub.URL, Title: req.Title, Version: pub.Version, Key: key}
	if x.ExternalID == "" {
		x.ExternalID = pub.Slug
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	saved, err := store.bind(x, time.Now())
	if err != nil {
		if key != "" {
			if store.pending == nil {
				store.pending = map[string]Binding{}
			}
			store.pending[pkey] = x
		}
		return Binding{}, &BindError{Published: pub, Err: err}
	}
	return saved, nil
}

// ConversationLister is a provider that knows by itself which documents a conversation published (the built-in
// catalog lists by session).
type ConversationLister interface {
	ConversationDocuments(ctx context.Context, conversation string) ([]integrations.Document, error)
}

// ConversationDocuments is what a conversation published, newest first: the provider's own list when it keeps one,
// else the runtime's bindings with this provider, each refreshed from the provider when it answers (a document the
// provider no longer has is left out; one it cannot be asked about now is shown as bound).
func ConversationDocuments(ctx context.Context, p Provider, store *Bindings, conversation string) ([]integrations.Document, error) {
	if l, ok := p.(ConversationLister); ok {
		return l.ConversationDocuments(ctx, conversation)
	}
	list, err := store.Of(conversation, p.Capabilities().Provider)
	if err != nil {
		return nil, err
	}
	docs := make([]integrations.Document, 0, len(list))
	for _, b := range list {
		d := integrations.Document{ID: b.ID, Slug: b.Slug, ExternalID: b.ExternalID, Title: b.Title, Version: b.Version,
			UpdatedAt: b.UpdatedAt.UTC().Format(time.RFC3339), ViewURL: b.ViewURL, Provider: b.Provider}
		if s, err := p.Get(ctx, b.Slug); err == nil {
			if s.Title != "" {
				d.Title = s.Title
			}
			if s.URL != "" {
				d.ViewURL = s.URL
			}
		} else {
			var ue *integrations.UpstreamError
			if errors.As(err, &ue) && ue.Status == 404 {
				continue
			}
		}
		docs = append(docs, d)
	}
	return docs, nil
}
