package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// The agent's turn when a person first opens an app's window in a chat (internal/apps windowevent.go). The
// page's first report of the app is the sign that its window is up. The turn is given the way a message of another
// agent is (convo peers.go): a message in the chat's queue under a client id of its own, with the event's envelope in
// its text. Only an agent at rest gets it; a busy one is not interrupted and the event is dropped.

const (
	// appOpenEventEnv turns the event off when it is "0" (on when unset).
	appOpenEventEnv = "RUNTIME_APP_OPEN_EVENT"
	// appOpenEventEvery is the least time between two events in one chat.
	appOpenEventEvery = time.Minute
	// appOpenClientPrefix starts the client id of every event message.
	appOpenClientPrefix = "app-event-"
)

type appOpenEvents struct {
	mu   sync.Mutex
	last map[string]time.Time
	// now is for tests.
	now func() time.Time
}

var appOpens = &appOpenEvents{}

// due says whether the chat may get an event now, and counts it when it may.
func (e *appOpenEvents) due(conv string) bool {
	now := time.Now()
	if e.now != nil {
		now = e.now()
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.last == nil {
		e.last = map[string]time.Time{}
	}
	if at, ok := e.last[conv]; ok && now.Sub(at) < appOpenEventEvery {
		return false
	}
	e.last[conv] = now
	return true
}

// opened hears the page's report of an app's window in a chat and, the first time for that app in that chat, answers
// with the message that gives the chat's agent its turn; false when there is none to send.
func (e *appOpenEvents) opened(opens *apps.WindowOpens, rec wire.Conversation, name string, r apps.PageReport) (wire.SendCmd, bool) {
	if os.Getenv(appOpenEventEnv) == "0" || apps.IsBuiltin(r.App) {
		return wire.SendCmd{}, false
	}
	if !opens.Claim(rec.ID, r.App) {
		return wire.SendCmd{}, false
	}
	// A busy agent is not interrupted, and one that does not run is not started for a greeting.
	if rec.State.K != wire.StateReady && rec.State.K != wire.StateIdle {
		return wire.SendCmd{}, false
	}
	if !e.due(rec.ID) {
		return wire.SendCmd{}, false
	}
	raw := make([]byte, 8)
	_, _ = rand.Read(raw)
	id := appOpenClientPrefix + hex.EncodeToString(raw)
	return wire.SendCmd{OpID: id, ClientID: id, ConversationID: rec.ID, Text: apps.OpenEvent(r.App, name, apps.WindowScreen(r.Context)),
		AttachmentIDs: []string{}, Mode: wire.ModeQueue, Kind: wire.KindPrompt}, true
}

// send puts the event's message in the chat's queue.
func (e *appOpenEvents) send(chats appChats, cmd wire.SendCmd) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	chats.do(ctx, cmd)
}
