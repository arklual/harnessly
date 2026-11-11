package proxy

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/history"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// CountChatListReads counts, for the rest of the test, how many times the chats of a list were read.
func CountChatListReads(t *testing.T) *atomic.Int64 {
	t.Helper()
	var n atomic.Int64
	was := listChats
	listChats = func(ctx context.Context, db *store.DB, userID string, machines []string) ([]history.Chat, error) {
		n.Add(1)
		return was(ctx, db, userID, machines)
	}
	t.Cleanup(func() { listChats = was })
	return &n
}
