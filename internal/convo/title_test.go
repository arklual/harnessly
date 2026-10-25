package convo

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/sessionmeta"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// named opens the rig's chat as v2 makes a new one: named by the person's first message.
func (r *rig) named(title string) {
	r.t.Helper()
	r.open()
	if err := r.rt.call(context.Background(), r.id, func(c *conv, now time.Time) {
		c.rec.Title, c.rec.TitleSource = title, wire.TitleFromFirstMessage
	}); err != nil {
		r.t.Fatal(err)
	}
}

func TestTheAgentsTitleReplacesTheFirstMessageAndTheSidebarHearsOfIt(t *testing.T) {
	r := newRig(t)
	r.rt.d.Meta = &sessionmeta.Reader{}
	r.named("Привет. Слушай, а у меня есть идея, как модернизировать…")
	r.settle(wire.StateReady)
	r.user("Привет. Слушай, а у меня есть идея, как модернизировать дизайн")
	r.say("Расскажи подробнее")
	r.tick(metaEvery)
	if rec := r.record(); rec.TitleSource != wire.TitleFromFirstMessage {
		t.Fatalf("no title written yet, the first message stays: %q %q", rec.Title, rec.TitleSource)
	}
	upserts := len(r.events(wire.EvConversationUpsert))
	r.raw(`{"type":"ai-title","aiTitle":"Редизайн дашборда","sessionId":"s"}`)
	r.tick(metaEvery)
	rec := r.record()
	if rec.Title != "Редизайн дашборда" || rec.TitleSource != wire.TitleFromAgent {
		t.Fatalf("the agent's title: %q %q", rec.Title, rec.TitleSource)
	}
	if n := len(r.events(wire.EvConversationUpsert)); n != upserts+1 {
		t.Fatalf("one upsert tells the sidebar, tabs and header: %d -> %d", upserts, n)
	}
	// The agent rewrites its title as the talk goes on; the person's own rename in Claude (/rename) goes before it.
	r.raw(`{"type":"ai-title","aiTitle":"Редизайн дашборда организатора","sessionId":"s"}`)
	r.tick(metaEvery)
	if got := r.record().Title; got != "Редизайн дашборда организатора" {
		t.Fatalf("a newer ai-title: %q", got)
	}
	r.raw(`{"type":"custom-title","customTitle":"dash","sessionId":"s"}`)
	r.tick(metaEvery)
	if got := r.record().Title; got != "dash" {
		t.Fatalf("custom-title: %q", got)
	}
}

func TestATitleTheUserTypedWinsOverTheAgents(t *testing.T) {
	r := newRig(t)
	r.rt.d.Meta = &sessionmeta.Reader{}
	r.named("first words")
	r.settle(wire.StateReady)
	r.user("first words")
	r.say("ok")
	r.raw(`{"type":"ai-title","aiTitle":"Agent's title","sessionId":"s"}`)
	r.tick(metaEvery)
	if got := r.record().Title; got != "Agent's title" {
		t.Fatalf("the agent names it first: %q", got)
	}
	r.do(wire.ConversationRenameCmd{OpID: "ren", ID: r.id, Title: "Моё имя"})
	r.raw(`{"type":"ai-title","aiTitle":"Agent's newer title","sessionId":"s"}`)
	r.raw(`{"type":"custom-title","customTitle":"claude rename","sessionId":"s"}`)
	r.tick(metaEvery)
	rec := r.record()
	if rec.Title != "Моё имя" || rec.TitleSource != wire.TitleFromUser {
		t.Fatalf("the person's rename: %q %q", rec.Title, rec.TitleSource)
	}
	// And it stays through a restart of the runtime.
	r.rt.Stop()
	r.rt = r.runtime()
	r.rt.d.Meta = &sessionmeta.Reader{}
	r.rt.Start(context.Background())
	r.tick(metaEvery)
	if rec := r.record(); rec.Title != "Моё имя" || rec.TitleSource != wire.TitleFromUser {
		t.Fatalf("after a restart: %q %q", rec.Title, rec.TitleSource)
	}
}

func TestAChatTheAgentNamedNotIsNamedOnceFromItsFirstExchange(t *testing.T) {
	r := newRig(t)
	var asked [][2]string
	r.rt.d.Titler = func(p wire.Provider, message, answer string) (string, error) {
		asked = append(asked, [2]string{message, answer})
		return "Доступ к dev-машине", nil
	}
	r.named("Привет, дай доступ")
	r.settle(wire.StateReady)
	r.user("Привет, дай доступ к dev-машине")
	r.say("Выдал ключ")
	r.turnEnd()
	r.tick(time.Second)
	if len(asked) != 0 {
		t.Fatalf("the agent has its time to write a title: %v", asked)
	}
	r.tick(titleWait)
	if len(asked) != 1 || asked[0][0] != "Привет, дай доступ к dev-машине" || asked[0][1] != "Выдал ключ" {
		t.Fatalf("asked: %v", asked)
	}
	rec := r.record()
	if rec.Title != "Доступ к dev-машине" || rec.TitleSource != wire.TitleFromAgent {
		t.Fatalf("made title: %q %q", rec.Title, rec.TitleSource)
	}
	r.user("ещё")
	r.say("да")
	r.turnEnd()
	r.tick(titleWait * 2)
	if len(asked) != 1 {
		t.Fatalf("once: %v", asked)
	}
}

func TestNoTitleIsMadeWhenTheAgentOrThePersonNamedTheChatMeanwhile(t *testing.T) {
	for _, who := range []string{"agent", "person"} {
		t.Run(who, func(t *testing.T) {
			r := newRig(t)
			r.rt.d.Meta = &sessionmeta.Reader{}
			calls := 0
			r.rt.d.Titler = func(wire.Provider, string, string) (string, error) { calls++; return "made", nil }
			r.named("first")
			r.settle(wire.StateReady)
			r.user("first")
			r.say("answer")
			r.turnEnd()
			r.tick(time.Second)
			if who == "agent" {
				r.raw(`{"type":"ai-title","aiTitle":"Agent","sessionId":"s"}`)
			} else {
				r.do(wire.ConversationRenameCmd{OpID: "ren", ID: r.id, Title: "Mine"})
			}
			r.tick(titleWait * 2)
			r.tick(metaEvery)
			r.tick(metaEvery)
			rec := r.record()
			want := map[string]string{"agent": "Agent", "person": "Mine"}[who]
			if calls != 0 || rec.Title != want {
				t.Fatalf("calls %d, title %q", calls, rec.Title)
			}
		})
	}
}

func TestATitleThatCouldNotBeMadeLeavesTheFirstMessage(t *testing.T) {
	r := newRig(t)
	r.rt.d.Titler = func(wire.Provider, string, string) (string, error) { return "", errors.New("no claude") }
	r.named("first")
	r.settle(wire.StateReady)
	r.user("first")
	r.say("answer")
	r.turnEnd()
	r.tick(titleWait + time.Second)
	if rec := r.record(); rec.Title != "first" || rec.TitleSource != wire.TitleFromFirstMessage {
		t.Fatalf("%q %q", rec.Title, rec.TitleSource)
	}
}

func TestAMessageOfNotesIsNamedByItsComment(t *testing.T) {
	note := func(lines string) string {
		return "<annotation n=\"1\" url=\"/a/acme-post-4x5/Main.dc.html\" target=\"port:4471\">\n" + lines + "\n</annotation>"
	}
	for text, want := range map[string]string{
		note("element: span.badge — «СКОРО»\ncomment: Сделай плашку крупнее"):                           "Сделай плашку крупнее",
		note("element: span.badge — «СКОРО»") + "\n\n" + note("element: h1\ncomment: Заголовок короче"): "Заголовок короче",
		note("element: span.badge — «СКОРО»"):                                                           "span.badge — «СКОРО»",
		"Поправь отступы\n\n" + note("element: h1\ncomment: x"):                                         "Поправь отступы",
		"Просто вопрос": "Просто вопрос",
	} {
		if got := titleOf(firstMessageTitle(text)); got != want {
			t.Errorf("%q: %q, want %q", text, got, want)
		}
	}
}

func TestRetitleNamesTheChatAnewFromItsFirstMessageAndLatestAnswer(t *testing.T) {
	r := newRig(t)
	var asked [][2]string
	r.rt.d.Titler = func(_ wire.Provider, message, answer string) (string, error) {
		asked = append(asked, [2]string{message, answer})
		return "Платёжная форма", nil
	}
	r.named("first")
	r.settle(wire.StateReady)
	r.user("Сделай форму оплаты")
	r.say("Форма готова")
	r.user("Теперь возвраты")
	r.say("Возвраты добавлены")
	r.turnEnd()
	r.tick(time.Second)
	r.do(wire.ConversationRenameCmd{OpID: "ren", ID: r.id, Title: "Моё имя"})
	res := r.do(wire.ConversationRetitleCmd{OpID: "rt1", ID: r.id})
	if res.R != wire.ResultAccepted {
		t.Fatalf("result: %+v", res)
	}
	if len(asked) != 1 || asked[0][0] != "Сделай форму оплаты" || asked[0][1] != "Возвраты добавлены" {
		t.Fatalf("asked: %v", asked)
	}
	// The person asked, so even a title they typed gives way.
	if rec := r.record(); rec.Title != "Платёжная форма" || rec.TitleSource != wire.TitleFromAgent {
		t.Fatalf("%q %q", rec.Title, rec.TitleSource)
	}
}

func TestRetitleRefusesWhatCannotBeNamed(t *testing.T) {
	r := newRig(t)
	r.rt.d.Titler = func(wire.Provider, string, string) (string, error) { return "", errors.New("no claude") }
	r.named("first")
	r.settle(wire.StateReady)
	if res := r.do(wire.ConversationRetitleCmd{OpID: "rt0", ID: r.id}); res.R != wire.ResultRefused || res.Code != wire.RefuseBadRequest {
		t.Fatalf("nothing to name it from: %+v", res)
	}
	r.user("ask")
	r.say("answer")
	r.turnEnd()
	r.tick(time.Second)
	if res := r.do(wire.ConversationRetitleCmd{OpID: "rt1", ID: r.id}); res.R != wire.ResultRefused || res.Code != wire.RefuseInternal {
		t.Fatalf("no title made: %+v", res)
	}
	if rec := r.record(); rec.Title != "first" {
		t.Fatalf("the title stays: %q", rec.Title)
	}
}

func TestAConversationOfALaunchWorksInAWorktreeOfItsOwn(t *testing.T) {
	r := newRig(t)
	var made [][2]string
	r.rt.d.Worktree = func(cwd, name string) (string, error) {
		made = append(made, [2]string{cwd, name})
		return cwd + "/.worktrees/" + name, nil
	}
	launch := &wire.Launch{ID: "l1", Prompt: "Сделай форму"}
	res := r.do(wire.ConversationCreateCmd{OpID: "c1", Cwd: "/w", Provider: wire.ProviderClaude,
		Model: &wire.Model{ID: "opus"}, Worktree: true, Launch: launch})
	if res.R != wire.ResultAccepted || res.ID == nil {
		t.Fatalf("%+v", res)
	}
	if len(made) != 1 || made[0][0] != "/w" || !strings.HasPrefix(made[0][1], "l1-opus-") {
		t.Fatalf("made: %v", made)
	}
	r.id = *res.ID
	rec := r.record()
	if rec.Cwd != "/w/.worktrees/"+made[0][1] || rec.Launch == nil || rec.Launch.ID != "l1" {
		t.Fatalf("record: %q %+v", rec.Cwd, rec.Launch)
	}
	r.rt.d.Worktree = func(string, string) (string, error) { return "", errors.New("no git") }
	if res := r.do(wire.ConversationCreateCmd{OpID: "c2", Cwd: "/w", Provider: wire.ProviderClaude, Worktree: true}); res.R != wire.ResultRefused || res.Code != wire.RefuseInternal {
		t.Fatalf("a tree that could not be made is no reason to share the folder: %+v", res)
	}
}
