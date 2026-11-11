package main

import (
	"context"
	"path/filepath"
	"time"

	"github.com/ly-solutions/harnessly/internal/chatwork"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// The work of the chats in git (internal/chatwork): the observer follows the chats' feeds off the hub and keeps the
// records in <DataDir>/chatwork.json; the replica socket hands them to the agent, which forwards them to the hub.

func newChatWork(v *v2Live) *chatwork.Observer {
	return chatwork.NewObserver(chatwork.Open(filepath.Join(v.d.DataDir, "chatwork.json")), v.chatOf, v.d.Home)
}

// chatOf is what the observer needs of a chat of this machine.
func (v *v2Live) chatOf(id string) (chatwork.Chat, bool) {
	rec, ok := v.rt.Record(id)
	if !ok {
		return chatwork.Chat{}, false
	}
	return chatFromRecord(rec), true
}

func chatFromRecord(rec wire.Conversation) chatwork.Chat {
	c := chatwork.Chat{ID: rec.ID, Cwd: rec.Cwd, CreatedAt: int64(rec.CreatedAt), Transcript: rec.TranscriptPath}
	if rec.ParentID != nil {
		c.Parent = *rec.ParentID
	}
	return c
}

// serveWork follows the hub's conversation channels until done.
func (v *v2Live) serveWork(done <-chan struct{}) {
	stop := v.hub.Tap(func(ch wire.Channel, _ int64, frame []byte) { v.work.Frame(string(ch), frame) }, func() {})
	defer stop()
	v.work.Run(done)
}

// localChats are the chats of this machine as the observer knows them, with their transcripts.
func (v *v2Live) localChats(ctx context.Context) []chatwork.Chat {
	snap, err := v.rt.Conversations(ctx)
	if err != nil {
		return nil
	}
	out := make([]chatwork.Chat, 0, len(snap.Conversations))
	for _, c := range snap.Conversations {
		if rec, ok := v.rt.Record(c.ID); ok {
			c = rec
		}
		out = append(out, chatFromRecord(c))
	}
	return out
}

// chatWorkScanWait is how long the changes route waits for the transcripts of old chats to be looked through.
const chatWorkScanWait = 20 * time.Second

// chatWork is what the work routes need of the live chats.
type chatWork interface {
	workObserver() *chatwork.Observer
	localChats(ctx context.Context) []chatwork.Chat
}

func (v *v2Live) workObserver() *chatwork.Observer { return v.work }
