package hubproto

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseError(t *testing.T) {
	tests := []struct {
		in, code, text string
	}{
		{"policy_denied: terminal is off", CodePolicyDenied, "terminal is off"},
		{"busy: a login is running", CodeBusy, "a login is running"},
		{"not_found: no such login", CodeNotFound, "no such login"},
		{"invalid: bad name", CodeInvalid, "bad name"},
		{"unsupported: gemini", CodeUnsupported, "gemini"},
		{"failed: boom", CodeFailed, "boom"},
		{"unknown operation foo", CodeFailed, "unknown operation foo"},
		{"made_up: x", CodeFailed, "made_up: x"},
		{"busy", CodeFailed, "busy"},
	}
	for _, tc := range tests {
		e := ParseError(tc.in)
		if e.Code != tc.code || e.Text != tc.text {
			t.Errorf("%q: %+v", tc.in, e)
		}
	}
	if e := Errf(CodeInvalid, "name %q", "A"); e.Error() != `invalid: name "A"` {
		t.Errorf("%v", e)
	}
}

// The JSON names are the contract: the hub and the UI read them.
func TestWireNames(t *testing.T) {
	status := AgentStatus{Provider: "claude", Installed: true, Login: LoggedIn}
	raw, _ := json.Marshal(status)
	for _, want := range []string{`"version":null`, `"authMode":null`, `"overriddenBy":null`, `"access":null`, `"login":"logged_in"`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("%s missing from %s", want, raw)
		}
	}
	if strings.Contains(string(raw), "soon") {
		t.Errorf("soon is for gemini only: %s", raw)
	}
	raw, _ = json.Marshal(Login{ID: "x", State: LoginStarting})
	for _, want := range []string{`"needsCode":false`, `"userCode":null`, `"consoleUrl":null`, `"expiresAt":null`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("%s missing from %s", want, raw)
		}
	}
	raw, _ = json.Marshal(KeyCheck{Provider: "codex", Verdict: KeyAbsent})
	if strings.Contains(string(raw), "saved") || !strings.Contains(string(raw), `"status":null`) {
		t.Errorf("%s", raw)
	}
	raw, _ = json.Marshal(Hello{Protocol: Version, Features: []string{"a"}})
	if !strings.Contains(string(raw), `"protocol":2`) || !strings.Contains(string(raw), `"machineId"`) {
		t.Errorf("%s", raw)
	}
	tmux := ToolState{Installed: true}
	raw, _ = json.Marshal(Tools{Tmux: tmux})
	if !strings.Contains(string(raw), `"tmux":{"installed":true,"version":null}`) {
		t.Errorf("%s", raw)
	}
}

func TestProfileAndVia(t *testing.T) {
	for _, p := range []Profile{ProfileDir, ProfileContainer, ProfileFull} {
		if !p.Valid() {
			t.Errorf("%s", p)
		}
	}
	if Profile("root").Valid() || Via("teleport").Valid() || !ViaLocal.Valid() {
		t.Error("validity")
	}
}
