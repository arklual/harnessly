package history

import (
	"context"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

func workAt(conv, parent, wt string, at int64, commits ...string) hubproto.ChatWorkEntry {
	return hubproto.ChatWorkEntry{Conv: conv, Parent: parent, Repo: "/r", Worktree: wt, Branch: "agent/x", Base: "aaaaaaa",
		Head: "bbbbbbb", Commits: commits, Remote: "https://gitlab.example.com/g/p.git", Since: 1, At: at, Source: hubproto.ChatWorkLive}
}

// A parent that worked nowhere itself sees the work of the chats it delegated, at any depth and on another machine;
// a chat delegated before the column was there is found by its record; an older record never replaces a newer one.
func TestAParentSeesTheWorkOfItsDelegatedChats(t *testing.T) {
	ctx, db := context.Background(), openDB(t)
	// The child's record names its parent (the chat list as the machine sends it).
	child := conv("tab-child", 1, 10)
	child["parentId"] = "tab-parent"
	if _, err := ApplyFrame(ctx, db, mine, live("convs", 1, upsert("tab-parent", 1, 5),
		j(map[string]any{"type": "conversation_upsert", "conv": child})), now); err != nil {
		t.Fatal(err)
	}
	// An old row: written before parent_id was filled.
	old := conv("tab-old", 1, 11)
	old["parentId"] = "tab-parent"
	if _, err := db.Exec(ctx, `INSERT INTO history_conversations (machine_id, conv_id, owner_id, record, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"m-1", "tab-old", "u-sergey", string(j(old)), 1); err != nil {
		t.Fatal(err)
	}
	if err := ApplyWork(ctx, db, mine, []hubproto.ChatWorkEntry{workAt("tab-child", "tab-parent", "/r/.worktrees/x", 20, "ccccccc")}, now); err != nil {
		t.Fatal(err)
	}
	// The grandchild runs on the second machine; only its work names its parent.
	if err := ApplyWork(ctx, db, second, []hubproto.ChatWorkEntry{workAt("tab-grand", "tab-child", "/s/wt", 30)}, now); err != nil {
		t.Fatal(err)
	}
	if err := ApplyWork(ctx, db, mine, []hubproto.ChatWorkEntry{workAt("tab-old", "", "/r", 15)}, now); err != nil {
		t.Fatal(err)
	}
	// Older than stored: nothing changes. Not a chat's work: skipped.
	stale := workAt("tab-child", "tab-parent", "/r/.worktrees/x", 19)
	stale.Branch = "stale"
	bad := workAt("../x", "", "relative", 99)
	if err := ApplyWork(ctx, db, mine, []hubproto.ChatWorkEntry{stale, bad}, now); err != nil {
		t.Fatal(err)
	}
	// Somebody else's machine writes nothing the person reads.
	if err := ApplyWork(ctx, db, other, []hubproto.ChatWorkEntry{workAt("tab-child", "tab-parent", "/other", 50)}, now); err != nil {
		t.Fatal(err)
	}

	got, err := Work(ctx, db, "u-sergey", "tab-parent", []string{"m-1", "m-2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].Conv != "tab-grand" || got[0].MachineID != "m-2" || got[1].Conv != "tab-child" || got[2].Conv != "tab-old" {
		t.Fatalf("work = %+v", got)
	}
	if got[1].Branch != "agent/x" || len(got[1].Commits) != 1 || got[1].Commits[0] != "ccccccc" || got[1].Parent != "tab-parent" {
		t.Fatalf("child = %+v", got[1])
	}
	// The machines the person reaches bound what is read.
	if got, _ := Work(ctx, db, "u-sergey", "tab-parent", []string{"m-1"}); len(got) != 2 {
		t.Fatalf("one machine: %+v", got)
	}
	// A delegated chat sees its own subtree, not its parent's.
	if got, _ := Work(ctx, db, "u-sergey", "tab-child", []string{"m-1", "m-2"}); len(got) != 2 {
		t.Fatalf("child subtree: %+v", got)
	}
	if got, _ := Work(ctx, db, "u-other", "tab-parent", []string{"m-1", "m-2"}); len(got) != 0 {
		t.Fatalf("another person: %+v", got)
	}
}
