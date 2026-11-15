package docs

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

type ring struct{ user, email, key, title, text, link string }

type bellFake struct {
	mu    sync.Mutex
	rings []ring
}

func (b *bellFake) ring(_ context.Context, userID, email, key, title, text, link string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rings = append(b.rings, ring{userID, email, key, title, text, link})
	return true
}

func mailID(secret, email string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(email))
	return "mail:" + hex.EncodeToString(m.Sum(nil))
}

func mentionRig(t *testing.T) (*boardRig, *bellFake, store.User, store.User) {
	t.Helper()
	r := newBoardRig(t)
	b := &bellFake{}
	r.s.bell = b.ring
	ctx := context.Background()
	ann := &store.User{Email: "Ann@Example.com", Name: "Аня"}
	bob := &store.User{Email: "bob@example.com", Name: "Боб"}
	for _, u := range []*store.User{ann, bob} {
		if err := r.s.db.S().CreateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	return r, b, *ann, *bob
}

func TestBoardMentionRingsThePeopleCalled(t *testing.T) {
	r, b, ann, bob := mentionRig(t)
	body := `{"event":"thread.replied","slug":"flow-board","title":"План","thread":{"id":"t1"},
		"message":{"id":"m9","text":"@Аня и @Боб,   гляньте\nкассу, @harnessly помоги","author":"Вера","authorId":"hub:someone","authorKind":"person",
		"mentions":[{"name":"harnessly","agent":true},{"name":"Аня","id":"hub:` + ann.ID + `"},{"name":"Боб","id":"` + mailID("board-secret", "bob@example.com") + `"}]}}`
	if code, out := r.post(t, "board-secret", body); code != http.StatusAccepted {
		t.Fatalf("event: %d %v", code, out)
	}
	if len(b.rings) != 2 {
		t.Fatalf("rings = %+v, want Аня and Боб, not the agent", b.rings)
	}
	got := map[string]ring{}
	for _, x := range b.rings {
		got[x.user] = x
	}
	a, ok := got[ann.ID]
	if !ok || a.email != "ann@example.com" {
		t.Fatalf("Аня by her hub id: %+v", b.rings)
	}
	if a.key != "mention:flow-board:m9" || a.title != "Упоминание на доске «План»" ||
		a.text != "Вера: @Аня и @Боб, гляньте кассу, @harnessly помоги" || a.link != "/apps/harnessly.boards?o=flow-board&th=t1" {
		t.Errorf("notice: %+v", a)
	}
	if _, ok := got[bob.ID]; !ok {
		t.Errorf("Боб by the keyed hash of his e-mail: %+v", b.rings)
	}
}

func TestBoardMentionSkipsTheAuthorStrangersAndOldShapes(t *testing.T) {
	r, b, ann, _ := mentionRig(t)
	post := func(body string) {
		t.Helper()
		if code, out := r.post(t, "board-secret", body); code != http.StatusAccepted {
			t.Fatalf("event: %d %v", code, out)
		}
	}
	// The author calling themself; an id of another secret; an unknown hub id; a name without an id.
	post(`{"event":"thread.created","slug":"flow-board","thread":{"id":"t1"},"message":{"id":"m1","text":"@Аня","author":"Аня","authorId":"hub:` + ann.ID + `","authorKind":"person",
		"mentions":[{"name":"Аня","id":"hub:` + ann.ID + `"},{"name":"Боб","id":"` + mailID("other-secret", "bob@example.com") + `"},{"name":"Кто","id":"hub:nobody"},{"name":"Ким","id":null}]}}`)
	// The boards service before HRN-1469 sent names only.
	post(`{"event":"thread.created","slug":"flow-board","thread":{"id":"t2"},"message":{"id":"m2","text":"@Аня","author":"Боб","authorKind":"person","mentions":["Аня"]}}`)
	// A resolve carries no message to tell of.
	post(`{"event":"thread.resolved","slug":"flow-board","thread":{"id":"t2","resolved":true}}`)
	if len(b.rings) != 0 {
		t.Fatalf("nobody should be told: %+v", b.rings)
	}
	// A switched-off person is not told.
	now := time.Now()
	if _, err := r.s.db.S().Q().Exec(context.Background(), `UPDATE users SET disabled_at = ? WHERE id = ?`, now.UnixMilli(), ann.ID); err != nil {
		t.Fatal(err)
	}
	post(`{"event":"thread.created","slug":"flow-board","thread":{"id":"t3"},"message":{"id":"m3","text":"@Аня","author":"Боб","authorKind":"person","mentions":[{"name":"Аня","id":"hub:` + ann.ID + `"}]}}`)
	if len(b.rings) != 0 {
		t.Fatalf("a switched-off person is told: %+v", b.rings)
	}
}

func TestMentionLineIsShort(t *testing.T) {
	long := strings.Repeat("слово ", 60)
	got := mentionLine(long)
	if r := []rune(got); len(r) > mentionTextMax || !strings.HasSuffix(got, "…") {
		t.Errorf("mentionLine: %d runes %q", len(r), got)
	}
	if mentionLine("  a\n\n b ") != "a b" {
		t.Errorf("mentionLine joins lines")
	}
}
