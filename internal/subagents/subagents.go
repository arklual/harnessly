// Package subagents lists the subagents of a Claude Code conversation: the transcripts Claude Code writes next to the
// conversation's own (<session>/subagents/agent-<id>.jsonl, with an agent-<id>.meta.json), each summed up the way the
// previous runtime's inspector showed it.
package subagents

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"harnessly/internal/agents"
	"harnessly/internal/agents/claude"
	"harnessly/internal/jscompat"
)

const (
	// runningWindowMs: a subagent that wrote within this time still counts as running.
	runningWindowMs = 45_000
	maxText         = 6_000
)

// FileName matches a subagent transcript's file name.
var FileName = regexp.MustCompile(`^agent-[\w-]+\.jsonl$`)

// DirFor is the folder of a conversation's subagent transcripts.
func DirFor(jsonlPath string) string {
	return filepath.Join(strings.TrimSuffix(jsonlPath, ".jsonl"), "subagents")
}

// Files are the subagent transcripts of a conversation, by name; none when the folder cannot be read.
func Files(jsonlPath string) []string {
	dir := DirFor(jsonlPath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if FileName.MatchString(e.Name()) {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out
}

// Summary is one subagent as the inspector lists it. The fields taken from the meta file keep the value found there.
type Summary struct {
	ID          string  `json:"id"`
	AgentType   any     `json:"agentType"`
	Description any     `json:"description"`
	ToolUseID   any     `json:"toolUseId"`
	Background  bool    `json:"background"`
	Status      string  `json:"status"`
	StartedAt   any     `json:"startedAt"`
	UpdatedAt   float64 `json:"updatedAt"`
	ToolUses    int     `json:"toolUses"`
	LastAction  any     `json:"lastAction"`
	Prompt      string  `json:"prompt"`
	Result      any     `json:"result"`
}

// textOf is the text of a message's content: a string as it is, the text parts of a list joined by lines.
func textOf(content any) string {
	if s, ok := content.(string); ok {
		return s
	}
	list, ok := content.([]any)
	if !ok {
		return ""
	}
	var parts []string
	for _, c := range list {
		o := agents.AsObj(c)
		if o == nil || o.Val("type") != "text" {
			continue
		}
		if t := o.Val("text"); t != nil {
			parts = append(parts, agents.JSString(t))
		} else {
			parts = append(parts, "")
		}
	}
	return strings.Join(parts, "\n")
}

func typeOf(v any) any {
	if o := agents.AsObj(v); o != nil {
		return o.Val("type")
	}
	return nil
}

// Summarize sums up one subagent transcript; ok is false where the previous runtime failed on it (a line or a meta
// file holding null), which left the subagent out.
func Summarize(raw string, meta any, id string, mtimeMs float64, now float64) (s Summary, ok bool) {
	if meta == nil {
		return Summary{}, false
	}
	var startedAt any
	toolUses := 0
	var lastAction any
	prompt := ""
	var result any
	pendingTool, finished := false, false
	for _, line := range strings.Split(raw, "\n") {
		if jscompat.Trim(line) == "" {
			continue
		}
		row, err := agents.Decode([]byte(line))
		if err != nil {
			continue
		}
		if row == nil {
			return Summary{}, false
		}
		r := agents.AsObj(row)
		if startedAt == nil {
			if t := r.Val("timestamp"); agents.Truthy(t) {
				if ms := jscompat.ParseDate(agents.JSString(t)); ms == ms {
					startedAt = ms
				}
			}
		}
		message := agents.AsObj(r.Val("message"))
		var content any
		if message != nil {
			content = message.Val("content")
		}
		switch r.Val("type") {
		case "user":
			if prompt == "" {
				prompt = jscompat.Slice(textOf(content), maxText)
			}
			if list, ok := content.([]any); ok {
				for _, c := range list {
					if typeOf(c) == "tool_result" {
						pendingTool = false
						break
					}
				}
			}
		case "assistant":
			list, ok := content.([]any)
			if !ok {
				continue
			}
			var uses []*agents.Obj
			for _, c := range list {
				if typeOf(c) == "tool_use" {
					uses = append(uses, agents.AsObj(c))
				}
			}
			if len(uses) > 0 {
				toolUses += len(uses)
				last := uses[len(uses)-1]
				name := "undefined"
				if v, has := last.Get("name"); has {
					name = agents.JSString(v)
				}
				input := last.Val("input")
				if input == nil {
					input = agents.NewObj()
				}
				lastAction = claude.SummarizeToolCall(name, input)
				pendingTool, finished = true, false
			}
			if text := jscompat.Trim(textOf(content)); text != "" {
				result = jscompat.Slice(text, maxText)
			}
			if message != nil && message.Val("stop_reason") == "end_turn" && len(uses) == 0 {
				finished = true
			}
		}
	}
	status := "stalled"
	switch {
	case finished && !pendingTool:
		status = "done"
	case now-mtimeMs < runningWindowMs:
		status = "running"
	}
	m := agents.AsObj(meta)
	field := func(k string) any {
		if m == nil {
			return nil
		}
		return m.Val(k)
	}
	agentType := field("agentType")
	if !agents.Truthy(agentType) {
		agentType = "general-purpose"
	}
	description := field("description")
	if !agents.Truthy(description) {
		first, _, _ := strings.Cut(prompt, "\n")
		if first = jscompat.Slice(first, 80); first != "" {
			description = first
		} else {
			description = "Субагент"
		}
	}
	if status != "done" {
		result = nil
	}
	return Summary{
		ID:          id,
		AgentType:   agentType,
		Description: description,
		ToolUseID:   field("toolUseId"),
		Background:  field("requestShape") == "background",
		Status:      status,
		StartedAt:   startedAt,
		UpdatedAt:   mtimeMs,
		ToolUses:    toolUses,
		LastAction:  lastAction,
		Prompt:      prompt,
		Result:      result,
	}, true
}

type cached struct {
	mtimeMs float64
	size    int64
	summary Summary
}

// Lister lists subagents, keeping each settled summary until its transcript changes.
type Lister struct {
	// Now is the clock (tests freeze it); nil is the real one.
	Now func() time.Time

	mu    sync.Mutex
	cache map[string]cached
}

func (l *Lister) now() float64 {
	if l.Now != nil {
		return float64(l.Now().UnixMilli())
	}
	return float64(time.Now().UnixMilli())
}

// List is every subagent of a conversation, the most recently started first.
func (l *Lister) List(jsonlPath string) []Summary {
	dir := DirFor(jsonlPath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []Summary{}
	}
	out := []Summary{}
	for _, e := range entries {
		if !FileName.MatchString(e.Name()) {
			continue
		}
		if s, ok := l.one(dir, strings.TrimSuffix(e.Name(), ".jsonl")); ok {
			out = append(out, s)
		}
	}
	key := func(s Summary) float64 {
		if t, ok := s.StartedAt.(float64); ok {
			return t
		}
		return s.UpdatedAt
	}
	sort.SliceStable(out, func(i, j int) bool { return key(out[i]) > key(out[j]) })
	return out
}

func (l *Lister) one(dir, id string) (Summary, bool) {
	file := filepath.Join(dir, id+".jsonl")
	st, err := os.Stat(file)
	if err != nil {
		return Summary{}, false
	}
	mtime := jscompat.TimeMs(st.ModTime())
	now := l.now()
	l.mu.Lock()
	c, hit := l.cache[file]
	l.mu.Unlock()
	// A summary made while the agent still counted as running is not kept past that: the previous runtime kept it and
	// showed a stalled agent as running until its transcript changed.
	if hit && c.mtimeMs == mtime && c.size == st.Size() && now-mtime > runningWindowMs && c.summary.Status != "running" {
		return c.summary, true
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return Summary{}, false
	}
	var meta any = agents.NewObj()
	if metaRaw, err := os.ReadFile(filepath.Join(dir, id+".meta.json")); err == nil {
		if v, err := agents.Decode([]byte(jscompat.DecodeUTF8(metaRaw))); err == nil {
			meta = v
		}
	}
	s, ok := Summarize(jscompat.DecodeUTF8(raw), meta, strings.TrimPrefix(id, "agent-"), mtime, now)
	if !ok {
		return Summary{}, false
	}
	l.mu.Lock()
	if l.cache == nil {
		l.cache = map[string]cached{}
	}
	l.cache[file] = cached{mtimeMs: mtime, size: st.Size(), summary: s}
	l.mu.Unlock()
	return s, true
}
