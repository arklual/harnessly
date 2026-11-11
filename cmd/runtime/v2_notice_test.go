package main

import (
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/convo"
	"github.com/ly-solutions/harnessly/internal/status"
	"github.com/ly-solutions/harnessly/internal/wire"
)

type sentPush struct {
	tab, kind, event, transcript string
}

type notePusher struct{ sent []sentPush }

func (p *notePusher) Push(tabID string, e status.Entry, kind string) {
	ev := ""
	if e.LastEvent.Has() {
		ev = e.LastEvent.Val.Name
	}
	p.sent = append(p.sent, sentPush{tabID, kind, ev, e.JSONLPath})
}

type noticeRig struct {
	n         *chatNotices
	p         *notePusher
	rec       wire.Conversation
	owned     bool
	snapshots []string
	codex     []string
}

func newNoticeRig() *noticeRig {
	r := &noticeRig{p: &notePusher{}, owned: true, rec: wire.Conversation{ID: "c1", Session: "s1", Cwd: "/w",
		Provider: wire.ProviderClaude, TranscriptPath: "/t/c1.jsonl", State: wire.AgentState{K: wire.StateReady, Since: 10}}}
	r.n = &chatNotices{
		owns:    func(s string) bool { return r.owned && s == "s1" },
		record:  func(string) (wire.Conversation, bool) { return r.rec, true },
		entry:   func(string) (string, status.Entry, bool) { return "tab1", status.Entry{WorkspaceID: "ws1"}, true },
		push:    func() status.Pusher { return r.p },
		turnEnd: func(tab, cwd string) { r.snapshots = append(r.snapshots, tab+":"+cwd) },
		cwd:     func(string) string { return "/pane" },
		codex:   func(t string) { r.codex = append(r.codex, t) },
		after:   func(_ time.Duration, f func()) { f() },
	}
	return r
}

func (r *noticeRig) tell(k convo.NoticeKind) { r.n.notice(convo.Notice{Kind: k, Conv: r.rec}) }

func (r *noticeRig) pushes() string {
	var s []string
	for _, p := range r.p.sent {
		s = append(s, p.tab+"/"+p.kind+"/"+p.event)
	}
	return strings.Join(s, ",")
}

// A chat's turn end pushes once, from the keeper's tab with the chat's transcript, and snapshots the pane's folder
// under the chat's id: the rewind looks there.
func TestChatNoticeDone(t *testing.T) {
	r := newNoticeRig()
	r.tell(convo.NoticeTurnEnded)
	r.tell(convo.NoticeDone)
	if got := r.pushes(); got != "tab1/done/stop" || r.p.sent[0].transcript != "/t/c1.jsonl" {
		t.Fatalf("pushes: %s %+v", got, r.p.sent)
	}
	if strings.Join(r.snapshots, ",") != "c1:/pane" || len(r.codex) != 0 {
		t.Fatalf("snapshots %v, codex %v", r.snapshots, r.codex)
	}
}

// A failed turn pushes as an error; a question as a question; a Codex chat's turn end reads its rate limits.
func TestChatNoticeFailedQuestionCodex(t *testing.T) {
	r := newNoticeRig()
	r.tell(convo.NoticeFailed)
	r.rec.State = wire.AgentState{K: wire.StateWaitingQuestion, Since: 20}
	r.tell(convo.NoticeQuestion)
	if got := r.pushes(); got != "tab1/done/stop-failure,tab1/question/" {
		t.Fatalf("pushes: %s", got)
	}
	r.rec.Provider = wire.ProviderCodex
	r.tell(convo.NoticeTurnEnded)
	if len(r.codex) != 1 || r.codex[0] != "/t/c1.jsonl" {
		t.Fatalf("codex limits: %v", r.codex)
	}
}

// A chat that moved on before the push settles tells nobody; a session that is not a chat's is the keeper's.
func TestChatNoticeSkips(t *testing.T) {
	r := newNoticeRig()
	r.n.after = func(_ time.Duration, f func()) {
		r.rec.State = wire.AgentState{K: wire.StateWorking, Since: 30}
		f()
	}
	r.tell(convo.NoticeDone)
	if got := r.pushes(); got != "" {
		t.Fatalf("a chat at work again was pushed: %s", got)
	}
	r = newNoticeRig()
	r.owned = false
	r.tell(convo.NoticeDone)
	r.tell(convo.NoticeTurnEnded)
	if got := r.pushes(); got != "" || len(r.snapshots) != 0 {
		t.Fatalf("a session not a chat's: %s %v", got, r.snapshots)
	}
}

// The chat's states reach the keeper's tab in the keeper's words, off the actor; a session not a chat's is not
// mirrored.
func TestChatNoticeMirrorsTheState(t *testing.T) {
	r := newNoticeRig()
	var got []string
	r.n.mirror = func(s string, st status.ChatState) {
		got = append(got, s+":"+st.CliState+"/"+st.Event+"/"+st.Transcript)
	}
	r.rec.State = wire.AgentState{K: wire.StateWorking}
	r.tell(convo.NoticeState)
	r.rec.State = wire.AgentState{K: wire.StateReady}
	r.tell(convo.NoticeState)
	r.rec.State = wire.AgentState{K: wire.StateStarting}
	r.tell(convo.NoticeState)
	r.owned = false
	r.tell(convo.NoticeState)
	want := "s1:busy/prompt-submit//t/c1.jsonl,s1:ready-for-review/stop//t/c1.jsonl,s1://"
	if j := strings.Join(got, ","); !strings.HasPrefix(j, want) || len(got) != 3 {
		t.Fatalf("mirrored: %s", j)
	}
	if len(r.p.sent) != 0 {
		t.Fatalf("a state pushed: %+v", r.p.sent)
	}
}

// States told faster than the keeper takes them coalesce to the latest, in order.
func TestChatNoticeMirrorCoalesces(t *testing.T) {
	r := newNoticeRig()
	var queued []func()
	r.n.after = func(_ time.Duration, f func()) { queued = append(queued, f) }
	var got []string
	r.n.mirror = func(_ string, st status.ChatState) { got = append(got, st.CliState) }
	r.rec.State = wire.AgentState{K: wire.StateWorking}
	r.tell(convo.NoticeState)
	r.rec.State = wire.AgentState{K: wire.StateReady}
	r.tell(convo.NoticeState)
	if len(queued) != 1 {
		t.Fatalf("%d drainers", len(queued))
	}
	queued[0]()
	if strings.Join(got, ",") != "ready-for-review" {
		t.Fatalf("mirrored: %v", got)
	}
}
