package codex

import (
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/jscompat"
)

const (
	tailSize = 131_072
	// StaleInterrupted is the quiet window after an agent message, StaleAwaitingAPI after a prompt.
	StaleInterrupted = 20_000
	StaleAwaitingAPI = 90_000
	snippetMax       = 200
	snapshotCacheMax = 256
)

var spaces = regexp.MustCompile(jscompat.SpaceClass + `+`)

// compact is text on one line, cut to limit units with an ellipsis.
func compact(text string, limit int) string {
	return jscompat.Truncate(jscompat.Trim(spaces.ReplaceAllString(text, " ")), limit)
}

func isShell(name string) bool { return name == "exec_command" || name == "shell" || name == "bash" }

func summarizeFunctionCall(name string, args any) string {
	if isShell(name) {
		obj := agents.AsObj(args)
		cmd := obj.Val("cmd")
		if cmd == nil {
			cmd = obj.Val("command")
		}
		if line := strings.SplitN(agents.AsStr(cmd), "\n", 2)[0]; line != "" {
			return "$ " + line
		}
		return "$ "
	}
	switch name {
	case "write_stdin":
		return "Write stdin"
	case "apply_patch":
		return "Apply patch"
	}
	var keys []string
	switch x := args.(type) {
	case *agents.Obj:
		keys = x.Keys()
	case []any:
		for i := range x {
			keys = append(keys, itoa(i))
		}
	default:
		return name
	}
	if len(keys) == 0 || keys[0] == "" {
		return name
	}
	var v any
	switch x := args.(type) {
	case *agents.Obj:
		v = x.Val(keys[0])
	case []any:
		v = x[0]
	}
	preview, ok := v.(string)
	if !ok {
		preview = agents.Stringify(v)
	}
	return name + " " + compact(preview, 60)
}

func itoa(i int) string { return strconv.Itoa(i) }

func functionCallAction(name string, raw any) *agents.CurrentAction {
	args := raw
	if s, ok := raw.(string); ok {
		if v, err := agents.Decode([]byte(s)); err == nil && v != nil {
			args = v
		}
	}
	tool := name
	if isShell(name) {
		tool = "Bash"
	} else if name == "apply_patch" {
		tool = "Edit"
	}
	return agents.Tool(tool, summarizeFunctionCall(name, args))
}

func commandAction(payload *agents.Obj) *agents.CurrentAction {
	command := ""
	switch c := payload.Val("command").(type) {
	case string:
		command = c
	case []any:
		var parts []string
		for _, p := range c {
			if s, ok := p.(string); ok {
				parts = append(parts, s)
			}
		}
		command = strings.Join(parts, " ")
	}
	if command == "" {
		return agents.Tool("Bash", "$ ")
	}
	return agents.Tool("Bash", "$ "+strings.SplitN(command, "\n", 2)[0])
}

func isCompletion(t string) bool {
	return t == "task_complete" || t == "TurnComplete" || t == "shutdown_complete" || t == "ShutdownComplete"
}

func isInterrupt(t string) bool { return t == "turn_aborted" || t == "TurnAborted" }

// ScanResult is a snapshot and whether its idle flag must be re-read from the file's age.
type ScanResult struct {
	agents.Snapshot
	NeedsStaleRecheck bool
}

// ScanLines reads a rollout's tail from the newest line back; elapsed is the file's age in milliseconds.
//
// Every line of the tail is read: a completion anywhere in it (an earlier turn's too) makes the snapshot idle, and
// the quiet window is the one of the oldest prompt or message after the newest completion. This is how the previous
// runtime read it, kept so both answer alike.
func ScanLines(entries []*agents.Obj, elapsed float64) ScanResult {
	var (
		action          *agents.CurrentAction
		snippet         *string
		reset           bool
		lastEntry       *float64
		interrupted     bool
		terminalIdle    bool
		needsRecheck    bool
		staleMs         int64
		completedCalls  = map[string]bool{}
		noActionNoReset = func() bool { return !reset && !terminalIdle && action == nil }
	)
	for i := len(entries) - 1; i >= 0; i-- {
		item := entries[i]
		if item == nil {
			continue
		}
		if ts := item.Val("timestamp"); agents.Truthy(ts) && lastEntry == nil {
			if ms := jscompat.ParseDate(agents.JSString(ts)); !math.IsNaN(ms) && !math.IsInf(ms, 0) {
				lastEntry = &ms
			}
		}
		payload := agents.AsObj(item.Val("payload"))
		switch item.Val("type") {
		case "event_msg":
			t := agents.AsStr(payload.Val("type"))
			switch {
			case isCompletion(t):
				terminalIdle, needsRecheck, staleMs = true, false, 0
			case isInterrupt(t):
				terminalIdle, interrupted, needsRecheck, staleMs = true, true, false, 0
			case t == "user_message":
				if snippet == nil && action == nil {
					reset = true
				}
				if !terminalIdle {
					needsRecheck, staleMs = elapsed <= StaleAwaitingAPI, StaleAwaitingAPI
				}
			case t == "agent_message":
				msg := agents.AsStr(payload.Val("message"))
				if msg != "" && snippet == nil {
					s := compact(msg, snippetMax)
					snippet = &s
				}
				if noActionNoReset() && msg != "" {
					action = agents.Text(compact(msg, snippetMax))
				}
				if !terminalIdle {
					needsRecheck, staleMs = elapsed <= StaleInterrupted, StaleInterrupted
				}
			case (t == "exec_command_end" || t == "ExecCommandEnd") && agents.Truthy(payload.Val("call_id")):
				completedCalls[agents.JSString(payload.Val("call_id"))] = true
			case (t == "exec_command_begin" || t == "ExecCommandBegin") && agents.Truthy(payload.Val("call_id")):
				if noActionNoReset() && !completedCalls[agents.JSString(payload.Val("call_id"))] {
					action = commandAction(payload)
					needsRecheck, staleMs = false, 0
				}
			}
		case "response_item":
			rt := agents.AsStr(payload.Val("type"))
			callID := agents.AsStr(payload.Val("call_id"))
			switch {
			case (rt == "function_call_output" || rt == "custom_tool_call_output") && callID != "":
				completedCalls[callID] = true
			case rt == "function_call" || rt == "custom_tool_call":
				name := agents.AsStr(payload.Val("name"))
				if callID != "" && completedCalls[callID] {
					continue
				}
				if noActionNoReset() && name != "" {
					args := payload.Val("arguments")
					if args == nil {
						args = payload.Val("input")
					}
					action = functionCallAction(name, args)
				}
			case rt == "web_search_call":
				status := agents.AsStr(payload.Val("status"))
				if noActionNoReset() && status != "completed" && status != "failed" {
					summary := "WebSearch"
					if q := agents.AsStr(payload.Val("query")); q != "" {
						summary = `WebSearch "` + q + `"`
					}
					action = agents.Tool("WebSearch", summary)
				}
			}
		}
	}
	return ScanResult{
		Snapshot: agents.Snapshot{
			Idle:                 terminalIdle || (needsRecheck && elapsed > float64(staleMs)),
			Stale:                !terminalIdle && needsRecheck,
			LastAssistantSnippet: snippet,
			CurrentAction:        action,
			Reset:                reset,
			LastEntryTs:          lastEntry,
			StaleMs:              staleMs,
			Interrupted:          interrupted,
		},
		NeedsStaleRecheck: needsRecheck,
	}
}

type cached struct {
	mtime time.Time
	scan  ScanResult
}

// SnapshotReader reads rollout snapshots, keeping the last 256 by path until the file changes.
type SnapshotReader struct {
	Now   func() time.Time
	cache agents.LRU[cached]
}

// NewSnapshotReader reads with the wall clock.
func NewSnapshotReader() *SnapshotReader {
	return &SnapshotReader{Now: time.Now, cache: agents.LRU[cached]{Max: snapshotCacheMax}}
}

// TailEntries decodes the complete lines of a rollout's last 128 KiB.
func TailEntries(path string, size int64) ([]*agents.Obj, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := agents.ReadTail(f, size, tailSize)
	if err != nil {
		return nil, err
	}
	if size > int64(len(raw)) {
		// The first line is cut.
		if i := indexNewline(raw); i >= 0 {
			raw = raw[i+1:]
		} else {
			raw = nil
		}
	}
	lines := agents.Lines(raw)
	out := make([]*agents.Obj, len(lines))
	for i, l := range lines {
		out[i] = agents.DecodeObj(l)
	}
	return out, nil
}

func indexNewline(b []byte) int {
	for i, c := range b {
		if c == '\n' {
			return i
		}
	}
	return -1
}

// Read is the snapshot of the rollout at path; force skips the cache. An empty file is idle, an unreadable one says
// nothing.
func (r *SnapshotReader) Read(path string, force bool) agents.Snapshot {
	st, err := os.Stat(path)
	if err != nil {
		return agents.Snapshot{}
	}
	if st.Size() == 0 {
		return agents.Snapshot{Idle: true}
	}
	now := r.Now()
	if c, ok := r.cache.Get(path); ok && !force && c.mtime.Equal(st.ModTime()) {
		if !c.scan.NeedsStaleRecheck {
			return c.scan.Snapshot
		}
		s := c.scan.Snapshot
		s.Idle = agents.MillisSince(now, st.ModTime()) > float64(s.StaleMs)
		s.Stale = !s.Idle
		return s
	}
	entries, err := TailEntries(path, st.Size())
	if err != nil {
		return agents.Snapshot{}
	}
	scan := ScanLines(entries, agents.MillisSince(now, st.ModTime()))
	r.cache.Put(path, cached{mtime: st.ModTime(), scan: scan})
	return scan.Snapshot
}

// SummarizeFunctionCall is the one-line description of a tool call (its arguments as the rollout holds them: a JSON
// string or a value), as the current action and the timeline show it.
func SummarizeFunctionCall(name string, raw any) string { return functionCallAction(name, raw).Summary }
