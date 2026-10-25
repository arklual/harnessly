package background

import (
	"encoding/json"
	"math"
	"regexp"
	"strings"

	"github.com/ly-solutions/harnessly/internal/jscompat"
)

// A named teammate (the agent-teams feature: an Agent call with a `name`) lives beside the agent between its turns. It
// works on what it was sent, then says it is idle and waits for the next message; it is over when it shut down.
const (
	TeammateWorking  = "working"
	TeammateIdle     = "idle"
	TeammateShutDown = "shutdown"
)

// Teammate is one named teammate the transcript started, as its lead last saw it. Times are epoch milliseconds.
type Teammate struct {
	ID          string // the agent id: its transcript is subagents/agent-<ID>.jsonl beside the lead's
	Name        string // what the lead sends to
	Description string
	Model       string // what the spawn asked for, "" when it did not say
	State       string
	SpawnedAt   Ms
	// WorkFrom is when its current stretch of work began (spawned or sent a message); Since is when it went idle or
	// shut down, 0 while it works.
	WorkFrom Ms
	Since    Ms
	poked    float64 // the last message sent to it: an idle notice older than that is about the work before it
}

// teammatesKeep bounds the teammates a scan remembers; the ones that shut down go first.
const teammatesKeep = 50

var (
	spawned        = regexp.MustCompile(`(?s)Spawned successfully.*?agent_id: ([\w@.-]+)`)
	spawnedName    = regexp.MustCompile(`\bname: ([\w@.-]+)`)
	teammateMsg    = regexp.MustCompile(`(?s)<teammate-message teammate_id="([^"]+)"[^>]*>(.*?)</teammate-message>`)
	hasShutDown    = regexp.MustCompile(`^(\S+) has shut down`)
	teammateTarget = []string{"to", "recipient"}
)

// team is what the transcript said of its teammates, by name in the order they were started.
type team struct {
	order []string
	by    map[string]*Teammate
}

func (t *team) get(name string) *Teammate {
	if t.by == nil {
		return nil
	}
	return t.by[name]
}

// spawn starts a teammate. The same spawn read again (a request copy of the call) changes nothing; a new teammate
// under a name that ended starts over.
func (t *team) spawn(use toolUse, text string) {
	m := spawned.FindStringSubmatch(text)
	if m == nil {
		return
	}
	name, _ := use.input["name"].(string)
	if n := spawnedName.FindStringSubmatch(text); n != nil {
		name = n[1]
	}
	if name == "" {
		return
	}
	if have := t.get(name); have != nil && have.ID == m[1] {
		return
	}
	if t.by == nil {
		t.by = map[string]*Teammate{}
	}
	desc, _ := use.input["description"].(string)
	model, _ := use.input["model"].(string)
	if _, had := t.by[name]; had {
		t.drop(name)
	}
	t.by[name] = &Teammate{ID: m[1], Name: name, Description: clip(desc), Model: model, State: TeammateWorking,
		SpawnedAt: Ms(use.at), WorkFrom: Ms(use.at), poked: use.at}
	t.order = append(t.order, name)
	t.trim()
}

func (t *team) drop(name string) {
	delete(t.by, name)
	for i, x := range t.order {
		if x == name {
			t.order = append(t.order[:i], t.order[i+1:]...)
			return
		}
	}
}

// trim forgets the oldest teammates that shut down, then the oldest at all, past teammatesKeep.
func (t *team) trim() {
	for len(t.order) > teammatesKeep {
		victim := t.order[0]
		for _, name := range t.order {
			if t.by[name].State == TeammateShutDown {
				victim = name
				break
			}
		}
		t.drop(victim)
	}
}

// poke is a message sent to a teammate ("*" is every one): it works on it from then on.
func (t *team) poke(to string, at float64) {
	for _, name := range t.order {
		if to != "*" && name != to {
			continue
		}
		mate := t.by[name]
		if mate.State == TeammateShutDown {
			continue
		}
		mate.poked = math.Max(mate.poked, at)
		if mate.State == TeammateIdle {
			mate.State, mate.WorkFrom, mate.Since = TeammateWorking, Ms(at), 0
		}
	}
}

// messages reads the teammate messages the lead was given: an idle notice, an approved shutdown, the system's word
// that one shut down. Plain messages from a working teammate change nothing.
func (t *team) messages(text string, at float64) {
	if len(t.order) == 0 || !strings.Contains(text, "<teammate-message") {
		return
	}
	for _, m := range teammateMsg.FindAllStringSubmatch(text, -1) {
		var body struct {
			Type      string `json:"type"`
			From      string `json:"from"`
			Timestamp any    `json:"timestamp"`
			Message   string `json:"message"`
		}
		if json.Unmarshal([]byte(jscompat.Trim(m[2])), &body) != nil {
			continue
		}
		when := at
		if jscompat.Truthy(body.Timestamp) {
			if v := jscompat.DateValue(body.Timestamp); !math.IsNaN(v) {
				when = v
			}
		}
		name := body.From
		if name == "" {
			name = m[1]
		}
		switch body.Type {
		case "idle_notification":
			mate := t.get(name)
			// A notice that comes after a message sent later is about the work before that message.
			if mate != nil && mate.State == TeammateWorking && when >= mate.poked {
				mate.State, mate.Since = TeammateIdle, Ms(when)
			}
		case "shutdown_approved":
			t.shutDown(name, when)
		case "teammate_terminated":
			if s := hasShutDown.FindStringSubmatch(body.Message); s != nil {
				t.shutDown(s[1], when)
			}
		}
	}
}

// shutDown: the system's word and the teammate's own approval come together; the approval says when.
func (t *team) shutDown(name string, at float64) {
	mate := t.get(name)
	switch {
	case mate == nil:
	case mate.State != TeammateShutDown:
		mate.State, mate.Since = TeammateShutDown, Ms(at)
	case at < float64(mate.Since):
		mate.Since = Ms(at)
	}
}

// list is every teammate, the latest started first.
func (t *team) list() []Teammate {
	out := make([]Teammate, 0, len(t.order))
	for i := len(t.order) - 1; i >= 0; i-- {
		out = append(out, *t.by[t.order[i]])
	}
	return out
}

// teammateTo is whom a SendMessage call writes to.
func teammateTo(input map[string]any) string {
	for _, k := range teammateTarget {
		if s, ok := input[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

// leadLine: a line the lead's own conversation is made of. The request copies of the API calls hold the same blocks
// again, later and without a time, and must not wake a teammate a second time.
func leadLine(typ string) bool {
	return typ == "" || typ == "user" || typ == "assistant"
}

// userText is what a user line says in words: its text, not what tools returned (a tool may well print a teammate
// message it read from a file).
func userText(content any) string {
	switch c := content.(type) {
	case string:
		return c
	case []any:
		var parts []string
		for _, item := range c {
			if m, ok := item.(map[string]any); ok && m["type"] == "text" {
				if s, ok := m["text"].(string); ok {
					parts = append(parts, s)
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

// ParseTeammates is the teammates a whole transcript started, as it leaves them; now is epoch milliseconds.
func ParseTeammates(lines []string, now float64) []Teammate {
	s := newScan()
	s.lines(lines, now)
	return s.team.list()
}
