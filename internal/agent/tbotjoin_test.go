package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

func writeTbotJoin(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "join.json")
	body := `{"bot":"agent-anna","token":"agent-anna-dev-2","machine":"fleet-dev-2","publicKey":"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIPPQYNrmNnuK4y7UvFRlRp1N1wgB3HTVM51PQN/mKf9i"}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(tbotJoinFileEnv, path)
	return dir
}

// The machine sends the request on link up; the hub's approval writes join-approved and a revoke takes it away.
func TestTbotJoinRequestAndApproval(t *testing.T) {
	dir := writeTbotJoin(t)
	r := startHubAgent(t, rigOptions{})
	c := r.connect()
	ev, _ := c.waitEvent(hubproto.EventTbotJoin, 0, 5*time.Second)
	if ev == nil {
		t.Fatal("no tbot.join reached the hub")
	}
	if !strings.Contains(string(mustJSON(ev)), `"token":"agent-anna-dev-2"`) || strings.Contains(string(mustJSON(ev)), "PRIVATE") {
		t.Fatalf("tbot.join: %s", mustJSON(ev))
	}
	approved := filepath.Join(dir, tbotApprovedName)
	c.mustCall(hubproto.OpTbotJoined, map[string]any{"token": "agent-anna-dev-2", "status": "pending"})
	if _, err := os.Stat(approved); err == nil {
		t.Fatal("pending wrote join-approved")
	}
	c.mustCall(hubproto.OpTbotJoined, map[string]any{"token": "agent-other-dev-2", "status": "approved"})
	if _, err := os.Stat(approved); err == nil {
		t.Fatal("another token's approval wrote join-approved")
	}
	c.mustCall(hubproto.OpTbotJoined, map[string]any{"token": "agent-anna-dev-2", "status": "approved"})
	if b, err := os.ReadFile(approved); err != nil || strings.TrimSpace(string(b)) != tbotApprovedMark {
		t.Fatalf("join-approved %q %v", b, err)
	}
	c.mustCall(hubproto.OpTbotJoined, map[string]any{"token": "agent-anna-dev-2", "status": "revoked"})
	if _, err := os.Stat(approved); err == nil {
		t.Fatal("revoke left join-approved")
	}
}

// A join-approved the operator wrote by hand (tbot joined another way) is neither asked about nor removed.
func TestTbotJoinLeavesAHandStartedTbot(t *testing.T) {
	dir := writeTbotJoin(t)
	approved := filepath.Join(dir, tbotApprovedName)
	if err := os.WriteFile(approved, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	r := startHubAgent(t, rigOptions{})
	c := r.connect()
	if ev, _ := c.waitEvent(hubproto.EventTbotJoin, 0, 500*time.Millisecond); ev != nil {
		t.Fatalf("asked: %v", ev)
	}
	c.mustCall(hubproto.OpTbotJoined, map[string]any{"token": "agent-anna-dev-2", "status": "revoked"})
	if _, err := os.Stat(approved); err != nil {
		t.Fatal("a hand-made join-approved was removed")
	}
}

// An observe link carries the request only when the operator opted in.
func TestTbotJoinOnAnObserveLink(t *testing.T) {
	writeTbotJoin(t)
	r := startHubAgent(t, rigOptions{observe: true})
	c := r.connect()
	if ev, _ := c.waitEvent(hubproto.EventTbotJoin, 0, 500*time.Millisecond); ev != nil {
		t.Fatalf("asked without the opt-in: %v", ev)
	}
	if _, errText := c.call(hubproto.OpTbotJoined, map[string]any{"token": "agent-anna-dev-2", "status": "approved"}); errText == "" {
		t.Fatal("tbot.joined served without the opt-in")
	}
}

func TestTbotJoinOnAnObserveLinkOptedIn(t *testing.T) {
	writeTbotJoin(t)
	t.Setenv(tbotJoinOptIn, "on")
	r := startHubAgent(t, rigOptions{observe: true})
	c := r.connect()
	if ev, _ := c.waitEvent(hubproto.EventTbotJoin, 0, 5*time.Second); ev == nil {
		t.Fatal("not asked with the opt-in")
	}
}
