package peers

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// Scope is who may write when no rule names them: the fleet (every machine and person the gateway vouches for, the
// default) or only those the list allows.
type Scope string

const (
	ScopeTeam Scope = "team"
	ScopeList Scope = "list"
)

// Delivery is when a message from another agent reaches a working agent: at its next step, inside the running turn
// (the default), or once the turn is over.
type Delivery string

const (
	DeliveryStep Delivery = "step"
	DeliveryTurn Delivery = "turn"
)

// RuleKind says what a rule names.
type RuleKind string

const (
	RuleMachine RuleKind = "machine"
	RulePerson  RuleKind = "person"
)

// Rule allows or blocks one machine or one person, whatever the scope says.
type Rule struct {
	Kind  RuleKind `json:"kind"`
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Allow bool     `json:"allow"`
}

// Settings is the «Связь между агентами» section of Settings → Машины.
type Settings struct {
	// Accept: messages from other agents are taken at all.
	Accept   bool     `json:"accept"`
	Scope    Scope    `json:"scope"`
	Delivery Delivery `json:"delivery"`
	Rules    []Rule   `json:"rules"`
	Rev      int64    `json:"rev"`
}

// Defaults: on, for the whole fleet, at the agent's next step.
func Defaults() Settings {
	return Settings{Accept: true, Scope: ScopeTeam, Delivery: DeliveryStep, Rules: []Rule{}}
}

// Refusal is why a message was not taken: a code the sender's CLI prints and the page translates.
type Refusal string

const (
	RefuseOff         Refusal = "off"          // the machine takes no messages from other agents
	RefuseBlocked     Refusal = "blocked"      // a rule blocks the sender's machine or person
	RefuseNotAllowed  Refusal = "not-allowed"  // only the list may write, and the sender is not on it
	RefuseUnverified  Refusal = "unverified"   // the sender's identity did not check out
	RefuseUnknownChat Refusal = "unknown-chat" // no such conversation here
	RefuseAgentGone   Refusal = "agent-gone"   // the conversation has no agent to read it
	RefuseEmpty       Refusal = "empty"
	RefuseTooLong     Refusal = "too-long"
	RefuseRateLimited Refusal = "rate-limited" // the sender wrote too much in the last minute
	RefuseNoRelay     Refusal = "no-relay"     // this machine has no way to the other machines
	RefuseUnreachable Refusal = "unreachable"  // the other machine did not answer
	RefuseBadRef      Refusal = "bad-ref"
	RefuseFailed      Refusal = "failed" // the conversation did not take it (its reason is in the detail)
)

const maxRules = 200

var ruleID = regexp.MustCompile(`^[A-Za-z0-9._@+-]{1,128}$`)

// Validate says whether the settings can be kept.
func (s Settings) Validate() error {
	if s.Scope != ScopeTeam && s.Scope != ScopeList {
		return errors.New("scope: team or list")
	}
	if s.Delivery != DeliveryStep && s.Delivery != DeliveryTurn {
		return errors.New("delivery: step or turn")
	}
	if len(s.Rules) > maxRules {
		return errors.New("rules: too many")
	}
	seen := map[string]bool{}
	for _, r := range s.Rules {
		if r.Kind != RuleMachine && r.Kind != RulePerson {
			return errors.New("rules: kind is machine or person")
		}
		if !ruleID.MatchString(r.ID) {
			return errors.New("rules: bad id")
		}
		if len(r.Name) > 200 {
			return errors.New("rules: name too long")
		}
		key := string(r.Kind) + ":" + strings.ToLower(r.ID)
		if seen[key] {
			return errors.New("rules: the same id twice")
		}
		seen[key] = true
	}
	return nil
}

// Decide says whether sender may write to this machine's agents: a block names it first, then an allow, then the
// scope. It never looks at the message.
func (s Settings) Decide(sender Sender) (bool, Refusal) {
	if !s.Accept {
		return false, RefuseOff
	}
	allowed := false
	for _, r := range s.Rules {
		if !r.names(sender) {
			continue
		}
		if !r.Allow {
			return false, RefuseBlocked
		}
		allowed = true
	}
	if allowed || s.Scope == ScopeTeam {
		return true, ""
	}
	return false, RefuseNotAllowed
}

func (r Rule) names(s Sender) bool {
	switch r.Kind {
	case RuleMachine:
		return strings.EqualFold(r.ID, s.Machine)
	case RulePerson:
		return s.Person != "" && strings.EqualFold(r.ID, s.Person)
	}
	return false
}

// Store keeps the settings in a file of the runtime's data folder; a missing or unreadable file is the defaults.
type Store struct {
	Path string
	mu   sync.Mutex
}

// Get is the settings as kept.
func (st *Store) Get() Settings {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.read()
}

func (st *Store) read() Settings {
	s := Defaults()
	raw, err := os.ReadFile(st.Path)
	if err != nil {
		return s
	}
	var got Settings
	if json.Unmarshal(raw, &got) != nil || got.Validate() != nil {
		return s
	}
	if got.Rules == nil {
		got.Rules = []Rule{}
	}
	return got
}

// ErrConflict: the settings changed since the revision the change was made against.
var ErrConflict = errors.New("conflict")

// Put keeps next if the kept settings are still at next.Rev; the answer is what is kept now.
func (st *Store) Put(next Settings) (Settings, error) {
	if next.Rules == nil {
		next.Rules = []Rule{}
	}
	if err := next.Validate(); err != nil {
		return Settings{}, err
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	cur := st.read()
	if next.Rev != cur.Rev {
		return cur, ErrConflict
	}
	next.Rev = cur.Rev + 1
	raw, _ := json.MarshalIndent(next, "", "  ")
	if err := os.MkdirAll(filepath.Dir(st.Path), 0o700); err != nil {
		return cur, err
	}
	tmp := st.Path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return cur, err
	}
	if err := os.Rename(tmp, st.Path); err != nil {
		return cur, err
	}
	return next, nil
}
