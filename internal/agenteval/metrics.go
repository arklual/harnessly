package agenteval

import (
	"encoding/json"
	"regexp"
	"sort"
)

// Metrics are what a run cost and how its agents worked together, read from its facts: the numbers a comparison of
// ways to work (subagents, delegated chats, teammates) is made of besides the pass.
type Metrics struct {
	// Tokens are all tokens of every session: input, cache read and written, output.
	Tokens int `json:"tokens"`
	// Sessions are the agent sessions besides the person's chat.
	Sessions int `json:"sessions"`
	// Subagents, Delegations and Teammates are the calls that started each kind of helper.
	Subagents   int `json:"subagents"`
	Delegations int `json:"delegations"`
	Teammates   int `json:"teammates"`
	// LeadEdits are the file edits of the person's chat itself.
	LeadEdits int `json:"leadEdits"`
	// Conflicts are files edited from more than one session; Duplicates are the same edit made in more than one.
	Conflicts  int `json:"conflicts"`
	Duplicates int `json:"duplicates"`
	// Asks are the times the person was needed: a question tool, a turn that ended waiting for input.
	Asks int `json:"asks"`
	// Messages are what agents told each other mid-work (SendMessage, send_to_conversation).
	Messages int `json:"messages"`
	// Repl are the calls of Code Mode's REPL tool.
	Repl int `json:"repl"`
}

var (
	editTools    = map[string]bool{"Write": true, "Edit": true, "MultiEdit": true, "NotebookEdit": true}
	subagentTool = regexp.MustCompile(`^(Agent|Task)$`)
	delegateCall = regexp.MustCompile(`(^|\.)delegate_task$`)
	sendCall     = regexp.MustCompile(`(^|\.)send_to_conversation$`)
	tabCreate    = regexp.MustCompile(`harnessly tab create\b.*--prompt`)
)

// MetricsOf reads the metrics of a run.
func MetricsOf(r Result) Metrics {
	var m Metrics
	if r.Facts == nil {
		return m
	}
	f := r.Facts
	m.Tokens = f.TokensIn + f.CacheRead + f.CacheWrite + f.TokensOut
	m.Sessions = f.Sessions
	if r.TurnState == "needs-input" {
		m.Asks++
	}
	byFile := map[string]map[int]bool{}
	byEdit := map[string]map[int]bool{}
	for _, c := range f.Calls {
		var a map[string]any
		_ = json.Unmarshal(c.Args, &a)
		switch {
		case subagentTool.MatchString(c.Name):
			// A teammate of an agent team is an Agent call with a name (and a team).
			team, _ := a["team_name"].(string)
			if mate, _ := a["name"].(string); team != "" || mate != "" {
				m.Teammates++
			} else {
				m.Subagents++
			}
		case delegateCall.MatchString(c.Name):
			m.Delegations++
		case c.Name == "Bash":
			if cmd, _ := a["command"].(string); tabCreate.MatchString(cmd) {
				m.Delegations++
			}
		case c.Name == "SendMessage" || sendCall.MatchString(c.Name):
			m.Messages++
		case c.Name == "AskUserQuestion":
			m.Asks++
		case c.Name == "REPL":
			m.Repl++
		}
		if !editTools[c.Name] || c.Error {
			continue
		}
		file, _ := a["file_path"].(string)
		if file == "" {
			file, _ = a["notebook_path"].(string)
		}
		if file == "" {
			continue
		}
		if c.Session == 0 {
			m.LeadEdits++
		}
		if byFile[file] == nil {
			byFile[file] = map[int]bool{}
		}
		byFile[file][c.Session] = true
		key := c.Name + "\x00" + string(c.Args)
		if byEdit[key] == nil {
			byEdit[key] = map[int]bool{}
		}
		byEdit[key][c.Session] = true
	}
	for _, s := range byFile {
		if len(s) > 1 {
			m.Conflicts++
		}
	}
	for _, s := range byEdit {
		if len(s) > 1 {
			m.Duplicates++
		}
	}
	return m
}

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	t := 0.0
	for _, x := range xs {
		t += x
	}
	return t / float64(len(xs))
}
