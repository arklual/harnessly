package history

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func note(id, path string, from int) ReviewNote {
	return ReviewNote{ID: id, Path: path, Side: "after", From: from, To: from + 1, Lines: "a\nb", Text: "rename it",
		State: ReviewDraft, Worktree: "/r/.worktrees/x", Base: "abc1234"}
}

// A person's notes on a chat are kept, changed, read by file and line, and forgotten; another person and another chat
// read none of them.
func TestReviewNotesAreThePersonsOwn(t *testing.T) {
	ctx, db := context.Background(), openDB(t)
	for _, n := range []ReviewNote{note("n2", "b.go", 4), note("n1", "a.go", 9), note("n3", "a.go", 2)} {
		if _, err := PutReviewNote(ctx, db, "u-sergey", "tab-1", n, 100); err != nil {
			t.Fatal(err)
		}
	}
	changed := note("n1", "a.go", 9)
	changed.Text = "use the helper"
	changed.State = ReviewSent
	kept, err := PutReviewNote(ctx, db, "u-sergey", "tab-1", changed, 200)
	if err != nil || kept.CreatedAt != 200 || kept.UpdatedAt != 200 {
		t.Fatalf("put = %+v, %v", kept, err)
	}
	got, err := ReviewNotes(ctx, db, "u-sergey", "tab-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].ID != "n3" || got[1].ID != "n1" || got[1].Text != "use the helper" || got[2].ID != "n2" {
		t.Fatalf("notes = %+v", got)
	}
	for _, other := range [][2]string{{"u-other", "tab-1"}, {"u-sergey", "tab-2"}} {
		if got, _ := ReviewNotes(ctx, db, other[0], other[1]); len(got) != 0 {
			t.Fatalf("%v reads %+v", other, got)
		}
	}
	if err := DeleteReviewNote(ctx, db, "u-sergey", "tab-1", "n2"); err != nil {
		t.Fatal(err)
	}
	if got, _ := ReviewNotes(ctx, db, "u-sergey", "tab-1"); len(got) != 2 {
		t.Fatalf("after delete = %+v", got)
	}
}

func TestABadNoteIsNotKept(t *testing.T) {
	ctx, db := context.Background(), openDB(t)
	bad := []func(n *ReviewNote){
		func(n *ReviewNote) { n.ID = "../x" },
		func(n *ReviewNote) { n.Path = "" },
		func(n *ReviewNote) { n.Side = "left" },
		func(n *ReviewNote) { n.From, n.To = 5, 4 },
		func(n *ReviewNote) { n.Text = "  " },
		func(n *ReviewNote) { n.State = "lost" },
		func(n *ReviewNote) { n.Base = "not a sha" },
		func(n *ReviewNote) { n.Text = string(make([]byte, reviewTextMax+1)) },
	}
	for i, spoil := range bad {
		n := note("n", "a.go", 1)
		spoil(&n)
		if _, err := PutReviewNote(ctx, db, "u-sergey", "tab-1", n, 1); !errors.Is(err, ErrBadNote) {
			t.Errorf("case %d: err = %v", i, err)
		}
	}
	if _, err := PutReviewNote(ctx, db, "u-sergey", "not-a-chat", note("n", "a.go", 1), 1); !errors.Is(err, ErrBadNote) {
		t.Errorf("bad chat: err = %v", err)
	}
}

func TestAChatKeepsAtMostSoManyNotes(t *testing.T) {
	ctx, db := context.Background(), openDB(t)
	for i := 0; i < ReviewNotesMax; i++ {
		if _, err := PutReviewNote(ctx, db, "u-sergey", "tab-1", note(fmt.Sprintf("n%d", i), "a.go", i+1), 1); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := PutReviewNote(ctx, db, "u-sergey", "tab-1", note("extra", "a.go", 1), 1); !errors.Is(err, ErrTooManyNotes) {
		t.Fatalf("err = %v", err)
	}
	// Changing one that is kept is still fine.
	if _, err := PutReviewNote(ctx, db, "u-sergey", "tab-1", note("n0", "a.go", 3), 2); err != nil {
		t.Fatal(err)
	}
}
