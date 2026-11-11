package main

import (
	"path/filepath"
	"strings"

	"github.com/ly-solutions/harnessly/internal/safego"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// A chat finds its agent's transcript itself: every hook of its agent names the agent's session, and the session's
// file is found by that id (a Codex hook names the file too). The tab keeper's view (syncOnce) is the way only while
// the chats' sessions are left to the tabs' keepers (LegacyLife).

// followSession is follow for a hook that names its tmux session only.
func (v *v2Live) followSession(session, agentSession, transcript string) {
	if !v.ready.Load() {
		return
	}
	if id := v.rt.ConvOfSession(session); id != "" {
		v.follow(id, session, agentSession, transcript)
	}
}

// follow attaches the conversation to the agent session a hook of its own agent named, when that is not the one it
// reads already: with the transcript the hook gave, or the one found by the session's id. The search reads a
// directory, so it runs beside the hook; a transcript not written yet is found by a later hook.
func (v *v2Live) follow(conv, session, agentSession, transcript string) {
	if agentSession == "" || !v.rt.OwnsSession(session) {
		return
	}
	rec, ok := v.rt.Record(conv)
	if !ok || followed(rec, agentSession, transcript) {
		return
	}
	if transcript != "" {
		_ = v.rt.Attach(conv, session, transcript, agentSession)
		return
	}
	if v.ses == nil {
		return
	}
	if _, busy := v.finding.LoadOrStore(conv, struct{}{}); busy {
		return
	}
	safego.Go("chat transcript", func() {
		defer v.finding.Delete(conv)
		if found, ok := v.ses.Find(rec.Provider, agentSession); ok && found.TranscriptPath != "" {
			_ = v.rt.Attach(conv, session, found.TranscriptPath, agentSession)
		}
	})
}

// followed says the conversation already reads the transcript of that agent session: the file the hook named, or,
// when it named none, a file called by the session's id (both agents name their transcripts so).
func followed(rec wire.Conversation, agentSession, transcript string) bool {
	if rec.AgentSessionID != agentSession || rec.TranscriptPath == "" {
		return false
	}
	if transcript != "" {
		return rec.TranscriptPath == transcript
	}
	return strings.Contains(filepath.Base(rec.TranscriptPath), agentSession)
}
