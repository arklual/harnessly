package main

import (
	"encoding/json"
	"testing"
	"time"

	"harnessly/internal/agent"
	"harnessly/internal/status"
)

func TestShadowReportsOnlyLastingDifferences(t *testing.T) {
	m := status.New(status.Deps{Layouts: readonlyLayouts{}, Tmux: coreTmux{}}, nullHub{})
	s := &coreShadow{m: m, old: map[string]shadowTab{}, diffs: map[string]*shadowDiff{}}
	s.take([]byte(`{"type":"status:sync","tabs":{"t1":{"cliState":"idle","workspaceId":"ws","tabName":"a"}}}`))
	if len(s.old) != 1 {
		t.Fatalf("%v", s.old)
	}
	saved := shadowGrace
	shadowGrace = 0
	defer func() { shadowGrace = saved }()
	s.compare() // first sight of the difference
	if s.differed != 0 {
		t.Fatal("reported at first sight")
	}
	s.compare()
	if s.differed != 1 {
		t.Fatalf("a lasting difference was not reported: %d", s.differed)
	}
	s.compare()
	if s.differed != 1 {
		t.Fatal("one difference reported twice")
	}
	s.take([]byte(`{"type":"status:update","tabId":"t1","cliState":null,"workspaceId":"","tabName":""}`))
	s.compare()
	if len(s.diffs) != 0 {
		t.Fatalf("a removed tab still differs: %v", s.diffs)
	}
}

func TestShadowFields(t *testing.T) {
	sid := "s1"
	a := shadowTab{CliState: "busy", WorkspaceID: "ws", AgentSessionID: &sid}
	b := a
	b.CliState = "idle"
	b.AgentSessionID = nil
	got := shadowFields(a, b, true, true)
	raw, _ := json.Marshal(got)
	if string(raw) != `[["cliState","busy","idle"],["agentSessionId","s1","null"]]` {
		t.Fatalf("%s", raw)
	}
	if len(shadowFields(a, a, true, true)) != 0 {
		t.Fatal("equal views differ")
	}
	_ = time.Second
}

func TestSignSessionIsAPageSession(t *testing.T) {
	tok := signSession([]byte("secret"), "x", time.Minute)
	if err := agent.VerifySession([]byte("secret"), tok, time.Now()); err != nil {
		t.Fatal(err)
	}
}
