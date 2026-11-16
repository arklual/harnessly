package main

import (
	"context"
	"fmt"

	"github.com/ly-solutions/harnessly/internal/journal"
	"github.com/ly-solutions/harnessly/internal/memsync"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/limits"
	"github.com/ly-solutions/harnessly/internal/providers"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
	"github.com/ly-solutions/harnessly/internal/voice/consent"
	"github.com/ly-solutions/harnessly/internal/voice/realtime"
)

// The voice mode (internal/voice/realtime): a spoken conversation in real time. The subscription route is
// unofficial and off until the person turns it on in Settings → ChatGPT; it is made with this machine's Codex sign-in.
// Without it, a call takes the public Realtime API with the machine's OpenAI key, when there is one.
// Like the ChatGPT integration, only the person's own page gets in, never an agent's cli-token.
//
//	GET    <platform>/voice/live                     state
//	PUT    <platform>/voice/live/settings            {subscription, voice, model}
//	POST   <platform>/voice/live/calls               {sdp, mode, voice, model, context} -> {id, sdp, route, model}
//	GET    <platform>/voice/live/calls/<id>/events   text/event-stream
//	POST   <platform>/voice/live/calls/<id>/say      {text, task?, speak?}
//	DELETE <platform>/voice/live/calls/<id>

var (
	voiceLiveMu  sync.Mutex
	voiceLiveFor = map[string]*realtime.Service{}
)

func voiceLiveService(d deps) *realtime.Service {
	voiceLiveMu.Lock()
	defer voiceLiveMu.Unlock()
	if s := voiceLiveFor[d.DataDir]; s != nil {
		return s
	}
	s := realtime.New(realtime.Config{
		Dir:       filepath.Join(d.DataDir, "voice-live"),
		Tokens:    codexTokens(d.Home),
		Key:       openAIKey(d.DataDir),
		Endpoints: voiceLiveEndpoints(os.Getenv),
		Handoff:   voiceHandoff,
		Notes:     voiceNotes(d),
		Primed:    voicePrimed,
		Relayed:   voiceRelayed,
		Live:      voiceLiveFlag(d),
		// The team's deployment turns the subscription route on for everyone who never turned it (15.11, the owner's
		// decision); any other installation keeps it off until its person turns it.
		SubscriptionOn: os.Getenv("RUNTIME_VOICE_SUBSCRIPTION") == "on",
	})
	voiceLiveFor[d.DataDir] = s
	return s
}

// codexTokens reads the machine's Codex sign-in each time it is asked: Codex refreshes it itself, nothing rotates here.
func codexTokens(home string) realtime.TokenSource {
	return realtime.TokenFunc(func() (realtime.Token, bool) {
		l := limits.ReadCodexLogin(limits.CodexHome(os.Getenv("CODEX_HOME"), home))
		if l == nil {
			return realtime.Token{}, false
		}
		return realtime.Token{Access: l.AccessToken, Account: l.AccountID}, true
	})
}

// openAIKey is the OpenAI key the machine keeps for speech (Settings → Voice input): the key route's, when the
// subscription route is off or Codex is not signed in.
func openAIKey(dataDir string) realtime.KeyFunc {
	return func() (string, bool) {
		k, err := voiceStore(dataDir).Key(providers.SpeechOpenAI)
		return k, err == nil && k != ""
	}
}

// voiceLiveEndpoints are OpenAI's own, or a test's fake on this machine (RUNTIME_VOICE_CALL_URL and
// RUNTIME_VOICE_LIVE_URL, loopback only: a token never goes to another host).
func voiceLiveEndpoints(getenv func(string) string) realtime.Endpoints {
	call, live := strings.TrimSpace(getenv("RUNTIME_VOICE_CALL_URL")), strings.TrimSpace(getenv("RUNTIME_VOICE_LIVE_URL"))
	if call == "" && live == "" {
		return realtime.DefaultEndpoints()
	}
	ep := realtime.DefaultEndpoints()
	if !chatgptEndpointOK(call) || !chatgptEndpointOK(strings.Replace(strings.Replace(live, "ws://", "http://", 1), "wss://", "https://", 1)) {
		agent.Log("warn", "voice: RUNTIME_VOICE_CALL_URL and RUNTIME_VOICE_LIVE_URL must both be loopback addresses; OpenAI's own are used")
		return realtime.DefaultEndpoints()
	}
	ep.Call, ep.Live = call, live
	return ep
}

func voiceLiveRoutes(d deps) []gate.Route {
	guard := func(w http.ResponseWriter, r *http.Request) bool {
		if sameorigin.Refuse(w, r) {
			return false
		}
		if !d.SessionOK(r) {
			secretJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
			return false
		}
		return true
	}
	var routes []gate.Route
	for _, base := range []string{platformAPI + "/voice/live", "/agents" + platformAPI + "/voice/live"} {
		var h http.Handler
		var once sync.Once
		routes = append(routes, gate.Route{
			Match: func(r *http.Request) bool { return r.URL.Path == base || strings.HasPrefix(r.URL.Path, base+"/") },
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				once.Do(func() { h = voiceLiveService(d).Handler(base, guard) })
				h.ServeHTTP(w, r)
			}),
		})
	}
	return routes
}

// voiceHandoff writes each task the voice handed over, and each result told back for it, into the journal
// (voice.handoff): a voice that promised work nobody started shows as a call without these lines.
// The words themselves stay out of the journal: what a person said is theirs, its length shows the handoff happened.
func voiceHandoff(step, mode, task, text string) {
	journal.Add(journal.Entry{At: time.Now().UnixMilli(), Level: journal.Info, Source: journal.SourceRuntime,
		Event: "voice.handoff", Tool: mode + "/" + step, Trace: task, Text: strconv.Itoa(len([]rune(text))) + " chars"})
}

// voiceNotes are the person's agents' notes about them in the project of the chat a call was opened from: the
// descriptions of their Claude Code memories of type user or feedback (realtime.MemoryNotes).
func voiceNotes(d deps) func(conv string) []string {
	projects := filepath.Join(env("CLAUDE_CONFIG_DIR", filepath.Join(d.Home, ".claude")), "projects")
	return func(conv string) []string {
		if d.ChatWork == nil {
			return nil
		}
		cw := d.ChatWork()
		if cw == nil {
			return nil
		}
		cwd, ok := cw.chatCwd(conv)
		if !ok || cwd == "" {
			return nil
		}
		return realtime.MemoryNotes(memsync.MemoryDir(projects, cwd))
	}
}

// voicePrimed journals how much a call started knowing (voice.primer): the sizes, never the words.
func voicePrimed(route string, sizes map[string]int) {
	journal.Add(journal.Entry{At: time.Now().UnixMilli(), Level: journal.Info, Source: journal.SourceRuntime,
		Event: "voice.primer", Tool: route, Text: fmt.Sprintf("about %d chars, notes %d, screen %d chars",
			sizes["about"], sizes["notes"], sizes["screen"])})
}

// voiceRelayed journals how the machine's connection of a call over the machine goes (voice.relay), in the scope of
// the call's start: with the chat's id the chat's agent sees the lines, as it sees the page's voice.connect-timeout.
func voiceRelayed(ctx context.Context, level, text string) {
	l := journal.Info
	if level == "warn" {
		l = journal.Warn
	}
	journal.Add(journal.Fill(ctx, journal.Entry{At: time.Now().UnixMilli(), Level: l, Source: journal.SourceRuntime,
		Event: "voice.relay", Text: text}))
}

// voiceLiveFlag marks the chat a call is opened from while it lasts: a file named by the agent's tmux session under
// <data>/voice-live/live, which the agent's PreToolUse hook (`harnessly hook voice-guard`) looks for to ask the person
// before a call that reaches outside (internal/voice/consent).
func voiceLiveFlag(d deps) func(conv string, on bool) {
	dir := filepath.Join(d.DataDir, consent.LiveDir)
	return func(conv string, on bool) {
		if d.ChatWork == nil {
			return
		}
		cw := d.ChatWork()
		if cw == nil {
			return
		}
		session, ok := cw.chatSession(conv)
		if !ok {
			return
		}
		if err := consent.Mark(dir, session, on); err != nil {
			agent.Log("warn", "voice: the live mark of a chat: "+err.Error())
		}
	}
}
