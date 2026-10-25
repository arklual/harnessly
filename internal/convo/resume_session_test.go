package convo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ly-solutions/harnessly/internal/wire"
)

const (
	sessOld   = "00000001-0000-4000-8000-000000000000"
	sessKnown = "00000002-0000-4000-8000-000000000000"
	sessGone  = "00000003-0000-4000-8000-000000000000"
	sessFile  = "00000004-0000-4000-8000-000000000000"
	sessNone  = "00000005-0000-4000-8000-000000000000"
)

// A session the agent kept outside the runtime opens a new conversation that shows the session's history and starts the
// agent on it; the same session again is the same conversation.
func TestResumeASessionOpensAConversationOnIt(t *testing.T) {
	r := newRig(t)
	folder := filepath.Join(r.dir, "app")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	r.user("old question")
	r.say("old answer")
	r.turnEnd()
	r.rt.d.AgentSession = func(p wire.Provider, id string) (AgentSession, bool) {
		if p != wire.ProviderClaude || id != sessOld {
			return AgentSession{}, false
		}
		return AgentSession{Title: "Old work", Cwd: folder, TranscriptPath: r.path}, true
	}

	res := r.do(wire.ConversationResumeCmd{OpID: "r1", Provider: wire.ProviderClaude, SessionID: sessOld, Cwd: folder})
	if res.R != wire.ResultAccepted || res.ID == nil || res.Ch != wire.ChannelConvs || !wire.ValidConvID(*res.ID) {
		t.Fatalf("resume: %+v", res)
	}
	r.id = *res.ID
	if len(r.launcher.launched) != 1 {
		t.Fatalf("launched %d", len(r.launcher.launched))
	}
	got := r.launcher.launched[0]
	if got.ID != r.id || got.Cwd != folder || got.Provider != wire.ProviderClaude || got.ResumeSessionID != sessOld || got.TranscriptPath != r.path || got.Model != nil {
		t.Fatalf("launch: %+v", got)
	}
	cs, _ := r.rt.Conversations(t.Context())
	if len(cs.Conversations) != 1 || cs.Conversations[0].Title != "Old work" || cs.Conversations[0].Cwd != folder {
		t.Fatalf("conversations: %+v", cs.Conversations)
	}
	snap := r.snap()
	if len(snap.Entries) < 2 || len(snap.Turns) != 1 {
		t.Fatalf("history: %d entries, %d turns", len(snap.Entries), len(snap.Turns))
	}
	if r.rt.AttachedSessions()[sessOld] != r.id {
		t.Fatalf("attached: %+v", r.rt.AttachedSessions())
	}

	again := r.do(wire.ConversationResumeCmd{OpID: "r2", Provider: wire.ProviderClaude, SessionID: sessOld, Cwd: folder})
	if again.R != wire.ResultAccepted || again.ID == nil || *again.ID != r.id || len(r.launcher.launched) != 1 {
		t.Fatalf("the same session again: %+v (%d launches)", again, len(r.launcher.launched))
	}
}

func TestResumeASessionRefusals(t *testing.T) {
	r := newRig(t)
	folder := filepath.Join(r.dir, "app")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(r.dir, "plain-file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := func(op, id, cwd string) wire.CmdResult {
		return r.do(wire.ConversationResumeCmd{OpID: op, Provider: wire.ProviderClaude, SessionID: id, Cwd: cwd})
	}
	if res := cmd("a", sessKnown, folder); res.R != wire.ResultRefused || res.Code != wire.RefuseUnsupported {
		t.Fatalf("a runtime that knows no sessions: %+v", res)
	}
	r.rt.d.AgentSession = func(_ wire.Provider, id string) (AgentSession, bool) {
		switch id {
		case sessKnown:
			return AgentSession{Title: "t", Cwd: folder, TranscriptPath: r.path}, true
		case sessGone:
			return AgentSession{Title: "t", Cwd: filepath.Join(r.dir, "absent"), TranscriptPath: r.path}, true
		case sessFile:
			return AgentSession{Title: "t", Cwd: file, TranscriptPath: r.path}, true
		}
		return AgentSession{}, false
	}
	for name, c := range map[string]struct {
		id, cwd string
		code    wire.RefuseCode
	}{
		"no such session":       {sessNone, folder, wire.RefuseNotFound},
		"another folder":        {sessKnown, r.dir, wire.RefuseBadRequest},
		"a folder that is gone": {sessGone, filepath.Join(r.dir, "absent"), wire.RefuseNotFound},
		"a file for a folder":   {sessFile, file, wire.RefuseNotFound},
	} {
		if res := cmd("op-"+name, c.id, c.cwd); res.R != wire.ResultRefused || res.Code != c.code {
			t.Errorf("%s: %+v", name, res)
		}
	}
	if len(r.launcher.launched) != 0 {
		t.Fatalf("launched %d", len(r.launcher.launched))
	}
	if bad := r.do(wire.ConversationResumeCmd{OpID: "x", ID: r.id, SessionID: sessKnown}); bad.Code != wire.RefuseBadRequest {
		t.Fatalf("both forms: %+v", bad)
	}
}
