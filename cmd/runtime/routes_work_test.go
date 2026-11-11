package main

import (
	"context"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/chatwork"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

type fakeChatWork struct {
	o     *chatwork.Observer
	chats []chatwork.Chat
}

func (f fakeChatWork) workObserver() *chatwork.Observer           { return f.o }
func (f fakeChatWork) localChats(context.Context) []chatwork.Chat { return f.chats }

// The chat's own records and those of the chats it delegated, at any depth; a stranger's stay out; every chat of the
// subtree has its transcript looked through once.
func TestChatWorkOfHoldsTheDelegatedChats(t *testing.T) {
	chats := []chatwork.Chat{{ID: "tab-p"}, {ID: "tab-c", Parent: "tab-p"}, {ID: "tab-g", Parent: "tab-c"}, {ID: "tab-x"}}
	reg := chatwork.Open("")
	for _, c := range []string{"tab-c", "tab-g", "tab-x"} {
		reg.Put(hubproto.ChatWorkEntry{Conv: c, Repo: "/r", Worktree: "/r/" + c, At: 1})
	}
	o := chatwork.NewObserver(reg, func(id string) (chatwork.Chat, bool) {
		for _, c := range chats {
			if c.ID == id {
				return c, true
			}
		}
		return chatwork.Chat{}, false
	}, "")
	done := make(chan struct{})
	defer close(done)
	go o.Run(done)
	got, complete := chatWorkOf(context.Background(), fakeChatWork{o: o, chats: chats}, "tab-p", 10*time.Second)
	if !complete || len(got) != 2 {
		t.Fatalf("complete=%v work=%+v", complete, got)
	}
	for _, id := range []string{"tab-p", "tab-c", "tab-g"} {
		if !reg.Scanned(id) {
			t.Errorf("%s not scanned", id)
		}
	}
	if reg.Scanned("tab-x") {
		t.Error("a chat outside the subtree was scanned")
	}
}

// The review writes only into a worktree of the chat's work: the delegated chats' count, a stranger's or any other
// folder does not.
func TestReviewWritesOnlyIntoTheChatsWorktrees(t *testing.T) {
	chats := []chatwork.Chat{{ID: "tab-p"}, {ID: "tab-c", Parent: "tab-p"}, {ID: "tab-x"}}
	reg := chatwork.Open("")
	for _, c := range []string{"tab-p", "tab-c", "tab-x"} {
		reg.Put(hubproto.ChatWorkEntry{Conv: c, Repo: "/r", Worktree: "/r/" + c, At: 1})
	}
	o := chatwork.NewObserver(reg, func(id string) (chatwork.Chat, bool) {
		for _, c := range chats {
			if c.ID == id {
				return c, true
			}
		}
		return chatwork.Chat{}, false
	}, "")
	done := make(chan struct{})
	defer close(done)
	go o.Run(done)
	cw := fakeChatWork{o: o, chats: chats}
	ctx := context.Background()
	for root, want := range map[string]bool{"/r/tab-p": true, "/r/tab-c/": true, "/r/tab-x": false, "/r": false, "/etc": false, "r/tab-p": false} {
		if got := isChatWorktree(ctx, cw, "tab-p", root); got != want {
			t.Errorf("%s: %v", root, got)
		}
	}
	if isChatWorktree(ctx, cw, "bad id!", "/r/tab-p") || isChatWorktree(ctx, nil, "tab-p", "/r/tab-p") {
		t.Error("a bad chat id or no registry")
	}
}
