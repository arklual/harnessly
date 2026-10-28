// Package asks is an agent asking a person: a question or an approval sent through one of the Telegram bots the
// person linked (internal/notifybots), with the person tagged, buttons to answer, and the answer handed back to the
// agent that waits for it. The person may also answer from the browser. The runtime holds every ask in a file of its
// data folder: the page lists them, the agent waits on one, the bot's poller closes them.
package asks

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// File is the asks' file in the runtime's v2 data folder.
const File = "asks.json"

// Keep is how many asks the file keeps: the oldest closed ones go first.
const Keep = 300

// Kind is what the agent wants.
type Kind string

const (
	KindQuestion Kind = "question" // an answer in words (or one of the offered options)
	KindApproval Kind = "approval" // approve or ask for changes
)

// Status is where an ask is.
type Status string

const (
	StatusWaiting   Status = "waiting"   // sent, no answer yet
	StatusAnswered  Status = "answered"  // the person answered
	StatusTimeout   Status = "timeout"   // no answer in time
	StatusCancelled Status = "cancelled" // the agent or the person called it off
	StatusFailed    Status = "failed"    // could not be sent
)

// Final says the ask will not change any more (late replies are still recorded).
func (s Status) Final() bool { return s != StatusWaiting }

// Decisions of an approval.
const (
	DecisionApproved = "approved"
	DecisionChanges  = "changes"
	// DecisionComment is an approval answered in words, not with a button: the agent reads it, it does not approve.
	DecisionComment = "comment"
)

// Person is someone an ask is addressed to or a message is from.
type Person struct {
	Name       string `json:"name,omitempty"`
	Username   string `json:"username,omitempty"` // Telegram @username without the @
	TelegramID int64  `json:"telegramId,omitempty"`
}

// Label is how the person is shown: @username, else the name.
func (p Person) Label() string {
	if p.Username != "" {
		return "@" + p.Username
	}
	return p.Name
}

// Reply is a message of a person that belongs to an ask: the answer, or what was written after it.
type Reply struct {
	From Person `json:"from"`
	Text string `json:"text"`
	// Choice is the button pressed (an approval's decision or an option's text); "" for a message in words.
	Choice string `json:"choice,omitempty"`
	Via    string `json:"via"` // telegram | web
	At     int64  `json:"at"`
}

// Ask is one question or approval as it is stored.
type Ask struct {
	ID       string   `json:"id"`
	Kind     Kind     `json:"kind"`
	Status   Status   `json:"status"`
	Question string   `json:"question"`
	Details  string   `json:"details,omitempty"`
	Link     string   `json:"link,omitempty"`
	Options  []string `json:"options,omitempty"`
	To       *Person  `json:"to,omitempty"`

	BotID     string `json:"botId"`
	BotName   string `json:"botName"`
	ChatTitle string `json:"chatTitle"`
	ChatID    int64  `json:"chatId"`
	MessageID int64  `json:"messageId"`
	// HTML is the sent text, kept to close the message in the chat with the outcome under it.
	HTML   string `json:"html"`
	Silent bool   `json:"silent,omitempty"`

	// Who asked: the agent's tmux session, its conversation, workspace and the conversation's title.
	Session      string `json:"session,omitempty"`
	Conversation string `json:"conversation,omitempty"`
	Workspace    string `json:"workspace,omitempty"`
	Agent        string `json:"agent,omitempty"`

	CreatedAt int64 `json:"createdAt"`
	Deadline  int64 `json:"deadline"`
	ClosedAt  int64 `json:"closedAt,omitempty"`

	Decision string  `json:"decision,omitempty"`
	Answer   *Reply  `json:"answer,omitempty"`
	Replies  []Reply `json:"replies,omitempty"`
	// SeenAt is when the agent took the outcome; SeenReplies how many replies it had read then.
	SeenAt      int64  `json:"seenAt,omitempty"`
	SeenReplies int    `json:"seenReplies,omitempty"`
	Error       string `json:"error,omitempty"`
}

type fileBody struct {
	Asks []Ask `json:"asks"`
	// Offsets are the next Telegram update each bot's poller asks for, so a restart does not read a reply twice.
	Offsets map[string]int64 `json:"offsets,omitempty"`
}

// ErrNotFound: no ask has that id.
var ErrNotFound = errors.New("asks: no such ask")

// Store reads and writes the asks' file; every writer of this process goes through one Store.
type Store struct {
	Path string
	mu   sync.Mutex
}

var (
	storesMu sync.Mutex
	stores   = map[string]*Store{}
)

// Open is the process's one Store of a data folder.
func Open(dataDir string) *Store {
	path := filepath.Join(dataDir, "v2", File)
	storesMu.Lock()
	defer storesMu.Unlock()
	if s := stores[path]; s != nil {
		return s
	}
	s := &Store{Path: path}
	stores[path] = s
	return s
}

func (s *Store) read() (fileBody, error) {
	raw, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return fileBody{}, nil
	}
	if err != nil {
		return fileBody{}, err
	}
	var body fileBody
	if err := json.Unmarshal(raw, &body); err != nil {
		return fileBody{}, err
	}
	return body, nil
}

func (s *Store) write(body fileBody) error {
	if len(body.Asks) > Keep {
		body.Asks = trim(body.Asks)
	}
	return writeFile(s.Path, body)
}

// trim drops the oldest closed asks over Keep; waiting ones stay.
func trim(asks []Ask) []Ask {
	over := len(asks) - Keep
	order := make([]int, len(asks))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return asks[order[a]].CreatedAt < asks[order[b]].CreatedAt })
	drop := map[int]bool{}
	for _, i := range order {
		if over == 0 {
			break
		}
		if asks[i].Status.Final() {
			drop[i] = true
			over--
		}
	}
	kept := make([]Ask, 0, len(asks)-len(drop))
	for i, a := range asks {
		if !drop[i] {
			kept = append(kept, a)
		}
	}
	return kept
}

// writeFile replaces a JSON file through a temporary file beside it, readable by the runtime's user only.
func writeFile(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, werr := f.Write(append(raw, '\n'))
	cerr := f.Close()
	if err := errors.Join(werr, cerr, os.Chmod(tmp, 0o600)); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// List is every ask, newest first.
func (s *Store) List() ([]Ask, error) {
	s.mu.Lock()
	body, err := s.read()
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	out := body.Asks
	sort.SliceStable(out, func(a, b int) bool { return out[a].CreatedAt > out[b].CreatedAt })
	return out, nil
}

// Get is one ask by id.
func (s *Store) Get(id string) (Ask, error) {
	asks, err := s.List()
	if err != nil {
		return Ask{}, err
	}
	for _, a := range asks {
		if a.ID == id {
			return a, nil
		}
	}
	return Ask{}, ErrNotFound
}

// Add stores a new ask.
func (s *Store) Add(a Ask) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	body, err := s.read()
	if err != nil {
		return err
	}
	body.Asks = append(body.Asks, a)
	return s.write(body)
}

// Update changes one ask under the lock; the changed ask is returned. A change that returns errUnchanged writes
// nothing.
func (s *Store) Update(id string, change func(*Ask) error) (Ask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	body, err := s.read()
	if err != nil {
		return Ask{}, err
	}
	for i := range body.Asks {
		if body.Asks[i].ID != id {
			continue
		}
		if err := change(&body.Asks[i]); err != nil {
			return body.Asks[i], err
		}
		return body.Asks[i], s.write(body)
	}
	return Ask{}, ErrNotFound
}

// Offset is the next update a bot's poller asks for.
func (s *Store) Offset(bot string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	body, err := s.read()
	if err != nil {
		return 0
	}
	return body.Offsets[bot]
}

// SetOffset keeps a bot's next update.
func (s *Store) SetOffset(bot string, offset int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	body, err := s.read()
	if err != nil {
		return err
	}
	if body.Offsets == nil {
		body.Offsets = map[string]int64{}
	}
	if body.Offsets[bot] == offset {
		return nil
	}
	body.Offsets[bot] = offset
	return s.write(body)
}
