// Package history keeps what the core remembers about finished turns: session-history.json, the newest 200 turns for
// the notification list, and session-stats/<id>.json, the last statusline figures of each agent session.
package history

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/nodejson"
)

// MaxEntries is how many turns the list keeps.
const MaxEntries = 200

// wireTextLimit: the list shows prompt and result on one truncated line; full texts made the first sync ~520 KB.
const wireTextLimit = 300

// Number is a JavaScript number as stored: NaN and infinities are written as null.
type Number float64

func (n Number) MarshalJSON() ([]byte, error) {
	f := float64(n)
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return []byte("null"), nil
	}
	return []byte(jscompat.Number(f)), nil
}

// Entry is one turn, fields in the order they are written.
type Entry struct {
	ID             string          `json:"id"`
	WorkspaceID    string          `json:"workspaceId"`
	WorkspaceName  string          `json:"workspaceName"`
	WorkspaceDir   *string         `json:"workspaceDir"`
	TabID          string          `json:"tabId"`
	ProviderID     string          `json:"providerId"`
	AgentSessionID *string         `json:"agentSessionId"`
	Prompt         *string         `json:"prompt"`
	Result         *string         `json:"result"`
	StartedAt      Number          `json:"startedAt"`
	CompletedAt    Number          `json:"completedAt"`
	Duration       Number          `json:"duration"`
	DismissedAt    *Number         `json:"dismissedAt"`
	ToolUsage      json.RawMessage `json:"toolUsage"`
	TouchedFiles   json.RawMessage `json:"touchedFiles"`
	Cancelled      bool            `json:"cancelled,omitempty"`
}

// Clip is the entry as it goes over the wire: prompt and result cut to 300 units.
func (e Entry) Clip() Entry {
	clip := func(s *string) *string {
		if s == nil || jscompat.Len(*s) <= wireTextLimit {
			return s
		}
		c := jscompat.Slice(*s, wireTextLimit)
		return &c
	}
	e.Prompt, e.Result = clip(e.Prompt), clip(e.Result)
	return e
}

// toNumber is Number(v) for what a JSON file can hold.
func toNumber(v any) Number {
	switch x := v.(type) {
	case nil:
		return 0
	case float64:
		return Number(x)
	case bool:
		if x {
			return 1
		}
		return 0
	case string:
		s := jscompat.Trim(x)
		if s == "" {
			return 0
		}
		if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
			if n, err := strconv.ParseUint(s[2:], 16, 64); err == nil {
				return Number(n)
			}
			return Number(math.NaN())
		}
		if s == "Infinity" || s == "+Infinity" || s == "-Infinity" {
			return Number(math.Inf(1))
		}
		if f, err := strconv.ParseFloat(s, 64); err == nil && !strings.ContainsAny(s, "_xXpPnN") {
			return Number(f)
		}
	}
	return Number(math.NaN())
}

func stringOrNull(v any) *string {
	if s, ok := v.(string); ok {
		return &s
	}
	return nil
}

// migrate reads an entry of any earlier shape (claudeSessionId before agentSessionId); nil when it has no id or tab.
func migrate(raw json.RawMessage) *Entry {
	var e map[string]json.RawMessage
	if json.Unmarshal(raw, &e) != nil || e == nil {
		return nil
	}
	val := func(k string) any {
		var v any
		if r, has := e[k]; has {
			_ = json.Unmarshal(r, &v)
		}
		return v
	}
	id, okID := val("id").(string)
	tab, okTab := val("tabId").(string)
	if !okID || !okTab {
		return nil
	}
	out := &Entry{ID: id, TabID: tab, ProviderID: "claude"}
	if val("providerId") == "codex" {
		out.ProviderID = "codex"
	}
	if r, has := e["agentSessionId"]; has && (bytes.Equal(bytes.TrimSpace(r), []byte("null")) || stringOrNull(val("agentSessionId")) != nil) {
		out.AgentSessionID = stringOrNull(val("agentSessionId"))
	} else {
		out.AgentSessionID = stringOrNull(val("claudeSessionId"))
	}
	str := func(k string) string {
		v := val(k)
		if v == nil {
			return ""
		}
		return jscompat.String(v)
	}
	out.WorkspaceID, out.WorkspaceName = str("workspaceId"), str("workspaceName")
	out.WorkspaceDir, out.Prompt, out.Result = stringOrNull(val("workspaceDir")), stringOrNull(val("prompt")), stringOrNull(val("result"))
	out.StartedAt, out.CompletedAt, out.Duration = toNumber(val("startedAt")), toNumber(val("completedAt")), toNumber(val("duration"))
	if f, ok := val("dismissedAt").(float64); ok {
		n := Number(f)
		out.DismissedAt = &n
	}
	out.ToolUsage = json.RawMessage("{}")
	if r, has := e["toolUsage"]; has && !bytes.Equal(bytes.TrimSpace(r), []byte("null")) {
		out.ToolUsage = r
	}
	out.TouchedFiles = json.RawMessage("[]")
	if _, isList := val("touchedFiles").([]any); isList {
		out.TouchedFiles = e["touchedFiles"]
	}
	out.Cancelled = jscompat.Truthy(val("cancelled"))
	return out
}

// Store is session-history.json: every read-modify-write under one lock, and a write whose entries equal the last
// ones written is skipped.
type Store struct {
	File string
	mu   sync.Mutex
	last string
}

type document struct {
	Version int     `json:"version"`
	Entries []Entry `json:"entries"`
}

func (s *Store) read() []Entry {
	raw, err := os.ReadFile(s.File)
	if err != nil {
		return []Entry{}
	}
	var parsed struct {
		Entries []json.RawMessage `json:"entries"`
	}
	if json.Unmarshal(raw, &parsed) != nil {
		// A broken file is kept aside and replaced by the next write.
		if bak := strings.TrimSuffix(s.File, ".json"); bak != s.File {
			_ = os.WriteFile(bak+".json.bak", raw, 0o644)
		}
		return []Entry{}
	}
	entries := []Entry{}
	for _, r := range parsed.Entries {
		if e := migrate(r); e != nil {
			entries = append(entries, *e)
		}
	}
	return entries
}

func (s *Store) write(entries []Entry) error {
	key := string(nodejson.Marshal(entries))
	if key == s.last {
		return nil
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, nodejson.Marshal(document{Version: 1, Entries: entries}), "", "  "); err != nil {
		return err
	}
	tmp := s.File + "." + strconv.Itoa(os.Getpid()) + ".tmp"
	if err := os.WriteFile(tmp, pretty.Bytes(), 0o600); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, s.File); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	s.last = key
	return nil
}

// Add puts a finished turn first, keeping the newest 200.
func (s *Store) Add(e Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries := append([]Entry{e}, s.read()...)
	if len(entries) > MaxEntries {
		entries = entries[:MaxEntries]
	}
	return s.write(entries)
}

// Dismiss marks the newest undismissed turn of the tab; nil when there is none.
func (s *Store) Dismiss(tabID string, at float64) (*Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries := s.read()
	for i := range entries {
		if entries[i].TabID == tabID && entries[i].DismissedAt == nil {
			n := Number(at)
			entries[i].DismissedAt = &n
			e := entries[i]
			return &e, s.write(entries)
		}
	}
	return nil, nil
}

// All is the list, newest first.
func (s *Store) All() []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.read()
}
