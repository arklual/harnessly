// Package convo owns the conversations of a runtime: one actor per conversation writes its record, agent state,
// queue, messages, gates, background tasks and seenAt, and every change it makes goes out as a wire event.
//
// This file is the seam between the actors and the protocol: internal/wire serves the socket, the commands and the
// pages on a Service, and the actors publish through a Publisher. Either side can be faked against it.
package convo

import (
	"context"
	"errors"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// Service is what the protocol asks of the conversations. Every method is safe to call from any goroutine; the
// answers are copies the caller may keep.
type Service interface {
	// Conversations is the data of a convs snapshot: every conversation not deleted, pinned ones first, then by
	// lastActivityAt, newest first.
	Conversations(ctx context.Context) (wire.ConvsSnapshot, error)

	// Conversation is the data of a whole conv:<id> snapshot. ErrNotFound for a conversation it does not have.
	Conversation(ctx context.Context, id string) (wire.ConvSnapshot, error)

	// ConversationAfter is the data of a conv:<id> snapshot with mode "after": the entries after afterEntryID and the
	// current queue, messages, gates, background and now. ErrUnknownCursor when the entry is not in the transcript;
	// the caller then sends a whole snapshot.
	ConversationAfter(ctx context.Context, id, afterEntryID string) (wire.ConvSnapshot, error)

	// Turns is the page of turns before beforeTurnID ("" from the end), at most limit, oldest first, with their
	// answer entries.
	Turns(ctx context.Context, id, beforeTurnID string, limit int) (wire.TurnsPage, error)

	// TurnEntries is every entry of one turn, for a turn opened in the feed.
	TurnEntries(ctx context.Context, id, turnID string) (wire.EntriesPage, error)

	// EntryOutput is the whole output of a tool entry.
	EntryOutput(ctx context.Context, id, entryID string) (wire.OutputPage, error)

	// Do runs a checked command for a device and answers it. The answer of an opId is kept for 24 hours: the same
	// opId with the same body gets it again, with another body refused{conflict}. An accepted or queued answer
	// names the channel and seq from which its effect shows; Do returns once that event is published, and always
	// within wire.Limits.CmdBudgetMs: what does not finish by then answers refused{timeout} and is safe to retry.
	Do(ctx context.Context, device string, cmd wire.Command) wire.CmdResult
}

// Publisher takes the events of the actors in order and gives each its seq on its channel.
type Publisher interface {
	Publish(ch wire.Channel, e wire.Event) (seq int64)
}

var (
	// ErrNotFound is a conversation, turn or entry the runtime does not have.
	ErrNotFound = errors.New("convo: not found")
	// ErrUnknownCursor is an afterEntryId that is not in the transcript.
	ErrUnknownCursor = errors.New("convo: unknown cursor")
	// ErrBusy is a conversation whose mailbox stayed full: its actor is stuck, and the message was not taken.
	ErrBusy = errors.New("convo: the conversation's mailbox is full")
)
