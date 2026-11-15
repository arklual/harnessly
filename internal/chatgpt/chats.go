package chatgpt

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"
)

// Chat is one conversation with ChatGPT, kept whole in its own file on the machine.
type Chat struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Model     string    `json:"model,omitempty"`
	Effort    string    `json:"effort,omitempty"`
	CreatedAt int64     `json:"createdAt"`
	UpdatedAt int64     `json:"updatedAt"`
	Messages  []Message `json:"messages"`
}

// Message is the person's question or ChatGPT's answer.
type Message struct {
	ID     string  `json:"id"`
	Role   string  `json:"role"` // user | assistant
	Text   string  `json:"text"`
	Files  []File  `json:"files,omitempty"`
	At     int64   `json:"at"`
	Model  string  `json:"model,omitempty"`
	Status string  `json:"status,omitempty"` // done | stopped | failed (assistant)
	Error  *Failed `json:"error,omitempty"`
}

// Failed is why an answer stopped short.
type Failed struct {
	Code   string `json:"code"`
	Detail string `json:"detail,omitempty"`
}

// File is a picture or a document the person attached; its bytes are kept next to the chat.
type File struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Mime string `json:"mime"`
	Size int64  `json:"size"`
}

// ChatSummary is a chat in the list.
type ChatSummary struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Model     string `json:"model,omitempty"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
	Running   bool   `json:"running"`
}

const (
	titleMax = 80
	textMax  = 200_000
	filesMax = 8
	fileMax  = 20 << 20
	// filesTotalMax is how much one message may attach in all.
	filesTotalMax = 32 << 20
	messageMax    = 400
)

var idRe = regexp.MustCompile(`^[a-f0-9]{18}$`)

// chats keeps the conversations: one JSON file each under dir/chats, attachments under dir/files/<chat>/.
type chats struct {
	dir  string
	mu   sync.Mutex
	byID map[string]*Chat
}

var errNoChat = errors.New("chatgpt: no such chat")

func openChats(dir string) *chats {
	c := &chats{dir: dir, byID: map[string]*Chat{}}
	entries, _ := os.ReadDir(filepath.Join(dir, "chats"))
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok || !idRe.MatchString(id) {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, "chats", e.Name()))
		if err != nil {
			continue
		}
		var ch Chat
		if json.Unmarshal(raw, &ch) == nil && ch.ID == id {
			c.byID[id] = &ch
		}
	}
	c.dropEmpty()
	return c
}

// dropEmpty removes the chats nobody wrote in, but the newest: they were drafts left by «New chat».
func (c *chats) dropEmpty() {
	var keep *Chat
	for _, ch := range c.byID {
		if len(ch.Messages) == 0 && (keep == nil || ch.UpdatedAt > keep.UpdatedAt) {
			keep = ch
		}
	}
	for id, ch := range c.byID {
		if len(ch.Messages) == 0 && ch != keep {
			_ = os.Remove(c.path(id))
			_ = os.RemoveAll(filepath.Join(c.dir, "files", id))
			delete(c.byID, id)
		}
	}
}

func (c *chats) path(id string) string { return filepath.Join(c.dir, "chats", id+".json") }

func (c *chats) filePath(chat, file string) string {
	return filepath.Join(c.dir, "files", chat, file)
}

// write keeps a chat's file atomically, readable by the runtime's user only. Called with mu held.
func (c *chats) write(ch *Chat) error {
	if err := os.MkdirAll(filepath.Join(c.dir, "chats"), 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(ch)
	if err != nil {
		return err
	}
	return writeFileAtomic(c.path(ch.ID), raw)
}

func writeFileAtomic(path string, raw []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	return os.Rename(name, path)
}

func (c *chats) list() []ChatSummary {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]ChatSummary, 0, len(c.byID))
	for _, ch := range c.byID {
		out = append(out, ChatSummary{ID: ch.ID, Title: ch.Title, Model: ch.Model, CreatedAt: ch.CreatedAt, UpdatedAt: ch.UpdatedAt})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	return out
}

// get is a deep enough copy of a chat to hand out.
func (c *chats) get(id string) (Chat, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := c.byID[id]
	if ch == nil {
		return Chat{}, errNoChat
	}
	cp := *ch
	cp.Messages = slices.Clone(ch.Messages)
	return cp, nil
}

// createOrReuse keeps a new chat, unless one stands empty and idle: then that one, with the new chat's model, is it.
func (c *chats) createOrReuse(ch *Chat, idle func(id string) bool) (Chat, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, was := range c.byID {
		if len(was.Messages) != 0 || !idle(was.ID) {
			continue
		}
		next := *was
		next.Messages = []Message{}
		if ch.Model != "" {
			next.Model, next.Effort = ch.Model, ch.Effort
		}
		next.UpdatedAt = ch.UpdatedAt
		if err := c.write(&next); err != nil {
			return Chat{}, err
		}
		c.byID[next.ID] = &next
		return next, nil
	}
	if err := c.write(ch); err != nil {
		return Chat{}, err
	}
	c.byID[ch.ID] = ch
	return *ch, nil
}

// change edits a chat under the lock and keeps it.
func (c *chats) change(id string, fn func(ch *Chat) error) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := c.byID[id]
	if ch == nil {
		return errNoChat
	}
	next := *ch
	next.Messages = slices.Clone(ch.Messages)
	if err := fn(&next); err != nil {
		return err
	}
	if err := c.write(&next); err != nil {
		return err
	}
	c.byID[id] = &next
	return nil
}

func (c *chats) remove(id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.byID[id] == nil {
		return errNoChat
	}
	if err := os.Remove(c.path(id)); err != nil && !os.IsNotExist(err) {
		return err
	}
	delete(c.byID, id)
	_ = os.RemoveAll(filepath.Join(c.dir, "files", id))
	return nil
}

// keepFile writes an attachment's bytes next to its chat.
func (c *chats) keepFile(chat, file string, data []byte) error {
	dir := filepath.Join(c.dir, "files", chat)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(dir, file), data)
}

func (c *chats) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.byID)
}

// dropFile removes an attachment that did not make it into its message.
func (c *chats) dropFile(chat, file string) { _ = os.Remove(c.filePath(chat, file)) }

// titleOf is a chat's first title: the start of the first message on one line.
func titleOf(text string) string {
	t := strings.Join(strings.Fields(text), " ")
	if utf8.RuneCountInString(t) > titleMax {
		r := []rune(t)
		t = strings.TrimSpace(string(r[:titleMax-1])) + "…"
	}
	return t
}

// cleanTitle is a title the person typed.
func cleanTitle(s string) string {
	t := strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(t) > titleMax {
		t = string([]rune(t)[:titleMax])
	}
	return t
}
