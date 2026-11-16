package agenteval

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ly-solutions/harnessly/internal/apps"
)

// Facts are what a run left, read after the agent's turn. They are saved with the run, so a run is judged again
// without running it.
type Facts struct {
	Calls []Call `json:"calls"`
	// Said is everything the agent wrote to the person, in order; Final is its last message.
	Said  []string `json:"said"`
	Final string   `json:"final"`
	// Pane are the right pane's intents the chat left.
	Pane []PaneIntent `json:"pane"`
	// Images are the picture jobs of the chat.
	Images []ImageJob `json:"images"`
	// Files are the workspace's files ("design:" before the design library's), with the text of the small ones.
	Files []FileFact `json:"files"`
	// Model is what the agent ran on, as the transcript says.
	Model      string `json:"model,omitempty"`
	TokensIn   int    `json:"tokensIn"`
	CacheRead  int    `json:"cacheRead"`
	CacheWrite int    `json:"cacheWrite"`
	TokensOut  int    `json:"tokensOut"`
	// APIErrors are the provider's errors the transcript recorded (overload, auth): an infrastructure failure.
	APIErrors []string `json:"apiErrors,omitempty"`
	// Sessions are the agent sessions the run left besides the person's chat: subagents, chats it delegated to,
	// teammates. Their calls are in Calls (Session > 0), their tokens in the totals.
	Sessions int `json:"sessions,omitempty"`
	// LeadTokens are the tokens of the person's chat alone (input, cache, output).
	LeadTokens int `json:"leadTokens,omitempty"`
	// ByModel are the tokens of each model the sessions ran on.
	ByModel map[string]Usage `json:"byModel,omitempty"`
	// Checks are the scenario's check commands as they ran in the workspace after the run.
	Checks []CheckResult `json:"checks,omitempty"`
}

// Usage are one model's tokens.
type Usage struct {
	In         int `json:"in"`
	CacheRead  int `json:"cacheRead"`
	CacheWrite int `json:"cacheWrite"`
	Out        int `json:"out"`
}

// CheckResult is one check command's outcome.
type CheckResult struct {
	Command string `json:"command"`
	Exit    int    `json:"exit"`
	Output  string `json:"output,omitempty"`
}

// Call is one tool call of the agent or of its subagents.
type Call struct {
	// Session is which transcript the call is in: 0 the person's chat, then the other sessions in the order read.
	Session int    `json:"session,omitempty"`
	Name    string `json:"name"`
	// At is when the call was made, as the transcript stamps it.
	At     string          `json:"at,omitempty"`
	Args   json.RawMessage `json:"args,omitempty"`
	Error  bool            `json:"error,omitempty"`
	Result string          `json:"result,omitempty"`
}

// PaneIntent is one thing the chat asked the right pane to show.
type PaneIntent struct {
	Kind   string `json:"kind"`
	Tab    string `json:"tab,omitempty"`
	Target string `json:"target,omitempty"`
	// Place is where in the target: a design's board.
	Place string `json:"place,omitempty"`
}

// ImageJob is a picture job of the chat.
type ImageJob struct {
	Prompt string `json:"prompt"`
	State  string `json:"state"`
	Images int    `json:"images"`
	Error  string `json:"error,omitempty"`
}

// FileFact is a file the run left.
type FileFact struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
	Text string `json:"text,omitempty"`
}

const (
	resultCap   = 2000
	fileTextCap = 64 << 10
	filesCap    = 2000
)

// ReadClaudeTranscript reads a Claude Code session file (and its subagents' files): the calls with their results, what
// was said, tokens and the provider's errors. Kinds it does not know are skipped: the format grows.
func ReadClaudeTranscript(paths ...string) (Facts, error) {
	var f Facts
	idx := map[string]int{}
	seenMsg := map[string]bool{}
	for i, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			if i == 0 {
				return f, err
			}
			continue
		}
		if i > 0 {
			f.Sessions++
		}
		sc := bufio.NewScanner(bytes.NewReader(raw))
		sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
		for sc.Scan() {
			line := bytes.TrimSpace(sc.Bytes())
			if len(line) == 0 || line[0] != '{' {
				continue
			}
			var e struct {
				Type              string `json:"type"`
				Timestamp         string `json:"timestamp"`
				IsAPIErrorMessage bool   `json:"isApiErrorMessage"`
				Message           struct {
					ID      string          `json:"id"`
					Model   string          `json:"model"`
					Content json.RawMessage `json:"content"`
					Usage   struct {
						In     int `json:"input_tokens"`
						Out    int `json:"output_tokens"`
						Read   int `json:"cache_read_input_tokens"`
						Create int `json:"cache_creation_input_tokens"`
					} `json:"usage"`
				} `json:"message"`
			}
			if json.Unmarshal(line, &e) != nil || (e.Type != "assistant" && e.Type != "user") {
				continue
			}
			var blocks []struct {
				Type      string          `json:"type"`
				Text      string          `json:"text"`
				ID        string          `json:"id"`
				Name      string          `json:"name"`
				Input     json.RawMessage `json:"input"`
				ToolUseID string          `json:"tool_use_id"`
				IsError   bool            `json:"is_error"`
				Content   json.RawMessage `json:"content"`
			}
			if json.Unmarshal(e.Message.Content, &blocks) != nil {
				continue
			}
			if e.Type == "assistant" {
				if e.Message.Model != "" && e.Message.Model != "<synthetic>" && i == 0 {
					f.Model = e.Message.Model
				}
				if e.Message.ID != "" && !seenMsg[e.Message.ID] {
					seenMsg[e.Message.ID] = true
					u := e.Message.Usage
					f.TokensIn += u.In
					f.CacheRead += u.Read
					f.CacheWrite += u.Create
					f.TokensOut += u.Out
					if i == 0 {
						f.LeadTokens += u.In + u.Read + u.Create + u.Out
					}
					if m := e.Message.Model; m != "" && m != "<synthetic>" {
						if f.ByModel == nil {
							f.ByModel = map[string]Usage{}
						}
						bm := f.ByModel[m]
						bm.In, bm.CacheRead, bm.CacheWrite, bm.Out = bm.In+u.In, bm.CacheRead+u.Read, bm.CacheWrite+u.Create, bm.Out+u.Out
						f.ByModel[m] = bm
					}
				}
			}
			for _, b := range blocks {
				switch {
				case e.Type == "assistant" && b.Type == "text" && strings.TrimSpace(b.Text) != "":
					if e.IsAPIErrorMessage {
						f.APIErrors = append(f.APIErrors, clip(b.Text, 300))
						continue
					}
					if i == 0 {
						f.Said = append(f.Said, b.Text)
						f.Final = b.Text
					}
				case e.Type == "assistant" && b.Type == "tool_use":
					if _, dup := idx[b.ID]; dup && b.ID != "" {
						continue
					}
					idx[b.ID] = len(f.Calls)
					f.Calls = append(f.Calls, Call{Session: i, Name: apps.ToolName(b.Name), At: e.Timestamp, Args: b.Input})
				case e.Type == "user" && b.Type == "tool_result":
					if k, ok := idx[b.ToolUseID]; ok {
						f.Calls[k].Error = b.IsError
						f.Calls[k].Result = clip(resultText(b.Content), resultCap)
					}
				}
			}
		}
	}
	return f, nil
}

func resultText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) == nil {
		var b strings.Builder
		for _, p := range parts {
			b.WriteString(p.Text)
		}
		return b.String()
	}
	return string(raw)
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// ReadPaneIntents reads the runtime's pane-intents.json for one chat.
func ReadPaneIntents(file, tab string) []PaneIntent {
	raw, err := os.ReadFile(file)
	if err != nil {
		return nil
	}
	var all map[string][]struct {
		Command struct {
			Kind   string `json:"kind"`
			Tab    string `json:"tab"`
			Target any    `json:"target"`
			Place  string `json:"place"`
		} `json:"command"`
	}
	if json.Unmarshal(raw, &all) != nil {
		return nil
	}
	var out []PaneIntent
	for _, it := range all[tab] {
		target := ""
		switch t := it.Command.Target.(type) {
		case string:
			target = t
		case nil:
		default:
			b, _ := json.Marshal(t)
			target = string(b)
		}
		out = append(out, PaneIntent{Kind: it.Command.Kind, Tab: it.Command.Tab, Target: target, Place: it.Command.Place})
	}
	return out
}

// ReadImageJobs reads the runtime's picture jobs of one chat (<data>/images/<job>/job.json).
func ReadImageJobs(dir, tab string) []ImageJob {
	files, _ := filepath.Glob(filepath.Join(dir, "*", "job.json"))
	sort.Strings(files)
	var out []ImageJob
	for _, p := range files {
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var j struct {
			Prompt string            `json:"prompt"`
			Conv   string            `json:"conv"`
			State  string            `json:"state"`
			Images []json.RawMessage `json:"images"`
			Error  string            `json:"error"`
		}
		if json.Unmarshal(raw, &j) != nil || (tab != "" && j.Conv != tab) {
			continue
		}
		out = append(out, ImageJob{Prompt: clip(j.Prompt, 300), State: j.State, Images: len(j.Images), Error: clip(j.Error, 300)})
	}
	return out
}

// ReadFiles lists a folder's files under a prefix, with the text of the small ones; dot folders other than the
// root's own files are skipped.
func ReadFiles(root, prefix string) []FileFact {
	var out []FileFact
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || len(out) >= filesCap {
			return nil
		}
		if d.IsDir() {
			if p != root && strings.HasPrefix(d.Name(), ".") || d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		ff := FileFact{Path: prefix + filepath.ToSlash(rel), Size: info.Size()}
		if info.Size() <= fileTextCap {
			if b, err := os.ReadFile(p); err == nil && !bytes.ContainsRune(b, 0) {
				ff.Text = string(b)
			}
		}
		out = append(out, ff)
		return nil
	})
	return out
}
