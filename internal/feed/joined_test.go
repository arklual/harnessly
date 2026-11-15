package feed

import (
	"encoding/json"
	"testing"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// Claude read two messages of its queue at once: one `queued_command` holds both, a line apart. The entry
// carries the client id of the one it was matched to; the other is found in it too — by a restart or a Повторить —
// and a message that only shares words with a line is not.
func TestAJoinedQueuedCommandIsFoundForEachOfItsMessages(t *testing.T) {
	a := "В смысле, подскажи пожалуйста, почему голосовой режим у нас не будет работать по подписке?"
	b := "А ещё переход между досками и документами какой-то супердёрганый, хотя интерфейс общий."
	prompt, _ := json.Marshal(a + "\n" + b)
	text := conversation(1) +
		`{"type":"attachment","uuid":"00000000-0000-4000-8000-0000000000dd","timestamp":"2026-10-12T10:00:30.000Z","attachment":{"type":"queued_command","commandMode":"prompt","prompt":` + string(prompt) + `}}` + "\n"
	f := New("c1", wire.ProviderClaude, &ClaudeSource{Path: write(t, text)})
	u := f.Load()
	validate(t, f)
	var entry string
	for _, s := range u.Signals {
		if s.K == SignalUser {
			entry = s.EntryID
		}
	}
	if entry == "" {
		t.Fatalf("signals: %+v", u.Signals)
	}
	f.SetClientID(entry, "m-a")
	for _, msg := range []string{a, b} {
		if id, ok := f.FindUserText(0, 0, msg); !ok || id != entry {
			t.Fatalf("%q: found %q %v, want %q", msg, id, ok, entry)
		}
	}
	if id, ok := f.FindUserText(0, 0, "А ещё переход между досками и документами какой-то супердёрганый"); ok {
		t.Fatalf("a part of a line was taken for a message: %q", id)
	}
}
