package peers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Party is one end of a message: a machine, its owner and a chat on it.
type Party struct {
	Person      string `json:"person"`
	PersonName  string `json:"personName"`
	Machine     string `json:"machine"`
	MachineName string `json:"machineName"`
	Conv        string `json:"conversationId"`
	Title       string `json:"title"`
}

func partyOf(s Sender, conv, title string) Party {
	return Party{Person: s.Person, PersonName: s.PersonName, Machine: s.Machine, MachineName: s.MachineName, Conv: conv, Title: title}
}

// Direction of an audit record: in (to this machine's agents), out (to another machine).
type Direction string

const (
	DirIn  Direction = "in"
	DirOut Direction = "out"
)

// State of an audited message.
type State string

const (
	StateDelivered State = "delivered" // the conversation took it: it is in its agent's queue or turn
	StateRefused   State = "refused"   // refused before it reached any agent; Reason says why
)

// Record is one message as the audit keeps it: who, to whom, the beginning of the text, and what came of it.
type Record struct {
	ID     string    `json:"id"`
	At     int64     `json:"at"`
	Dir    Direction `json:"dir"`
	From   Party     `json:"from"`
	To     Party     `json:"to"`
	Text   string    `json:"text"`
	State  State     `json:"state"`
	Reason Refusal   `json:"reason"`
	// Via: "agent", or "person" for a reply typed in the card.
	Via string `json:"via"`
}

// AuditKept is how many records the audit keeps, the newest; AuditText how much of a text.
const (
	AuditKept = 200
	AuditText = 280
)

// Audit is the recent messages between agents, newest first, kept in a file of the runtime's data folder.
type Audit struct {
	Path string
	mu   sync.Mutex
	recs []Record
	read bool
}

func (a *Audit) load() {
	if a.read {
		return
	}
	a.read = true
	raw, err := os.ReadFile(a.Path)
	if err != nil {
		return
	}
	var recs []Record
	if json.Unmarshal(raw, &recs) == nil {
		a.recs = recs
	}
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// Add keeps r as the newest record.
func (a *Audit) Add(r Record) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.load()
	r.Text = clip(r.Text, AuditText)
	a.recs = append([]Record{r}, a.recs...)
	if len(a.recs) > AuditKept {
		a.recs = a.recs[:AuditKept]
	}
	if a.Path == "" {
		return
	}
	raw, _ := json.Marshal(a.recs)
	if os.MkdirAll(filepath.Dir(a.Path), 0o700) != nil {
		return
	}
	tmp := a.Path + ".tmp"
	if os.WriteFile(tmp, raw, 0o600) == nil {
		_ = os.Rename(tmp, a.Path)
	}
}

// Recent is up to n records, newest first.
func (a *Audit) Recent(n int) []Record {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.load()
	if n > len(a.recs) {
		n = len(a.recs)
	}
	return append([]Record{}, a.recs[:n]...)
}

// Senders is every machine and person that wrote here, once each, the most recent first: the rows the settings offer
// a rule for.
func (a *Audit) Senders() []Sender {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.load()
	seen := map[string]bool{}
	out := []Sender{}
	for _, r := range a.recs {
		if r.Dir != DirIn || seen[r.From.Machine+"\x00"+r.From.Person] {
			continue
		}
		seen[r.From.Machine+"\x00"+r.From.Person] = true
		out = append(out, Sender{Person: r.From.Person, PersonName: r.From.PersonName, Machine: r.From.Machine,
			MachineName: r.From.MachineName})
	}
	return out
}
