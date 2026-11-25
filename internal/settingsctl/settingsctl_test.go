package settingsctl

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	os.Setenv("RUNTIME_LOCALE", "ru")
	os.Exit(m.Run())
}

func newService(t *testing.T) (*Service, map[string]string) {
	t.Helper()
	values := map[string]string{"idle": "2h", "continue": On, "children": "5"}
	get := func(k string) func() (string, error) { return func() (string, error) { return values[k], nil } }
	set := func(k string) func(string) error { return func(v string) error { values[k] = v; return nil } }
	now := time.UnixMilli(1_800_000_000_000)
	var clock sync.Mutex // the agent's wait and the person's yes read the clock from two goroutines
	s := &Service{
		Items: []Item{
			{Key: "idle", Section: "Чаты", Title: "Останавливать чат", Kind: KindEnum, Get: get("idle"), Set: set("idle"),
				Options: func() []Option {
					return []Option{{"off", "никогда"}, {"2h", "через 2 часа"}, {"6h", "через 6 часов"}}
				},
				Free: func(v string) bool { return v == "free/model" }},
			{Key: "continue", Title: "Продолжать", Kind: KindBool, Get: get("continue"), Set: set("continue")},
			{Key: "children", Title: "Сколько чатов", Kind: KindNumber, Min: 0, Max: 10, Get: get("children"), Set: set("children")},
			{Key: "broken", Title: "Сломана", Kind: KindBool, Get: func() (string, error) { return "", errors.New("unreadable") }},
		},
		Journal:      &Journal{Path: filepath.Join(t.TempDir(), "v2", "settings-changes.jsonl")},
		SettingsChat: func(conv string) bool { return conv == "settings-chat" },
		Now: func() time.Time {
			clock.Lock()
			defer clock.Unlock()
			now = now.Add(time.Second)
			return now
		},
	}
	return s, values
}

func TestCatalog(t *testing.T) {
	s, _ := newService(t)
	list := s.List()
	if len(list) != 3 {
		t.Fatalf("a setting that cannot be read is left out: %+v", list)
	}
	idle := list[0]
	if idle.Key != "idle" || idle.Value != "2h" || idle.ValueLabel != "через 2 часа" || len(idle.Options) != 3 || idle.Kind != KindEnum {
		t.Fatalf("%+v", idle)
	}
	if b := list[1]; b.ValueLabel != "включено" || len(b.Options) != 2 {
		t.Fatalf("%+v", b)
	}
	if n := list[2]; n.Min == nil || *n.Min != 0 || *n.Max != 10 || n.Value != "5" {
		t.Fatalf("%+v", n)
	}
	if _, err := s.Get("nothing"); RefusalOf(err).Reason != ReasonUnknownKey {
		t.Fatalf("%v", err)
	}
}

func TestShownFollowsTheRolloutLive(t *testing.T) {
	s, values := newService(t)
	open := false
	values["late"] = "default"
	s.Items = append(s.Items, Item{Key: "late", Title: "Позже", Kind: KindEnum, Shown: func() bool { return open },
		Get: func() (string, error) { return values["late"], nil }, Set: func(v string) error { values["late"] = v; return nil },
		Options: func() []Option { return []Option{{"default", "как сейчас"}, {"gpt", "GPT"}} }})
	if len(s.List()) != 3 {
		t.Fatalf("a setting not shown yet is left out: %+v", s.List())
	}
	if _, err := s.Get("late"); RefusalOf(err).Reason != ReasonUnknownKey {
		t.Fatalf("a setting not shown yet is unknown: %v", err)
	}
	open = true
	if len(s.List()) != 4 {
		t.Fatalf("the setting shows once its rollout opens, without a new catalog: %+v", s.List())
	}
	if v, err := s.Get("late"); err != nil || v.Value != "default" {
		t.Fatalf("%+v %v", v, err)
	}
}

func TestApplyChecksAndJournals(t *testing.T) {
	s, values := newService(t)
	person := Actor{Kind: ByPerson}
	for name, c := range map[string][3]string{
		"an unknown setting":       {"nothing", "x", ReasonUnknownKey},
		"a value it does not take": {"idle", "3h", ReasonBadValue},
		"not a switch's value":     {"continue", "yes", ReasonBadValue},
		"a number out of range":    {"children", "11", ReasonBadValue},
		"not a number":             {"children", "05", ReasonBadValue},
		"the value it has":         {"idle", "2h", ReasonSame},
	} {
		_, err := s.Apply(c[0], c[1], person, ViaPage, "")
		r := RefusalOf(err)
		if r.Reason != c[2] {
			t.Errorf("%s: %v", name, err)
		}
		if c[2] == ReasonBadValue && (r.Setting == nil || r.Setting.Key != c[0]) {
			t.Errorf("%s: the refusal does not say what the setting takes", name)
		}
	}
	if len(s.Journal.List(0, "")) != 0 {
		t.Fatal("a refused change is in the journal")
	}
	ch, err := s.Apply("idle", "6h", person, ViaPage, "")
	if err != nil || values["idle"] != "6h" || ch.Old != "2h" || ch.New != "6h" || ch.OldLabel != "через 2 часа" || ch.NewLabel != "через 6 часов" || ch.By.Kind != ByPerson || ch.At == 0 {
		t.Fatalf("%+v, %v", ch, err)
	}
	if _, err := s.Apply("idle", "free/model", person, ViaPage, ""); err != nil {
		t.Fatalf("a value the setting takes past its list: %v", err)
	}
	_, _ = s.Apply("children", "0", person, ViaPage, "")
	s.Record("continue", On, Off, person, ViaPage)
	s.Record("continue", Off, Off, person, ViaPage)
	all := s.Journal.List(0, "")
	if len(all) != 4 || all[0].Key != "continue" || all[0].NewLabel != "выключено" || all[3].ID != ch.ID {
		t.Fatalf("the journal, the newest first: %+v", all)
	}
	if got := s.Journal.List(1, "idle"); len(got) != 1 || got[0].New != "free/model" {
		t.Fatalf("one setting's last change: %+v", got)
	}
}

func TestTheSettingsChatChangesAtOnce(t *testing.T) {
	s, values := newService(t)
	out, err := s.Propose("idle", "6h", "  попросили\nдержать дольше ", Actor{Conv: "settings-chat", Title: "Настройки"})
	if err != nil || out.State != StateApplied || out.Proposal != nil || out.Change == nil || values["idle"] != "6h" {
		t.Fatalf("%+v, %v", out, err)
	}
	ch := s.Journal.List(0, "")[0]
	if ch.By.Kind != ByAgent || ch.By.Conv != "settings-chat" || ch.By.Title != "Настройки" || ch.Via != ViaSettingsChat || ch.Why != "попросили держать дольше" {
		t.Fatalf("%+v", ch)
	}
	if len(s.Pending()) != 0 {
		t.Fatal("a card for a change made at once")
	}
}

func TestAnotherChatWaitsForThePerson(t *testing.T) {
	s, values := newService(t)
	by := Actor{Conv: "c1", Title: "Починка входа"}
	out, err := s.Propose("idle", "6h", "так просили", by)
	if err != nil || out.State != StateWaiting || out.Proposal == nil || out.Change != nil {
		t.Fatalf("%+v, %v", out, err)
	}
	p := *out.Proposal
	if values["idle"] != "2h" || len(s.Journal.List(0, "")) != 0 {
		t.Fatal("changed before the person said yes")
	}
	if p.Conv != "c1" || p.ConvTitle != "Починка входа" || p.Old != "2h" || p.New != "6h" || p.NewLabel != "через 6 часов" || p.Why != "так просили" {
		t.Fatalf("%+v", p)
	}
	// The same change asked again is the same card; a chat that cannot be told is no Settings chat either.
	if again, _ := s.Propose("idle", "6h", "ещё раз", by); again.Proposal == nil || again.Proposal.ID != p.ID {
		t.Fatalf("%+v", again)
	}
	if out, _ := s.Propose("continue", Off, "", Actor{}); out.State != StateWaiting {
		t.Fatalf("an agent of no chat: %+v", out)
	}
	if got := s.Pending(); len(got) != 2 || got[0].ID != p.ID {
		t.Fatalf("%+v", got)
	}
	if out := s.Wait(context.Background(), p.ID, 0); out.State != StateWaiting {
		t.Fatalf("%+v", out)
	}

	// The agent waits; the person says yes.
	waited := make(chan Outcome, 1)
	go func() { waited <- s.Wait(context.Background(), p.ID, 5*time.Second) }()
	done, err := s.Approve(p.ID)
	if err != nil || done.State != StateApplied || done.Change == nil || values["idle"] != "6h" {
		t.Fatalf("%+v, %v", done, err)
	}
	if got := <-waited; got.State != StateApplied {
		t.Fatalf("the waiting agent: %+v", got)
	}
	ch := s.Journal.List(1, "idle")[0]
	if ch.By.Kind != ByAgent || ch.By.Conv != "c1" || ch.Via != ViaConfirmed || ch.Old != "2h" || ch.New != "6h" {
		t.Fatalf("%+v", ch)
	}
	if _, err := s.Approve(p.ID); RefusalOf(err).Reason != ReasonClosed {
		t.Fatalf("a second yes: %v", err)
	}
	if _, err := s.Decline("sp_nothing"); RefusalOf(err).Reason != ReasonNotFound {
		t.Fatalf("%v", err)
	}

	// The person says no: nothing changes.
	out, _ = s.Propose("children", "9", "", by)
	if got, err := s.Decline(out.Proposal.ID); err != nil || got.State != StateDeclined || values["children"] != "5" {
		t.Fatalf("%+v, %v", got, err)
	}
	if got := s.Wait(context.Background(), out.Proposal.ID, 0); got.State != StateDeclined {
		t.Fatalf("%+v", got)
	}
	if len(s.Journal.List(0, "children")) != 0 {
		t.Fatal("a declined change is in the journal")
	}
}

func TestProposalsExpireAndAreLimited(t *testing.T) {
	s, _ := newService(t)
	now := time.UnixMilli(1_800_000_000_000)
	s.Now = func() time.Time { return now }
	by := Actor{Conv: "c1"}
	out, _ := s.Propose("idle", "6h", "", by)
	now = now.Add(proposalTTL + time.Second)
	if got := s.Wait(context.Background(), out.Proposal.ID, 0); got.State != StateExpired {
		t.Fatalf("%+v", got)
	}
	if _, err := s.Approve(out.Proposal.ID); RefusalOf(err).Reason != ReasonClosed {
		t.Fatalf("a yes after the time: %v", err)
	}
	// One chat proposes at most perConv changes in perConvEvery.
	now = now.Add(time.Hour)
	var err error
	for _, v := range []string{"0", "1", "2", "3", "4", "6", "7", "8", "9"} {
		_, err = s.Propose("children", v, "", by)
	}
	if RefusalOf(err).Reason != ReasonRateLimited {
		t.Fatalf("%v", err)
	}
	if _, err := s.Propose("children", "7", "", Actor{Conv: "c2"}); err != nil {
		t.Fatalf("another chat: %v", err)
	}
}

func TestJournalKeepsTheLast(t *testing.T) {
	j := &Journal{Path: filepath.Join(t.TempDir(), "changes.jsonl")}
	for i := 0; i < JournalKept+3; i++ {
		if err := j.Append(Change{ID: "sc_" + strconv.Itoa(i), Key: "k"}); err != nil {
			t.Fatal(err)
		}
	}
	all := j.List(0, "")
	if len(all) != JournalKept || all[0].ID != "sc_"+strconv.Itoa(JournalKept+2) || all[len(all)-1].ID != "sc_3" {
		t.Fatalf("%d, %s … %s", len(all), all[0].ID, all[len(all)-1].ID)
	}
}

func TestWatchMovesWithEveryChange(t *testing.T) {
	s := &Service{}
	rev := s.Rev()
	// Nothing moved: the wait runs out and answers the same revision.
	if got := s.WaitRev(context.Background(), rev, 10*time.Millisecond); got != rev {
		t.Fatalf("nothing moved: %d, was %d", got, rev)
	}
	// A page with an older revision is answered at once.
	if got := s.WaitRev(context.Background(), rev-1, time.Minute); got != rev {
		t.Fatalf("an older revision: %d, want %d", got, rev)
	}
	got := make(chan uint64, 1)
	go func() { got <- s.WaitRev(context.Background(), rev, time.Minute) }()
	time.Sleep(20 * time.Millisecond)
	s.bump()
	select {
	case next := <-got:
		if next == rev {
			t.Fatal("the revision did not move")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the wait was not woken")
	}
}

// A value that changes a part of a text strikes out on the card only what it replaces: nothing when it adds.
func TestProposalOfAPartSaysWhatItReplaces(t *testing.T) {
	s, _ := newService(t)
	list := "a,b"
	s.Items = append(s.Items, Item{Key: "list", Title: "Список", Kind: KindText,
		Get: func() (string, error) { return list, nil }, Set: func(v string) error { list += "," + v; return nil },
		Free:     func(string) bool { return true },
		Replaces: func(v string) (string, bool) { return map[string]string{"b2": "b"}[v], v != "whole" }})
	for value, want := range map[string]string{"c": "", "b2": "b", "whole": "a,b"} {
		out, err := s.Propose("list", value, "", Actor{Conv: "chat-" + value})
		if err != nil || out.Proposal == nil || out.Proposal.OldLabel != want {
			t.Errorf("%s: %+v, %v; want the card to strike out %q", value, out.Proposal, err, want)
		}
	}
}

func TestAChangeMadeElsewhereIsToldWhoAndSaysItsRefusal(t *testing.T) {
	s, _ := newService(t)
	var by Actor
	var refuse error
	s.Items = append(s.Items, Item{Key: "shared", Title: "Общее", Kind: KindText, Get: func() (string, error) { return "[]", nil },
		Free:  func(string) bool { return true },
		SetBy: func(_ string, a Actor) (string, error) { by = a; return "ждёт админа", refuse }})

	// In the Settings chat the change is made at once, and the setting is told the chat; what the setting says of a
	// value it only passed on goes with the change.
	out, err := s.Propose("shared", "one", "", Actor{Kind: ByAgent, Conv: "settings-chat", Title: "Настройки"})
	if err != nil || out.Change == nil || out.Change.Note != "ждёт админа" {
		t.Fatalf("%+v %v", out, err)
	}
	if by.Conv != "settings-chat" || by.Title != "Настройки" {
		t.Fatalf("the setting was not told who: %+v", by)
	}

	// Where the change is made it may be refused: the words reach the caller.
	refuse = &Said{Message: "нет права"}
	_, err = s.Propose("shared", "two", "", Actor{Kind: ByAgent, Conv: "settings-chat"})
	if r := RefusalOf(err); err == nil || r.Reason != ReasonFailed || r.Message != "нет права" {
		t.Fatalf("%+v", r)
	}

	// From another chat the person agrees first; a refusal after that is kept with the proposal, which is declined.
	out, err = s.Propose("shared", "three", "", Actor{Kind: ByAgent, Conv: "tab-1", Title: "Чат"})
	if err != nil || out.State != StateWaiting {
		t.Fatalf("%+v %v", out, err)
	}
	if _, err := s.Approve(out.Proposal.ID); RefusalOf(err).Message != "нет права" {
		t.Fatalf("%v", err)
	}
	got := s.Wait(context.Background(), out.Proposal.ID, 0)
	if got.State != StateDeclined || got.Proposal == nil || got.Proposal.Failed != "нет права" {
		t.Fatalf("%+v", got)
	}
	if by.Conv != "tab-1" || by.Kind != ByAgent {
		t.Fatalf("%+v", by)
	}
}
