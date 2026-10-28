package agent

import (
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// policyTaps is a runtime stand-in that keeps what the agent told it of the terminal policy.
type policyTaps struct {
	mu     sync.Mutex
	bodies []string
	cookie bool
}

func (p *policyTaps) route(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasSuffix(r.URL.Path, "/api/agent/policy") {
		return false
	}
	raw, _ := io.ReadAll(r.Body)
	p.mu.Lock()
	p.bodies = append(p.bodies, string(raw))
	p.cookie = r.Header.Get("cookie") != "" && r.Method == http.MethodPost
	p.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
	return true
}

func (p *policyTaps) last() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.bodies) == 0 {
		return ""
	}
	return p.bodies[len(p.bodies)-1]
}

func (p *policyTaps) waitFor(t *testing.T, body string, within time.Duration) {
	t.Helper()
	for deadline := time.Now().Add(within); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if p.last() == body {
			return
		}
	}
	t.Fatalf("the runtime was told %q last, wanted %q (all: %v)", p.last(), body, p.bodies)
}

// The runtime cannot see the policy file: the agent tells it, at start and again when the file changes, with the
// session it signs for itself.
func TestAgentTellsTheRuntimeWhetherTheTerminalIsOff(t *testing.T) {
	taps := &policyTaps{}
	r := startHubAgent(t, rigOptions{policy: "terminal = false\n", runtimeRoute: taps.route})
	taps.waitFor(t, `{"terminal":false}`, 10*time.Second)
	taps.mu.Lock()
	signed := taps.cookie
	taps.mu.Unlock()
	if !signed {
		t.Error("the runtime was told without the agent's session")
	}

	// the owner opens the terminal: the runtime hears it without waiting for the next round
	writeFile(t, filepath.Join(r.rig.Home, "policy.toml"), "terminal = true\n")
	r.agent.ReloadPolicy()
	taps.waitFor(t, `{"terminal":true}`, 10*time.Second)

	// and closes it again
	writeFile(t, filepath.Join(r.rig.Home, "policy.toml"), "terminal = false\n")
	r.agent.ReloadPolicy()
	taps.waitFor(t, `{"terminal":false}`, 10*time.Second)
}

// With no policy file the terminal is on, and that is what the runtime is told: a hub's runtime starts with it off and
// has to learn it is on.
func TestAgentTellsTheRuntimeTheTerminalIsOnByDefault(t *testing.T) {
	taps := &policyTaps{}
	startHubAgent(t, rigOptions{runtimeRoute: taps.route})
	taps.waitFor(t, `{"terminal":true}`, 10*time.Second)
}

// A runtime that has not written its secret yet is asked again soon, not after the long round.
func TestAgentTriesAgainWhenTheRuntimeIsNotReady(t *testing.T) {
	taps := &policyTaps{}
	r := startHubAgent(t, rigOptions{policy: "terminal = false\n", runtimeRoute: taps.route, noSecretYet: true})
	time.Sleep(300 * time.Millisecond)
	if got := taps.last(); got != "" {
		t.Fatalf("told before the runtime had a secret: %q", got)
	}
	writeFile(t, r.secret, hubTestSecret)
	taps.waitFor(t, `{"terminal":false}`, runtimePolicyRetry+8*time.Second)
}
