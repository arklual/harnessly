package sessionmeta

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// agentTail is how much of a subagent's transcript is read to find its last step.
const agentTail = 256 << 10

// Agent is what a subagent's own files say about it. Every field is "" when they do not say.
type Agent struct {
	// Model is the id its requests went out with (claude-sonnet-5-5), else the alias it was started with.
	Model string
	// Effort is the output_config of its last request.
	Effort string
	// Role is the kind of agent it was started as («Explore», «general-purpose», a teammate's name).
	Role string
	// Last is its last step in a line: «Read · web/app/next/shell.tsx», or the first words of its last text.
	Last string
	// Done is whether Claude marked it finished.
	Done bool
}

type agentCache struct {
	mtime time.Time
	size  int64
	agent Agent
}

var (
	agents   = map[string]agentCache{} // by the subagent's transcript
	agentsMu sync.Mutex
)

// AgentFiles are the files of a subagent: <session>/subagents/agent-<id>.{jsonl,meta.json} beside the transcript.
func AgentFiles(transcript, id string) (jsonl, meta string) {
	dir := filepath.Join(strings.TrimSuffix(transcript, ".jsonl"), "subagents")
	return filepath.Join(dir, "agent-"+id+".jsonl"), filepath.Join(dir, "agent-"+id+".meta.json")
}

// ReadAgent reads a subagent of a session. A read of files that did not change since the last one is free.
func ReadAgent(transcript, id string) Agent {
	return ReadAgentFiles(AgentFiles(transcript, id))
}

// ReadAgentFiles reads a subagent from its transcript and meta file wherever they are (a workflow keeps its agents in
// a folder of the run). A read of files that did not change since the last one is free.
func ReadAgentFiles(jsonl, metaFile string) Agent {
	st, err := os.Stat(jsonl)
	if err != nil {
		return readMeta(metaFile)
	}
	agentsMu.Lock()
	c, ok := agents[jsonl]
	agentsMu.Unlock()
	if ok && c.mtime.Equal(st.ModTime()) && c.size == st.Size() {
		return c.agent
	}
	a := readMeta(metaFile)
	scanAgent(jsonl, st.Size(), &a)
	agentsMu.Lock()
	agents[jsonl] = agentCache{st.ModTime(), st.Size(), a}
	agentsMu.Unlock()
	return a
}

func readMeta(file string) Agent {
	raw, err := os.ReadFile(file)
	if err != nil {
		return Agent{}
	}
	var m struct {
		AgentType string `json:"agentType"`
		Model     string `json:"model"`
		Finished  bool   `json:"finished"`
	}
	if json.Unmarshal(raw, &m) != nil {
		return Agent{}
	}
	return Agent{Model: m.Model, Role: m.AgentType, Done: m.Finished}
}

func scanAgent(file string, size int64, a *Agent) {
	f, err := os.Open(file)
	if err != nil {
		return
	}
	defer f.Close()
	from := max(0, size-agentTail)
	buf := make([]byte, size-from)
	n, _ := f.ReadAt(buf, from)
	buf = buf[:n]
	if from > 0 {
		buf = buf[bytes.IndexByte(buf, '\n')+1:]
	}
	for _, raw := range bytes.Split(buf, []byte{'\n'}) {
		switch {
		case bytes.HasPrefix(raw, []byte(`{"type":"api-request",`)):
			var l struct {
				Params struct {
					Model        string `json:"model"`
					OutputConfig struct {
						Effort string `json:"effort"`
					} `json:"output_config"`
				} `json:"params"`
			}
			if json.Unmarshal(raw, &l) == nil {
				if l.Params.Model != "" {
					a.Model = l.Params.Model
				}
				a.Effort = l.Params.OutputConfig.Effort
			}
		case bytes.Contains(raw, []byte(`"type":"assistant"`)):
			if last := lastStep(raw); last != "" {
				a.Last = last
			}
		}
	}
}

// lastStep is the last tool call of an assistant line, else its text, in one short line.
func lastStep(raw []byte) string {
	var l struct {
		Type    string `json:"type"`
		Message struct {
			Content []struct {
				Type  string         `json:"type"`
				Text  string         `json:"text"`
				Name  string         `json:"name"`
				Input map[string]any `json:"input"`
			} `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(raw, &l) != nil || l.Type != "assistant" {
		return ""
	}
	out := ""
	for _, b := range l.Message.Content {
		switch b.Type {
		case "tool_use":
			out = step(b.Name, b.Input)
		case "text":
			if t := oneLine(b.Text); t != "" {
				out = t
			}
		}
	}
	return out
}

// stepKeys are the input fields that say what a tool does, in the order they are looked for.
var stepKeys = []string{"description", "file_path", "path", "pattern", "command", "url", "query", "prompt"}

func step(name string, input map[string]any) string {
	for _, k := range stepKeys {
		if v, ok := input[k].(string); ok && strings.TrimSpace(v) != "" {
			if k == "file_path" || k == "path" {
				v = strings.TrimPrefix(v, "/home/coder/workspace/")
			}
			return name + " · " + oneLine(v)
		}
	}
	return name
}

// oneLine is the first line of a text, cut to a length that fits a row.
func oneLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if r := []rune(s); len(r) > 120 {
		s = string(r[:120]) + "…"
	}
	return s
}
