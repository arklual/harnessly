package integrations

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Comment threads on documents, kept by the runtime in one file of its data directory: the catalog has no threads of
// its own. A thread hangs on a quote of the document (a text-quote anchor) and holds the comments under it.

// DocAnchor is where a thread hangs: the quoted words, their context and the nearest heading.
type DocAnchor struct {
	Quote       string   `json:"quote"`
	Prefix      string   `json:"prefix"`
	Suffix      string   `json:"suffix"`
	HeadingID   string   `json:"headingId,omitempty"`
	HeadingText string   `json:"headingText,omitempty"`
	ElementID   string   `json:"elementId,omitempty"`
	X           *float64 `json:"x,omitempty"`
	Y           *float64 `json:"y,omitempty"`
}

// DocComment is one message of a thread.
type DocComment struct {
	ID            string    `json:"id"`
	ThreadID      string    `json:"threadId"`
	Author        DocAuthor `json:"author"`
	Body          string    `json:"body"`
	CreatedAt     string    `json:"createdAt"`
	DeliveredAt   *string   `json:"deliveredAt"`
	DeliveryError *string   `json:"deliveryError"`
}

// DocThread is a discussion on one place of a document.
type DocThread struct {
	ID         string       `json:"id"`
	ExternalID *string      `json:"externalId"`
	Slug       string       `json:"slug"`
	Anchor     DocAnchor    `json:"anchor"`
	Status     string       `json:"status"`
	CreatedBy  DocAuthor    `json:"createdBy"`
	CreatedAt  string       `json:"createdAt"`
	ResolvedAt *string      `json:"resolvedAt"`
	Comments   []DocComment `json:"comments"`
}

// ErrNoThread: the thread is not on that document.
var ErrNoThread = errors.New("no such thread")

type threadFile struct {
	Threads []DocThread `json:"threads"`
	// Read is when each person last read each document: person → slug → ISO time.
	Read map[string]map[string]string `json:"read"`
}

// Threads is the file-backed thread store.
type Threads struct {
	Path string
	Now  func() time.Time
	mu   sync.Mutex
}

func (t *Threads) now() time.Time {
	if t.Now != nil {
		return t.Now()
	}
	return time.Now()
}

func (t *Threads) load() (threadFile, error) {
	var f threadFile
	raw, err := os.ReadFile(t.Path)
	if errors.Is(err, os.ErrNotExist) {
		return threadFile{Read: map[string]map[string]string{}}, nil
	}
	if err != nil {
		return f, err
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		return f, err
	}
	if f.Read == nil {
		f.Read = map[string]map[string]string{}
	}
	return f, nil
}

func (t *Threads) save(f threadFile) error {
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(t.Path), 0o700); err != nil {
		return err
	}
	tmp := t.Path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, t.Path)
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Of is a document's threads, oldest first.
func (t *Threads) Of(slug string) ([]DocThread, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	f, err := t.load()
	if err != nil {
		return nil, err
	}
	out := []DocThread{}
	for _, th := range f.Threads {
		if th.Slug == slug {
			out = append(out, th)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt < out[j].CreatedAt })
	return out, nil
}

// Create starts a thread with its first comment.
func (t *Threads) Create(slug string, anchor DocAnchor, body string, by DocAuthor) (DocThread, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	f, err := t.load()
	if err != nil {
		return DocThread{}, err
	}
	at := t.now().UTC().Format(time.RFC3339Nano)
	th := DocThread{ID: newID(), Slug: slug, Anchor: anchor, Status: "open", CreatedBy: by, CreatedAt: at}
	th.Comments = []DocComment{{ID: newID(), ThreadID: th.ID, Author: by, Body: body, CreatedAt: at}}
	f.Threads = append(f.Threads, th)
	return th, t.save(f)
}

func (t *Threads) update(slug, id string, change func(*DocThread)) (DocThread, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	f, err := t.load()
	if err != nil {
		return DocThread{}, err
	}
	for i := range f.Threads {
		if f.Threads[i].ID == id && f.Threads[i].Slug == slug {
			change(&f.Threads[i])
			return f.Threads[i], t.save(f)
		}
	}
	return DocThread{}, ErrNoThread
}

// Reply adds a comment to a thread and reopens it.
func (t *Threads) Reply(slug, id, body string, by DocAuthor) (DocThread, error) {
	at := t.now().UTC().Format(time.RFC3339Nano)
	return t.update(slug, id, func(th *DocThread) {
		th.Comments = append(th.Comments, DocComment{ID: newID(), ThreadID: th.ID, Author: by, Body: body, CreatedAt: at})
		th.Status, th.ResolvedAt = "open", nil
	})
}

// SetStatus resolves or reopens a thread.
func (t *Threads) SetStatus(slug, id, status string) (DocThread, error) {
	at := t.now().UTC().Format(time.RFC3339Nano)
	return t.update(slug, id, func(th *DocThread) {
		th.Status = status
		if status == "resolved" {
			th.ResolvedAt = &at
		} else {
			th.ResolvedAt = nil
		}
	})
}

// MarkRead records that a person read a document now.
func (t *Threads) MarkRead(person, slug string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	f, err := t.load()
	if err != nil {
		return err
	}
	if f.Read[person] == nil {
		f.Read[person] = map[string]string{}
	}
	f.Read[person][slug] = t.now().UTC().Format(time.RFC3339Nano)
	return t.save(f)
}

// Counts is open threads and comments newer than the person's last read, per document.
func (t *Threads) Counts(person string) (open, unread map[string]int, last map[string]string, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	f, err := t.load()
	if err != nil {
		return nil, nil, nil, err
	}
	open, unread, last = map[string]int{}, map[string]int{}, map[string]string{}
	read := f.Read[person]
	for _, th := range f.Threads {
		if th.Status == "open" {
			open[th.Slug]++
		}
		for _, c := range th.Comments {
			if c.CreatedAt > last[th.Slug] {
				last[th.Slug] = c.CreatedAt
			}
			if c.Author.Email != person && c.CreatedAt > read[th.Slug] {
				unread[th.Slug]++
			}
		}
	}
	return open, unread, last, nil
}
