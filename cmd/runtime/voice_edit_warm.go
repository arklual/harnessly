package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/sidechat"
)

// A quick edit by Claude Code waited ~7 s for the CLI to start and ~5 s more for its Read and Edit turns (HRN-1466,
// measured 15.11 on a loaded machine: 16–22 s in all). warmClaude keeps one Claude Code started ahead, without tools,
// that answers the edit as replacements in one turn (quickedit.PlanPrompt): ~3 s from the words to the answer. A
// process answers once and is let go (no context carries over); the next one starts at once, and the last one is let
// go after warmIdle without edits. Two wait, so edits said one right after another do not wait for a start either
// (measured 15.11: 2.7 s for the first, 4.1–4.9 s for the next ones with one waiting).
const (
	warmIdle = 10 * time.Minute
	warmKeep = 2
)

const warmSystem = "You make one small edit of a file a person looks at, as they asked out loud. You answer with the " +
	"JSON the message asks for and nothing else."

type warmProc struct {
	cmd *exec.Cmd
	in  io.WriteCloser
	out *bufio.Reader
	dir string
}

func (p *warmProc) stop() {
	_ = p.in.Close()
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
	_ = p.cmd.Wait()
	_ = os.RemoveAll(p.dir)
}

type warmClaude struct {
	d    deps
	mu   sync.Mutex
	next []*warmProc
	idle *time.Timer
}

func (w *warmClaude) spawn() (*warmProc, error) {
	dir, err := os.MkdirTemp("", "voice-edit-warm-")
	if err != nil {
		return nil, err
	}
	rt := sidechat.ReadRuntime(os.Getenv("RUNTIME_SIDE_CHAT_CONFIG"))
	cmd := exec.Command(rt.Binary, "--print", "--model", voiceEditModel(), "--tools", "",
		"--input-format", "stream-json", "--output-format", "stream-json", "--verbose",
		"--strict-mcp-config", "--disable-slash-commands", "--no-session-persistence",
		"--settings", `{"disableAllHooks":true}`, "--system-prompt", warmSystem)
	cmd.Env = sidechat.Env(withAgentEnv(os.Environ(), agentEnvDir(w.d.DataDir), agents.Claude), rt)
	cmd.Dir = dir
	in, err := cmd.StdinPipe()
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	return &warmProc{cmd: cmd, in: in, out: bufio.NewReaderSize(out, 64<<10), dir: dir}, nil
}

// Warm starts processes until warmKeep wait, and lets them go after warmIdle without edits.
func (w *warmClaude) Warm() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for len(w.next) < warmKeep {
		p, err := w.spawn()
		if err != nil {
			break
		}
		w.next = append(w.next, p)
	}
	if w.idle != nil {
		w.idle.Stop()
	}
	w.idle = time.AfterFunc(warmIdle, func() {
		w.mu.Lock()
		waiting := w.next
		w.next = nil
		w.mu.Unlock()
		for _, p := range waiting {
			p.stop()
		}
	})
}

// take is the longest waiting process (the warmest), or a new one.
func (w *warmClaude) take() (*warmProc, error) {
	w.mu.Lock()
	var p *warmProc
	if len(w.next) > 0 {
		p, w.next = w.next[0], w.next[1:]
	}
	w.mu.Unlock()
	if p != nil {
		return p, nil
	}
	return w.spawn()
}

// Plan is a quickedit.Planner: the prompt goes to the waiting process as one message, its result is the answer.
func (w *warmClaude) Plan(ctx context.Context, prompt string) (string, error) {
	p, err := w.take()
	if err != nil {
		return "", err
	}
	defer p.stop()
	defer func() { go w.Warm() }()
	msg, _ := json.Marshal(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": prompt}})
	if _, err := p.in.Write(append(msg, '\n')); err != nil {
		return "", err
	}
	type answer struct {
		text string
		err  error
	}
	got := make(chan answer, 1)
	go func() {
		for {
			line, err := p.out.ReadBytes('\n')
			if len(line) > 0 {
				var ev struct {
					Type    string `json:"type"`
					IsError bool   `json:"is_error"`
					Result  string `json:"result"`
				}
				if json.Unmarshal(line, &ev) == nil && ev.Type == "result" {
					if ev.IsError {
						got <- answer{err: errors.New("claude: " + clip(ev.Result, 200))}
					} else {
						got <- answer{text: ev.Result}
					}
					return
				}
			}
			if err != nil {
				got <- answer{err: errors.New("claude ended without an answer")}
				return
			}
		}
	}()
	select {
	case a := <-got:
		return a.text, a.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}
