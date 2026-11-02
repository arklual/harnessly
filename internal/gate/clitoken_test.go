package gate

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCLITokenAgentTokens(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cli-token"), []byte("person-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.UnixMilli(1_800_000_000_000)
	c := &CLIToken{DataDir: dir, Headers: []string{"x-cli-token"}, Now: func() time.Time { return now }}
	req := func(token string) (Principal, bool) {
		r := httptest.NewRequest("GET", "/api/cli/x", nil)
		if token != "" {
			r.Header.Set("x-cli-token", token)
		}
		return c.PrincipalOf(r)
	}

	if p, ok := req("person-token"); !ok || p.Agent {
		t.Fatalf("the person's token: %+v %v", p, ok)
	}
	agent, err := c.IssueAgent("launch-1", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(agent, "agent.launch-1.") {
		t.Fatalf("form: %q", agent)
	}
	if info, err := os.Stat(filepath.Join(dir, AgentTokenFile)); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the key is the runtime's alone: %v %v", info, err)
	}
	if p, ok := req(agent); !ok || !p.Agent || p.Session != "launch-1" {
		t.Fatalf("the agent's token: %+v %v", p, ok)
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("x-cli-token", agent)
	if s, ok := c.AgentOf(r); !ok || s != "launch-1" || !c.OK(r) {
		t.Fatalf("AgentOf %q %v", s, ok)
	}

	parts := strings.Split(agent, ".")
	for name, bad := range map[string]string{
		"another session":   "agent.launch-2." + parts[2] + "." + parts[3],
		"a later expiry":    "agent.launch-1.9" + parts[2] + "." + parts[3],
		"a changed mac":     agent[:len(agent)-1] + map[bool]string{true: "0", false: "1"}[agent[len(agent)-1] != '0'],
		"too few parts":     "agent.launch-1." + parts[3],
		"no token at all":   "",
		"a guess at a form": "agent.launch-1.1.x",
	} {
		if p, ok := req(bad); ok {
			t.Errorf("%s: let in as %+v", name, p)
		}
	}
	now = now.Add(2 * time.Hour)
	if _, ok := req(agent); ok {
		t.Fatal("an expired agent token")
	}
	// A key that is not there any more ends every agent token; the person's token stays.
	fresh, _ := c.IssueAgent("launch-3", time.Hour)
	_ = os.Remove(filepath.Join(dir, AgentTokenFile))
	if _, ok := req(fresh); ok {
		t.Fatal("a token of a removed key")
	}
	if _, ok := req("person-token"); !ok {
		t.Fatal("the person's token")
	}
	if _, err := c.IssueAgent("a.b", time.Hour); err != ErrAgentSession {
		t.Fatalf("a dotted session: %v", err)
	}
}
