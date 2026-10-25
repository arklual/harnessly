package convo

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/outbox"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// The first start of v2 takes over what Legacy had: its agent tabs become conversations, its prompt-queue.json their
// queued messages.

// ImportTab is a Legacy tab that runs an agent.
type ImportTab struct {
	ID      string
	Session string
	// Title is what Legacy shows for the tab (launch.Starter.Conversations): the agent's title of the transcript, its
	// summary, the last message, the tab's title, and the tab's name, the first that is there.
	Title string
	// Name is the tab's own name, "Claude Code" until the person renames it.
	Name           string
	Provider       wire.Provider
	Cwd            string
	TranscriptPath string
	AgentSessionID string
	CreatedAt      wire.Ms
	// LaunchUltracode: the tab was started with --effort ultracode. Claude's transcript logs it as xhigh.
	LaunchUltracode bool
}

// TabFromLayout reads a tab object of a Legacy workspace layout (state.TabFromLayout's fields); ok is false for a
// tab that runs no agent or has no usable id.
func TabFromLayout(tab *jsvalue.Object, cwd string) (ImportTab, bool) {
	str := func(o *jsvalue.Object, k string) string {
		if o == nil {
			return ""
		}
		v, _ := o.Get(k)
		s, _ := v.(string)
		return s
	}
	t := ImportTab{ID: str(tab, "id"), Session: str(tab, "sessionName"), Name: str(tab, "name"), Cwd: cwd}
	t.Title = firstOf(str(tab.Object("agentState"), "summary"), str(tab, "lastUserMessage"), str(tab, "title"), t.Name)
	switch str(tab, "panelType") {
	case "claude-code":
		t.Provider = wire.ProviderClaude
	case "codex-cli":
		t.Provider = wire.ProviderCodex
	default:
		return ImportTab{}, false
	}
	if !wire.ValidConvID(t.ID) {
		return ImportTab{}, false
	}
	st := tab.Object("agentState")
	if st != nil && str(st, "providerId") == string(t.Provider) {
		t.AgentSessionID, t.TranscriptPath = str(st, "sessionId"), str(st, "jsonlPath")
	}
	if t.Provider == wire.ProviderClaude {
		if t.AgentSessionID == "" {
			t.AgentSessionID = str(tab, "claudeSessionId")
		}
		if t.TranscriptPath == "" {
			t.TranscriptPath = str(tab, "claudeJsonlPath")
		}
		if t.AgentSessionID == "" && t.TranscriptPath != "" {
			t.AgentSessionID = strings.TrimSuffix(path.Base(t.TranscriptPath), ".jsonl")
		}
	}
	if v, ok := tab.Get("createdAt"); ok {
		if f, isNum := v.(float64); isNum {
			t.CreatedAt = wire.Ms(f)
		}
	}
	return t, true
}

func firstOf(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

// RetitleImported gives a conversation taken over from Legacy the title Legacy shows for its tab. A title the person
// typed in v2 stays; so does one an earlier import took from the tab's name before titles were read this way.
func (r *Runtime) RetitleImported(t ImportTab) {
	title := titleOf(t.Title)
	_ = r.call(context.Background(), t.ID, func(c *conv, now time.Time) {
		c.launchUltracode = t.LaunchUltracode // the tab's launch is Legacy's to remember, and may be read again
		if title == "" {
			return
		}
		fromName := c.rec.TitleSource == wire.TitleFromUser && c.rec.Title == t.Name
		if c.rec.Title == title || c.rec.TitleSource != wire.TitleFromAgent && !fromName {
			return
		}
		c.rec.Title, c.rec.TitleSource = title, wire.TitleFromAgent
		c.recResult(now)
	})
}

// LegacyQueue reads Legacy's prompt-queue.json: tab id → its messages, oldest first. An entry that does not have its
// fields' types is skipped, as Legacy's own loader does; a missing file is no queue.
func LegacyQueue(file string) (map[string][]outbox.LegacyItem, error) {
	raw, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string][]outbox.LegacyItem{}, nil
	}
	if err != nil {
		return nil, err
	}
	var byTab map[string]json.RawMessage
	if err := json.Unmarshal(raw, &byTab); err != nil {
		return nil, err
	}
	out := map[string][]outbox.LegacyItem{}
	for tab, list := range byTab {
		var entries []json.RawMessage
		if json.Unmarshal(list, &entries) != nil {
			continue
		}
		for _, e := range entries {
			if it, ok := legacyItem(e); ok {
				out[tab] = append(out[tab], it)
			}
		}
	}
	return out, nil
}

func legacyItem(raw json.RawMessage) (outbox.LegacyItem, bool) {
	var e struct {
		ID        *string          `json:"id"`
		Text      *string          `json:"text"`
		CreatedAt *float64         `json:"createdAt"`
		Images    *[]string        `json:"images"`
		Status    *json.RawMessage `json:"status"`
		RetriedAt *json.RawMessage `json:"retriedAt"`
	}
	if json.Unmarshal(raw, &e) != nil || e.ID == nil || e.Text == nil || e.CreatedAt == nil || e.Images == nil {
		return outbox.LegacyItem{}, false
	}
	it := outbox.LegacyItem{ID: *e.ID, Text: *e.Text, Images: *e.Images, CreatedAt: wire.Ms(*e.CreatedAt)}
	if e.Status != nil {
		var s string
		it.Failed = json.Unmarshal(*e.Status, &s) == nil && s == "failed"
	}
	if e.RetriedAt != nil {
		// A message the person asked for again lives from then.
		var at float64
		if json.Unmarshal(*e.RetriedAt, &at) == nil && wire.Ms(at) > it.CreatedAt {
			it.CreatedAt = wire.Ms(at)
		}
	}
	return it, true
}

// Import takes Legacy's tabs and their queues: each tab not known yet becomes a conversation, unknown until its
// probes say more, its queue in order. Importing twice changes nothing.
func (r *Runtime) Import(tabs []ImportTab, queues map[string][]outbox.LegacyItem) int {
	n := 0
	for _, t := range tabs {
		if !wire.ValidConvID(t.ID) || r.actor(t.ID) != nil || t.Session != "" && r.ConvOfSession(t.Session) != "" {
			continue
		}
		now := r.d.Clock.Now()
		created := t.CreatedAt
		if created == 0 {
			created = ms(now)
		}
		rec := wire.Conversation{ID: t.ID, MachineID: r.d.MachineID, Cwd: t.Cwd, Provider: t.Provider, Title: titleOf(t.Title),
			TitleSource: wire.TitleFromAgent, CreatedAt: created, LastActivityAt: created, LastPromptAt: created, SeenAt: ms(now),
			State: wire.AgentState{K: wire.StateUnknown, Since: ms(now)}, Rev: 1,
			Session: t.Session, TranscriptPath: t.TranscriptPath, AgentSessionID: t.AgentSessionID}
		c := newConv(&r.d, rec)
		c.dirty = true
		c.launchUltracode = t.LaunchUltracode
		r.add(c)
		items := queues[t.ID]
		// The conversation was added a line above, so only a stop of its actor fails this call; it is then gone.
		_ = r.call(context.Background(), t.ID, func(c *conv, now time.Time) {
			c.feedUpdate(c.fd.Load(), false, now)
			if t := c.fd.LastTurn(); t != nil && t.StartedAt > c.rec.LastActivityAt {
				c.rec.LastActivityAt = t.StartedAt
			}
			c.lastTurn()
			c.change(c.box.Import(items, now), now)
			c.recDirty = true
			c.flush(now, true)
			c.observe(now)
			c.save(now, true)
		})
		n++
	}
	return n
}
