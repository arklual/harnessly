package chatwork

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

const (
	// scanMaxBytes bounds how much of a transcript is read; scanMaxLine one line of it.
	scanMaxBytes = 256 << 20
	scanMaxLine  = 32 << 20
)

// found is what a transcript tells of the work: the files tools wrote, the folders of commands that changed git, and
// the outputs that may name a merge request.
type found struct {
	strong  []string
	weak    []string
	outputs []string
}

// scan looks through a chat's transcript for work done before the registry was there: the files Claude's write tools
// and Codex's patches wrote, the folders of the commands that changed git (cd, git -C, ~/ and absolute paths, a new
// worktree), and the merge requests the pushes and `glab mr create` printed. What it finds is registered as scanned
// work and looked at in git now. The chat is marked scanned even when it has no transcript.
func (o *Observer) scan(chat Chat) {
	if o.Reg.Scanned(chat.ID) {
		return
	}
	defer o.Reg.MarkScanned(chat.ID)
	if chat.Transcript == "" {
		return
	}
	f, err := os.Open(chat.Transcript)
	if err != nil {
		return
	}
	defer f.Close()
	got := readTranscript(io.LimitReader(f, scanMaxBytes), chat.Cwd, o.Home)
	seen := map[string]bool{}
	for _, path := range got.strong {
		if p, ok := o.locate(path); ok && !seen[p.worktree] {
			seen[p.worktree] = true
			o.register(chat, p, hubproto.ChatWorkScan, false)
		}
	}
	for _, path := range got.weak {
		if p, ok := o.locate(path); ok && !seen[p.worktree] {
			seen[p.worktree] = true
			o.register(chat, p, hubproto.ChatWorkScan, true)
		}
	}
	// The last merge request printed for a project is the chat's.
	for i := len(got.outputs) - 1; i >= 0; i-- {
		for _, e := range o.Reg.Of(chat.ID) {
			if e.MR != "" {
				continue
			}
			if mr := lastMR(got.outputs[i], e.Remote); mr != "" {
				e.MR = mr
				o.Reg.Put(e)
			}
		}
	}
}

// lastMR is the last merge request address of text on the remote's project, "" for none.
func lastMR(text, remote string) string {
	all := mrURL.FindAllStringSubmatch(text, -1)
	for i := len(all) - 1; i >= 0; i-- {
		if mr := mrOfProject(all[i][0], remote); mr != "" {
			return mr
		}
	}
	return ""
}

// readTranscript walks a Claude or Codex transcript, one JSON object a line.
func readTranscript(r io.Reader, cwd, home string) found {
	var got found
	seen := map[string]bool{}
	add := func(list *[]string, ps ...string) {
		for _, p := range ps {
			if p != "" && !seen[p] {
				seen[p] = true
				*list = append(*list, p)
			}
		}
	}
	commands := map[string]string{} // tool use id → its command, for the results that answer it
	shell := func(id, cmd, dir string) {
		if dir == "" {
			dir = cwd
		}
		if changesGit(cmd) {
			add(&got.weak, commandPaths(cmd, dir, home)...)
		}
		if id != "" && mrCommand.MatchString(cmd) {
			commands[id] = cmd
		}
	}
	br := bufio.NewReaderSize(r, 1<<20)
	for {
		line, err := br.ReadSlice('\n')
		if err == bufio.ErrBufferFull {
			// A line longer than the buffer: read it whole only up to scanMaxLine.
			long := append([]byte(nil), line...)
			for err == bufio.ErrBufferFull && len(long) < scanMaxLine {
				line, err = br.ReadSlice('\n')
				long = append(long, line...)
			}
			for err == bufio.ErrBufferFull {
				_, err = br.ReadSlice('\n')
			}
			line = long
		}
		if len(line) > 0 && (strings.Contains(string(line), "tool_use") || strings.Contains(string(line), "function_call") ||
			strings.Contains(string(line), "tool_result") || strings.Contains(string(line), "custom_tool_call")) {
			var v any
			if json.Unmarshal(line, &v) == nil {
				walk(v, func(o map[string]any) {
					typ, _ := o["type"].(string)
					switch typ {
					case "tool_use":
						name, _ := o["name"].(string)
						id, _ := o["id"].(string)
						in, _ := o["input"].(map[string]any)
						switch {
						case writeTools[name]:
							if p, _ := in["file_path"].(string); p != "" {
								add(&got.strong, resolve(p, cwd, home))
							}
						case name == "Bash":
							cmd, _ := in["command"].(string)
							shell(id, cmd, "")
						}
					case "function_call", "custom_tool_call":
						name, _ := o["name"].(string)
						id, _ := o["call_id"].(string)
						if name == "apply_patch" {
							in, _ := o["input"].(string)
							add(&got.strong, patchPaths(in, cwd)...)
							return
						}
						args, _ := o["arguments"].(string)
						var a map[string]any
						if json.Unmarshal([]byte(args), &a) != nil {
							return
						}
						dir, _ := a["workdir"].(string)
						switch c := a["command"].(type) {
						case string:
							shell(id, c, dir)
						case []any:
							parts := make([]string, 0, len(c))
							for _, p := range c {
								if s, ok := p.(string); ok {
									parts = append(parts, s)
								}
							}
							shell(id, strings.Join(parts, " "), dir)
						}
						if c, ok := a["cmd"].(string); ok {
							shell(id, c, dir)
						}
					case "tool_result", "function_call_output":
						id, _ := o["tool_use_id"].(string)
						if id == "" {
							id, _ = o["call_id"].(string)
						}
						if _, ok := commands[id]; ok {
							got.outputs = append(got.outputs, textOf(o["content"])+textOf(o["output"]))
						}
					}
				})
			}
		}
		if err != nil {
			break
		}
	}
	return got
}

// walk calls fn with every object in v.
func walk(v any, fn func(map[string]any)) {
	switch x := v.(type) {
	case map[string]any:
		fn(x)
		for _, c := range x {
			walk(c, fn)
		}
	case []any:
		for _, c := range x {
			walk(c, fn)
		}
	}
}

func textOf(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []any:
		var b strings.Builder
		for _, c := range x {
			if m, ok := c.(map[string]any); ok {
				if t, ok := m["text"].(string); ok {
					b.WriteString(t)
					b.WriteByte('\n')
				}
			}
		}
		return b.String()
	}
	return ""
}

// patchPaths are the files a Codex patch names (*** Add File: p, *** Update File: p, *** Delete File: p).
func patchPaths(patch, cwd string) []string {
	var out []string
	for _, l := range strings.Split(patch, "\n") {
		for _, h := range []string{"*** Add File: ", "*** Update File: ", "*** Delete File: "} {
			if p, ok := strings.CutPrefix(l, h); ok {
				if r := resolve(strings.TrimSpace(p), cwd, ""); r != "" {
					out = append(out, r)
				}
			}
		}
	}
	return out
}
