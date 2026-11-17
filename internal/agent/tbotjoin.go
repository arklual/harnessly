package agent

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// The machine's Teleport identity (tbot) waits for its join token (docs/hub/tbot-join.md). The operator's setup
// made a key pair and wrote join.json next to it; the agent sends the request to the hub, and when the person approves it
// the agent writes join-approved beside join.json. A systemd path unit watches that file and starts tbot. The agent
// never reads the private key.

const (
	// tbotJoinOptIn lets an observe link carry the request: the hub that observes the machine is the one the operator
	// trusts with it. An own link carries it always.
	tbotJoinOptIn = "AGENT_HUB_TBOT_JOIN"
	// tbotJoinFileEnv names the request; by default ~/.tbot/join.json.
	tbotJoinFileEnv = "AGENT_TBOT_JOIN_FILE"
	// tbotApprovedName is the file the path unit waits for; the hub's word is written into it so that only a file the
	// agent wrote is removed again.
	tbotApprovedName = "join-approved"
	tbotApprovedMark = "hub"
	// tbotJoinEvery is how often a request that is not approved asks again while the machine is linked.
	tbotJoinEvery = time.Hour
)

func tbotJoinFile() string {
	if p := strings.TrimSpace(os.Getenv(tbotJoinFileEnv)); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".tbot", "join.json")
}

func tbotApprovedFile(join string) string { return filepath.Join(filepath.Dir(join), tbotApprovedName) }

// tbotJoinServed says whether this link carries the request.
func (h *hub) tbotJoinServed() bool {
	if h.policy.Disabled() {
		return false
	}
	return !h.observe || os.Getenv(tbotJoinOptIn) == "on"
}

// tbotJoinRequest reads the request when there is one to send: join.json exists, and tbot was not started by hand (no
// join-approved, or one the agent wrote itself, so that a revoke is heard).
func tbotJoinRequest() (hubproto.TbotJoin, bool) {
	path := tbotJoinFile()
	if path == "" {
		return hubproto.TbotJoin{}, false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			Log("warn", "tbot join request not read", F{"error", err.Error()})
		}
		return hubproto.TbotJoin{}, false
	}
	var in hubproto.TbotJoin
	if json.Unmarshal(b, &in) != nil || in.Bot == "" || in.Token == "" || in.PublicKey == "" {
		Log("warn", "tbot join request is not {bot, token, publicKey}", F{"file", path})
		return hubproto.TbotJoin{}, false
	}
	if mark, err := os.ReadFile(tbotApprovedFile(path)); err == nil && strings.TrimSpace(string(mark)) != tbotApprovedMark {
		return hubproto.TbotJoin{}, false
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return hubproto.TbotJoin{}, false
	}
	return hubproto.TbotJoin{Bot: in.Bot, Token: in.Token, Machine: in.Machine, PublicKey: in.PublicKey}, true
}

// sendTbotJoin sends the request on the link, if there is one to send.
func (h *hub) sendTbotJoin(l *link) {
	if !h.tbotJoinServed() {
		return
	}
	in, ok := tbotJoinRequest()
	if !ok {
		return
	}
	frame, err := marshal(struct {
		Event string `json:"event"`
		hubproto.TbotJoin
	}{hubproto.EventTbotJoin, in})
	if err != nil {
		return
	}
	l.sendEvent(hubproto.EventTbotJoin, frame)
	h.audit.Record(hubproto.EventTbotJoin, true, map[string]any{"token": in.Token})
}

// tbotJoinLoop asks again every hour while the link holds: a request the hub had no answer to (the cluster did not
// answer) or one still waiting is filed again, and a revoke the machine missed is heard.
func (h *hub) tbotJoinLoop(l *link) {
	h.sendTbotJoin(l)
	t := time.NewTicker(tbotJoinEvery)
	defer t.Stop()
	for {
		select {
		case <-l.done:
			return
		case <-t.C:
			h.sendTbotJoin(l)
		}
	}
}

// opTbotJoined takes the hub's word on the request: approved writes join-approved, revoked or declined takes away the
// one the agent wrote.
func (h *hub) opTbotJoined(req map[string]json.RawMessage) (any, func(), error) {
	if !h.tbotJoinServed() {
		return nil, nil, hubproto.Errf(hubproto.CodePolicyDenied, "%s is not served on this link: the operator did not opt in with %s=on", hubproto.OpTbotJoined, tbotJoinOptIn)
	}
	token, _ := field[string](req, "token")
	status, _ := field[string](req, "status")
	path := tbotJoinFile()
	in, ok := tbotJoinRequest()
	if !ok || in.Token != token {
		h.audit.Record(hubproto.OpTbotJoined, false, map[string]any{"token": token, "status": status})
		return map[string]bool{"applied": false}, nil, nil
	}
	approved := tbotApprovedFile(path)
	var err error
	switch status {
	case hubproto.TbotJoinApproved:
		err = os.WriteFile(approved, []byte(tbotApprovedMark+"\n"), 0o600)
	case hubproto.TbotJoinRevoked, hubproto.TbotJoinDeclined:
		if err = os.Remove(approved); errors.Is(err, fs.ErrNotExist) {
			err = nil
		}
	default:
		// pending or refused: nothing changes on the machine.
		h.audit.Record(hubproto.OpTbotJoined, true, map[string]any{"token": token, "status": status})
		return map[string]bool{"applied": false}, nil, nil
	}
	h.audit.Record(hubproto.OpTbotJoined, err == nil, map[string]any{"token": token, "status": status})
	if err != nil {
		Log("warn", "tbot join state not written", F{"status", status}, F{"error", err.Error()})
		return nil, nil, err
	}
	Log("info", "tbot join "+status, F{"token", token})
	return map[string]bool{"applied": true}, nil, nil
}
