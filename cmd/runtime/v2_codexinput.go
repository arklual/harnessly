package main

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/ly-solutions/harnessly/internal/cliversion"
	"github.com/ly-solutions/harnessly/internal/codexapp"
	"github.com/ly-solutions/harnessly/internal/convo"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// codexInput is the structured input channel of Codex chats, on with RUNTIME_CODEX_APPSERVER=1: a new chat starts
// beside its own `codex app-server`, and its messages, the ends of its turns and its approvals go over that server
// instead of the terminal. Everything that does not work this way (the flag off, a Codex too old, a server that is
// not there) leaves the chat typing in its terminal.
type codexInput struct {
	mgr *codexapp.Manager
	// version reads `codex --version` of the binary at a path.
	version func(bin string) (string, error)
	rt      atomic.Pointer[convo.Runtime]
}

// newCodexInput is the channel when the flag turns it on, nil otherwise.
func newCodexInput(dataDir, flag string) *codexInput {
	if flag != "1" {
		return nil
	}
	c := &codexInput{version: func(bin string) (string, error) {
		out, err := exec.Command(bin, "--version").Output()
		return strings.TrimSpace(string(out)), err
	}}
	c.mgr = &codexapp.Manager{Dir: filepath.Join(dataDir, "codex-input"), Dial: codexapp.ProxyDial("codex"), Sink: c}
	return c
}

// codexInputOf is the channel the core's conversations take, nil when the core starts no agents or the flag is off.
func codexInputOf(c *core) convo.Input {
	if c == nil || c.agents == nil {
		return nil
	}
	return c.agents.codexInput.input()
}

// input is the channel as the conversations take it: nil when it is off.
func (c *codexInput) input() convo.Input {
	if c == nil {
		return nil
	}
	return c
}

// socket is where a new chat's app-server is to listen; "" when the machine's Codex has no app-server this runtime
// knows, and the chat starts as ever.
func (c *codexInput) socket(conv string) string {
	bin, err := exec.LookPath("codex")
	if err != nil {
		return ""
	}
	v, err := cliversion.Of(bin, func() (string, error) { return c.version(bin) })
	if err != nil || !codexapp.Supported(v) {
		return ""
	}
	return c.mgr.Socket(conv)
}

func (c *codexInput) Send(ctx context.Context, conv wire.Conversation, m convo.InputMessage) error {
	err := c.mgr.Send(ctx, conv.ID, codexapp.Message{ID: m.ClientID, Text: m.Text, Images: m.Images, Steer: m.Steer})
	if errors.Is(err, codexapp.ErrUncertain) {
		return errors.Join(convo.ErrInputUncertain, err)
	}
	return err
}

func (c *codexInput) TurnStarted(string, string) {}

// TurnEnded: a turn the person stopped is not told here — the stop's own path ends it.
func (c *codexInput) TurnEnded(conv, _, status, detail string) {
	rt := c.rt.Load()
	if rt == nil || status != codexapp.TurnCompleted && status != codexapp.TurnFailed {
		return
	}
	_ = rt.InputTurnEnded(conv, status == codexapp.TurnFailed, detail)
}

// Approval puts what Codex asks on the person's card and carries the decision back; false leaves it to Codex's dialog
// in the terminal.
func (c *codexInput) Approval(ctx context.Context, conv string, a codexapp.Approval) (codexapp.Decision, bool) {
	rt := c.rt.Load()
	if rt == nil {
		return "", false
	}
	ask := convo.PermissionAsk{Tool: "Bash", Cwd: a.Cwd}
	fields := map[string]string{"command": a.Command}
	if a.Command == "" {
		ask.Tool, fields = "Edit", map[string]string{"file_path": strings.Join(a.Paths, ", ")}
	}
	ask.Input, _ = json.Marshal(fields)
	reply, ok := rt.PermissionRequest(ctx, conv, ask)
	switch {
	case !ok:
		return "", false
	case reply.Behavior == "allow":
		return codexapp.Accept, true
	case reply.Interrupt:
		return codexapp.Cancel, true
	}
	return codexapp.Decline, true
}

func (c *codexInput) Lost(conv string) {
	if rt := c.rt.Load(); rt != nil {
		rt.InputLost(conv)
	}
}
