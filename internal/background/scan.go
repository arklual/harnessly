// Package background tells which background work an agent conversation still has running — shells, monitors, async
// agents and pending wakeups started from its transcript — reconciled with the agent's own footer, and keeps that list
// current for every live tab.
package background

import (
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/ly-solutions/harnessly/internal/jscompat"
)

// Kinds of background work.
const (
	Shell   = "shell"
	Monitor = "monitor"
	Agent   = "agent"
	Wakeup  = "wakeup"
)

// Task is one piece of background work. Times are epoch milliseconds.
type Task struct {
	ID        string  `json:"id"`
	Kind      string  `json:"kind"`
	Label     string  `json:"label"`
	StartedAt Ms      `json:"startedAt"`
	Until     *Ms     `json:"until,omitempty"`      // wakeups and expiring monitors are pending only until then
	Output    *string `json:"outputPath,omitempty"` // gone means the task is over
}

// Ms is a time in epoch milliseconds; an unparsable time is NaN and written as null.
type Ms float64

func (m Ms) MarshalJSON() ([]byte, error) {
	if math.IsNaN(float64(m)) || math.IsInf(float64(m), 0) {
		return []byte("null"), nil
	}
	return []byte(jscompat.Number(float64(m))), nil
}

func pending(t Task, now float64) bool {
	return t.Until == nil || !jscompat.Truthy(float64(*t.Until)) || float64(*t.Until) > now
}

var (
	doneStatuses = map[string]bool{"completed": true, "failed": true, "killed": true, "stopped": true, "cancelled": true, "error": true, "timeout": true}
	notification = regexp.MustCompile(`(?s)<task-id>([\w-]+)</task-id>.*?<status>(\w+)</status>`)
	outputFile   = regexp.MustCompile(`written to: (/[^` + jscompat.SpaceClass[1:len(jscompat.SpaceClass)-1] + `]+?\.output)`)
	shellID      = regexp.MustCompile(`running in background with ID: ([\w-]+)`)
	agentID      = regexp.MustCompile(`(?s)Async agent launched.*?agentId: ([\w-]+)`)
	wakeupIn     = regexp.MustCompile(`(?i)wakeup scheduled for [^(]*\(in (\d+)s\)`)
	monitorTask  = regexp.MustCompile(`\(task ([\w-]+)`)
	monitorToken = regexp.MustCompile(`\b(b[a-z0-9]{7,})\b`)
	expiresIn    = regexp.MustCompile(`expires in (\d+)(s|m|h)\b`)
	spaces       = regexp.MustCompile(jscompat.SpaceClass + `+`)
	units        = map[string]float64{"s": 1_000, "m": 60_000, "h": 3_600_000}
)

type toolUse struct {
	name  string
	input map[string]any
	at    float64
}

// openTasks is a JavaScript Map: setting a known id keeps its place, a deleted id comes back at the end.
type openTasks struct {
	order []string
	tasks map[string]Task
}

func (o *openTasks) set(t Task) {
	if _, has := o.tasks[t.ID]; !has {
		o.order = append(o.order, t.ID)
	}
	o.tasks[t.ID] = t
}

func (o *openTasks) del(id string) {
	if _, has := o.tasks[id]; !has {
		return
	}
	delete(o.tasks, id)
	for i, x := range o.order {
		if x == id {
			o.order = append(o.order[:i], o.order[i+1:]...)
			break
		}
	}
}

// scan is what a transcript has said so far about its background work, fed line by line as it grows.
type scan struct {
	uses map[string]toolUse
	open openTasks
	done map[string]bool
}

func newScan() *scan {
	return &scan{uses: map[string]toolUse{}, open: openTasks{tasks: map[string]Task{}}, done: map[string]bool{}}
}

// pickInput keeps only what labels need: a Write or Edit input can hold a whole file, and a scan lives with its tab.
func pickInput(input map[string]any) map[string]any {
	picked := map[string]any{}
	for _, k := range []string{"description", "command", "reason", "prompt", "run_in_background"} {
		if v, has := input[k]; has {
			if s, ok := v.(string); ok {
				v = jscompat.Slice(s, 200)
			}
			picked[k] = v
		}
	}
	return picked
}

func textOf(content any) string {
	switch c := content.(type) {
	case string:
		return c
	case []any:
		parts := make([]string, len(c))
		for i, item := range c {
			if m, ok := item.(map[string]any); ok {
				if t, has := m["text"]; has {
					parts[i] = jsString(t)
				}
			}
		}
		return strings.Join(parts, " ")
	}
	return ""
}

func jsString(v any) string {
	if v == nil {
		return "null"
	}
	return jscompat.String(v)
}

func clip(v any) string {
	s := ""
	if v != nil {
		s = jscompat.String(v)
	}
	return jscompat.Slice(jscompat.Trim(spaces.ReplaceAllString(s, " ")), 90)
}

// or is JavaScript's `a || b`.
func or(a, b any) any {
	if jscompat.Truthy(a) {
		return a
	}
	return b
}

func (s *scan) finish(id string) {
	s.done[id] = true
	s.open.del(id)
}

func (s *scan) lines(lines []string, now float64) {
	for _, line := range lines {
		for _, m := range notification.FindAllStringSubmatch(line, -1) {
			if doneStatuses[m[2]] {
				s.finish(m[1])
			}
		}
		var entry struct {
			IsSidechain any `json:"isSidechain"`
			Timestamp   any `json:"timestamp"`
			Message     struct {
				Content any `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal([]byte(line), &entry) != nil || jscompat.Truthy(entry.IsSidechain) {
			continue
		}
		blocks, ok := entry.Message.Content.([]any)
		if !ok {
			continue
		}
		at := now
		if jscompat.Truthy(entry.Timestamp) {
			at = jscompat.DateValue(entry.Timestamp)
		}
		for _, raw := range blocks {
			block, _ := raw.(map[string]any)
			typ, _ := block["type"].(string)
			id, _ := block["id"].(string)
			name, _ := block["name"].(string)
			input, _ := block["input"].(map[string]any)
			if typ == "tool_use" && id != "" && name != "" {
				s.uses[id] = toolUse{name: name, input: pickInput(input), at: at}
				stopped := input["task_id"]
				if stopped == nil {
					stopped = input["shell_id"]
				}
				if stopped == nil {
					stopped = input["bash_id"]
				}
				if sid, ok := stopped.(string); ok && (name == "TaskStop" || name == "KillShell" || name == "KillBash") {
					s.finish(sid)
				}
				continue
			}
			useID, _ := block["tool_use_id"].(string)
			if typ != "tool_result" || useID == "" {
				continue
			}
			use, ok := s.uses[useID]
			if !ok {
				continue
			}
			delete(s.uses, useID)
			s.result(use, useID, textOf(block["content"]))
		}
	}
}

func (s *scan) result(use toolUse, useID, text string) {
	var output *string
	if m := outputFile.FindStringSubmatch(text); m != nil {
		output = &m[1]
	}
	started := Ms(use.at)
	switch {
	case use.name == "Bash" && use.input["run_in_background"] == true && shellID.MatchString(text):
		id := shellID.FindStringSubmatch(text)[1]
		s.open.set(Task{ID: id, Kind: Shell, Label: clip(or(use.input["description"], use.input["command"])), StartedAt: started, Output: output})
	case (use.name == "Agent" || use.name == "Task") && agentID.MatchString(text):
		id := agentID.FindStringSubmatch(text)[1]
		s.open.set(Task{ID: id, Kind: Agent, Label: clip(use.input["description"]), StartedAt: started})
	case use.name == "Monitor":
		// "Monitor started (task bktwfrc6l, expires in 3m unless the source ends first …)"
		var id string
		if m := monitorTask.FindStringSubmatch(text); m != nil {
			id = m[1]
		} else if m := monitorToken.FindStringSubmatch(text); m != nil {
			id = m[1]
		}
		if id == "" {
			return
		}
		t := Task{ID: id, Kind: Monitor, Label: clip(or(use.input["description"], use.input["command"])), StartedAt: started, Output: output}
		if m := expiresIn.FindStringSubmatch(text); m != nil {
			n, _ := strconv.ParseFloat(m[1], 64)
			if until := Ms(use.at + n*units[m[2]]); jscompat.Truthy(float64(until)) {
				t.Until = &until
			}
		}
		s.open.set(t)
	case use.name == "ScheduleWakeup" && wakeupIn.MatchString(text):
		n, _ := strconv.ParseFloat(wakeupIn.FindStringSubmatch(text)[1], 64)
		until := Ms(use.at + n*1000)
		s.open.set(Task{ID: "wakeup-" + useID, Kind: Wakeup, Label: clip(or(use.input["reason"], use.input["prompt"])), StartedAt: started, Until: &until})
	}
}

// tasks is what is still running: expired ones dropped, finished ones left out, only the newest wakeup.
func (s *scan) tasks(now float64) []Task {
	for _, id := range append([]string(nil), s.open.order...) {
		if t := s.open.tasks[id]; t.Until != nil && jscompat.Truthy(float64(*t.Until)) && float64(*t.Until) <= now {
			s.open.del(id)
		}
	}
	out := []Task{}
	var wakeup *Task
	for _, id := range s.open.order {
		t := s.open.tasks[id]
		if t.Kind == Wakeup {
			wakeup = &t
		} else if !s.done[t.ID] {
			out = append(out, t)
		}
	}
	if wakeup != nil {
		out = append(out, *wakeup)
	}
	return out
}

// ParseTasks is the background work a whole transcript leaves running; now is epoch milliseconds.
func ParseTasks(lines []string, now float64) []Task {
	s := newScan()
	s.lines(lines, now)
	return s.tasks(now)
}
