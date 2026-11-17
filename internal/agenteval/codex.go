package agenteval

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ly-solutions/harnessly/internal/apps"
)

// ReadCodexRollout reads a Codex session file (~/.codex/sessions/…/rollout-*.jsonl): the function calls with their
// outputs and what the agent said. Tool names are made the scenario's: an MCP tool is "<server>.<tool>", a shell call
// is "Bash" with its command, as a Claude transcript has them, so one scenario judges both agents.
func ReadCodexRollout(path string) (Facts, error) {
	var f Facts
	raw, err := os.ReadFile(path)
	if err != nil {
		return f, err
	}
	idx := map[string][2]int{}
	completedCommands := map[int]bool{}
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	for sc.Scan() {
		var e struct {
			Type    string `json:"type"`
			Payload struct {
				Type      string          `json:"type"`
				Role      string          `json:"role"`
				Name      string          `json:"name"`
				Arguments string          `json:"arguments"`
				Input     string          `json:"input"`
				CallID    string          `json:"call_id"`
				Output    json.RawMessage `json:"output"`
				Content   []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
				Model string `json:"model"`
				Item  *struct {
					Type     string   `json:"type"`
					Command  []string `json:"command"`
					ExitCode *int     `json:"exit_code"`
				} `json:"item"`
				Info *struct {
					Total struct {
						In     int `json:"input_tokens"`
						Cached int `json:"cached_input_tokens"`
						Out    int `json:"output_tokens"`
					} `json:"total_token_usage"`
				} `json:"info"`
			} `json:"payload"`
		}
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			continue
		}
		p := e.Payload
		switch {
		case e.Type == "turn_context" && p.Model != "":
			f.Model = p.Model
		case e.Type == "event_msg" && p.Type == "token_count" && p.Info != nil:
			f.TokensIn = p.Info.Total.In - p.Info.Total.Cached
			f.CacheRead = p.Info.Total.Cached
			f.TokensOut = p.Info.Total.Out
		case e.Type == "event_msg" && p.Type == "item_completed" && p.Item != nil && p.Item.Type == "CommandExecution" && p.Item.ExitCode != nil:
			// Code-mode text(r.output) may omit exit status from the tool output.
			// Keep the authoritative terminal event even when that output says "Script completed".
			args, _ := json.Marshal(map[string]any{"command": p.Item.Command})
			_, normalized := codexTool("shell", string(args))
			for k := range f.Calls {
				if !completedCommands[k] && f.Calls[k].Name == "Bash" && bytes.Equal(f.Calls[k].Args, normalized) {
					f.Calls[k].Error = f.Calls[k].Error || *p.Item.ExitCode != 0
					completedCommands[k] = true
					break
				}
			}
		case e.Type != "response_item":
		case p.Type == "message" && p.Role == "assistant":
			var b strings.Builder
			for _, c := range p.Content {
				b.WriteString(c.Text)
			}
			if t := b.String(); strings.TrimSpace(t) != "" {
				f.Said = append(f.Said, t)
				f.Final = t
			}
		case p.Type == "function_call" || p.Type == "custom_tool_call":
			args := p.Arguments
			if args == "" {
				args = p.Input
			}
			from := len(f.Calls)
			if p.Name == "exec" && p.Type == "custom_tool_call" {
				// Code mode: one script calls tools as tools.<name>({…}); each is a call of its own.
				for _, c := range codeModeCalls(args) {
					name, a := codexTool(c.name, c.args)
					f.Calls = append(f.Calls, Call{Name: name, Args: a})
				}
			}
			if len(f.Calls) == from {
				name, a := codexTool(p.Name, args)
				f.Calls = append(f.Calls, Call{Name: name, Args: a})
			}
			idx[p.CallID] = [2]int{from, len(f.Calls)}
		case p.Type == "function_call_output" || p.Type == "custom_tool_call_output":
			span, ok := idx[p.CallID]
			if !ok {
				continue
			}
			out := resultText(p.Output)
			var wrapped struct {
				Output   string `json:"output"`
				Metadata struct {
					ExitCode *int `json:"exit_code"`
				} `json:"metadata"`
			}
			if json.Unmarshal([]byte(out), &wrapped) == nil && wrapped.Output != "" {
				out = wrapped.Output
				for k := span[0]; k < span[1]; k++ {
					f.Calls[k].Error = f.Calls[k].Error || (wrapped.Metadata.ExitCode != nil && *wrapped.Metadata.ExitCode != 0)
				}
			}
			for k := span[0]; k < span[1]; k++ {
				f.Calls[k].Result = clip(out, resultCap)
			}
		}
	}
	return f, sc.Err()
}

// codexTool names a Codex call as a scenario does.
func codexTool(name, args string) (string, json.RawMessage) {
	a := json.RawMessage(args)
	if !json.Valid(a) {
		b, _ := json.Marshal(map[string]string{"input": args})
		a = b
	}
	switch name {
	case "shell", "exec_command", "local_shell", "shell_command":
		var x struct {
			Command any    `json:"command"`
			Cmd     string `json:"cmd"`
		}
		_ = json.Unmarshal(a, &x)
		cmd := x.Cmd
		switch c := x.Command.(type) {
		case string:
			cmd = c
		case []any:
			parts := make([]string, 0, len(c))
			for _, p := range c {
				parts = append(parts, toString(p))
			}
			if len(parts) >= 3 && (filepath.Base(parts[0]) == "bash" || filepath.Base(parts[0]) == "sh") && strings.HasPrefix(parts[1], "-") {
				cmd = parts[len(parts)-1]
			} else {
				cmd = strings.Join(parts, " ")
			}
		}
		b, _ := json.Marshal(map[string]string{"command": cmd})
		return "Bash", b
	case "apply_patch":
		return "Edit", a
	}
	if strings.HasPrefix(name, "mcp__") {
		return apps.ToolName(name), a
	}
	if server, tool, ok := strings.Cut(name, "__"); ok && server != "" && tool != "" {
		return server + "." + tool, a
	}
	return name, a
}

type codeCall struct{ name, args string }

var (
	codeToolRe = regexp.MustCompile(`tools\.([A-Za-z0-9_$.]+)\s*\(`)
	bareKeyRe  = regexp.MustCompile(`([{,]\s*)([A-Za-z_$][A-Za-z0-9_$]*)\s*:`)
)

// codeModeCalls finds the tool calls of a Codex code-mode script (tools.exec_command({cmd:"…"}),
// tools.mcp__harnessly__deliver_file({…})): the tool's name and its argument object as JSON when it reads as one.
func codeModeCalls(script string) []codeCall {
	var out []codeCall
	for _, m := range codeToolRe.FindAllStringSubmatchIndex(script, -1) {
		name := strings.ReplaceAll(script[m[2]:m[3]], ".", "__")
		arg := balancedArg(script[m[1]:])
		js := bareKeyRe.ReplaceAllString(arg, `$1"$2":`)
		if !json.Valid([]byte(js)) {
			js = arg
		}
		out = append(out, codeCall{name: name, args: js})
	}
	return out
}

// balancedArg is the first argument of a call whose "(" was just read: up to its "," or ")" at depth zero, outside
// strings.
func balancedArg(s string) string {
	depth, quote, esc := 0, byte(0), false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case esc:
			esc = false
		case quote != 0:
			if c == '\\' {
				esc = true
			} else if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'' || c == '`':
			quote = c
		case c == '{' || c == '[' || c == '(':
			depth++
		case c == '}' || c == ']':
			depth--
		case c == ')':
			if depth == 0 {
				return strings.TrimSpace(s[:i])
			}
			depth--
		case c == ',' && depth == 0:
			return strings.TrimSpace(s[:i])
		}
	}
	return strings.TrimSpace(s)
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}
