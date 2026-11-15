package realtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeMemory(t *testing.T, dir, name, kind, desc, body string) {
	t.Helper()
	raw := "---\nname: " + name + "\ndescription: " + desc + "\nmetadata:\n  type: " + kind + "\n---\n\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestMemoryNotesAreTheDescriptionsOfThePersonsOwn(t *testing.T) {
	dir := t.TempDir()
	writeMemory(t, dir, "feedback_short", "feedback", "Отвечать коротко, 2–4 строки", "секретное тело заметки")
	writeMemory(t, dir, "user_role", "user", "Пользователь — владелец продукта", "тело")
	writeMemory(t, dir, "project_x", "project", "Проект X идёт", "тело проекта")
	writeMemory(t, dir, "reference_y", "reference", "Ссылка на дашборд", "url")
	_ = os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte("- [a](a.md) — index\n"), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "loose.md"), []byte("no front matter\ntype: user\n"), 0o600)

	got := MemoryNotes(dir)
	if len(got) != 2 || got[0] != "Отвечать коротко, 2–4 строки" || got[1] != "Пользователь — владелец продукта" {
		t.Fatalf("notes %q", got)
	}
	if MemoryNotes(filepath.Join(dir, "none")) != nil {
		t.Fatal("a missing folder gives nothing")
	}
}

func TestPrimerIsOneDeveloperMessageAndItsSizesHoldNoWords(t *testing.T) {
	if (Primer{}).Items() != nil {
		t.Fatal("nothing to start with gives no items")
	}
	p := Primer{About: "Зовите меня Аня", Notes: []string{"Отвечать по-русски"}, Screen: "Доска «Roadmap»: Backlog, Doing, Done"}
	items := p.Items()
	if len(items) != 1 || items[0]["role"] != "developer" {
		t.Fatalf("items %v", items)
	}
	text := p.Text()
	for _, want := range []string{"Зовите меня Аня", "- Отвечать по-русски", "Roadmap"} {
		if !strings.Contains(text, want) {
			t.Fatalf("%q not in %q", want, text)
		}
	}
	sizes := p.Sizes()
	if sizes["about"] != 15 || sizes["notes"] != 1 || sizes["screen"] == 0 {
		t.Fatalf("sizes %v", sizes)
	}
	long := Primer{Screen: strings.Repeat("я", maxScreen)}
	if l := len(long.Text()); l > maxScreen+200 {
		t.Fatalf("screen not clipped: %d", l)
	}
}

func TestACallStartsKnowingThePersonAndTheScreen(t *testing.T) {
	f, srv := newFake(t)
	var primed map[string]int
	s := New(Config{
		Dir:       t.TempDir(),
		Tokens:    TokenFunc(func() (Token, bool) { return Token{Access: "tok-secret"}, true }),
		Endpoints: Endpoints{Call: srv.URL + "/codex", Live: "ws" + strings.TrimPrefix(srv.URL, "http") + "/live"},
		Notes: func(conv string) []string {
			if conv != "conv-1" {
				return nil
			}
			return []string{"Отвечать коротко"}
		},
		Primed: func(_ string, sizes map[string]int) { primed = sizes },
	})
	_ = s.SaveSettings(Settings{Subscription: true, About: "Я Аня", Delivery: DeliveryResult})
	if _, err := s.Start(context.Background(), CallInput{SDP: "v=0", Conv: "conv-1", Screen: "Доска «План»"}); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	sess := f.body["session"].(map[string]any)
	f.mu.Unlock()
	items, _ := sess["initial_items"].([]any)
	if len(items) != 1 {
		t.Fatalf("initial items %v", sess["initial_items"])
	}
	text := items[0].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	for _, want := range []string{"Я Аня", "Отвечать коротко", "Доска «План»"} {
		if !strings.Contains(text, want) {
			t.Fatalf("%q not in %q", want, text)
		}
	}
	instr := sess["instructions"].(string)
	if strings.Contains(instr, "Я Аня") || !strings.Contains(instr, "results only") {
		t.Fatalf("instructions carry the delivery, not the person: %q", instr)
	}
	if sess["delegation"].(map[string]any)["ack_filler"] != true {
		t.Fatalf("delegation %v", sess["delegation"])
	}
	if primed["about"] != 5 || primed["notes"] != 1 {
		t.Fatalf("primed %v", primed)
	}
	// Notes off: the person's agents' notes stay out.
	off := false
	_ = s.SaveSettings(Settings{Subscription: true, Notes: &off})
	if _, err := s.Start(context.Background(), CallInput{SDP: "v=0", Conv: "conv-1"}); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	_, has := f.body["session"].(map[string]any)["initial_items"]
	f.mu.Unlock()
	if has || primed["notes"] != 0 {
		t.Fatalf("notes off still sent: %v", primed)
	}
}

func TestInstructionsHandOverFirstAndFollowTheStyle(t *testing.T) {
	a := Instructions(ModeAgent, "")
	for _, want := range []string{"The one rule above all", "never hand over a fragment", "Delivery: you tell the work", "Speed: instant"} {
		if !strings.Contains(a, want) {
			t.Fatalf("%q missing", want)
		}
	}
	if strings.Index(a, "The one rule above all") > strings.Index(a, "You are only its ears and mouth") {
		t.Fatal("the rule of handing over comes before what the mode does")
	}
	deep := Instructions(ModeAgent, "", Style{Delivery: DeliveryResult, Effort: "high"})
	if !strings.Contains(deep, "results only") || !strings.Contains(deep, "Speed: thorough") {
		t.Fatalf("style ignored")
	}
	if strings.Contains(Instructions(ModeChat, ""), "The one rule above all") {
		t.Fatal("a ChatGPT chat answers talk itself")
	}
	if reasoningOf["instant"] != "minimal" || reasoningOf["max"] != "xhigh" || len(Efforts) != 5 {
		t.Fatal("five steps, Instant to Pro")
	}
}

func TestDigestReadsAndAnswersItself(t *testing.T) {
	in := Instructions(ModeDigest, "главная")
	if !strings.Contains(in, `"READ:"`) || !strings.Contains(in, "Продолжить?") || strings.Contains(in, "The one rule above all") {
		t.Fatalf("the digest's reader reads what it is sent and does not route: %s", in)
	}
	if !strings.HasSuffix(in, "главная") {
		t.Fatal("where it was opened ends the instructions")
	}
	s := keySession("gpt-realtime-2.1", "marin", ModeDigest, in)
	if _, ok := s["tools"]; ok || strings.Contains(s["instructions"].(string), "hand_over") {
		t.Fatal("the digest's reader has nothing to hand over with")
	}
	if a := keySession("gpt-realtime-2.1", "marin", ModeAgent, "x"); a["tools"] == nil || !strings.Contains(a["instructions"].(string), "hand_over") {
		t.Fatal("the agent mode still hands over")
	}
}
