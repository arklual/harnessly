package noticeslot

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// fake builds a key-shaped test string from its prefix and filler, so no key-like literal sits in the source.
func fake(prefix string, filler string, n int) string { return prefix + strings.Repeat(filler, n) }

func TestLooksSecret(t *testing.T) {
	for _, s := range []string{
		fake("sk"+"-", "aB3d", 5), "мой ключ " + fake("gh"+"p_", "x1Y2", 5), fake("gl"+"pat-", "q7", 10),
		fake("AK"+"IA", "Q", 16), fake("ey"+"J", "abcdEFGH12", 1) + "." + fake("", "ijklMNOP34", 1) + "." + fake("", "qrstUVWX56", 1),
		"Zq8mN3xP7vL2kR9tW4yB6cF1hJ5sD0gA", fake("-----BEG"+"IN OPENSSH", "", 0),
	} {
		if !LooksSecret(s) {
			t.Errorf("%q should look secret", s)
		}
	}
	for _, s := range []string{
		"Куда выкатить сборку 482?", "https://gitlab.example.com/acme/app/-/merge_requests/80",
		"commit 571ea1e1c0ffee5d0e2b4b8a9f3e1d2c3b4a5f6e", "123e4567-e89b-12d3-a456-426614174000", "skip-this-step",
		"Покажи зимой тоже", "orders_2023_backup_table_name",
	} {
		if LooksSecret(s) {
			t.Errorf("%q should not look secret", s)
		}
	}
}

func TestCheckRefusesSecretsAndPasswordFields(t *testing.T) {
	cases := []Spec{
		{Kind: KindField, Title: "Введите пароль от почты"},
		{Kind: KindField, Title: "Нужен API key"},
		{Kind: KindCode, Title: "Код", Text: "войдите в аккаунт"},
		{Kind: KindCustom, Title: "Вход", View: `<input type="pass` + `word">`},
		{Kind: KindCustom, Title: "Вход", View: `<input autocomplete='current-pass` + `word'>`},
		{Kind: KindStatus, Title: "Ключ " + fake("sk"+"-", "aB3d", 5) + " готов"},
	}
	for _, c := range cases {
		if err := Check(&c); !errors.Is(err, ErrSecret) {
			t.Errorf("%+v: want ErrSecret, got %v", c, err)
		}
	}
	for _, ok := range []Spec{
		{Kind: KindField, Title: "Тема рассылки"},
		{Kind: KindCustom, Title: "Обложка", View: `<div class="ink-grid"><button class="ink-tile" data-answer='{"cover":"dark"}'>Тёмная</button></div>`},
	} {
		if err := Check(&ok); err != nil {
			t.Fatalf("%+v: %v", ok, err)
		}
	}
	bad := []Spec{
		{Kind: "secret-field", Title: "x"},
		{Kind: KindChoice, Title: "x", Options: []string{"one"}},
		{Kind: KindCode, Title: "x", Digits: 12},
		{Kind: KindChoice, Title: ""},
		{Kind: KindCustom, Title: "x", View: `<button onclick="go()">Да</button>`},
		{Kind: KindCustom, Title: "x", View: `<scr` + `ipt>fetch('/')</script>`},
		{Kind: KindCustom, Title: "x", View: `<input name="q">`},
		{Kind: KindCustom, Title: "x", View: `<form><button>Да</button></form>`},
		{Kind: KindCustom, Title: "x", View: `<iframe src="https://example.com"></iframe>`},
		{Kind: KindCustom, Title: "x", View: `<a href="https://example.com">Да</a>`},
	}
	for _, c := range bad {
		if err := Check(&c); !errors.Is(err, ErrArgs) {
			t.Errorf("%+v: want ErrArgs, got %v", c, err)
		}
	}
}

var app = Source{Kind: SourceApp, ID: "ci", Name: "Сборки"}

func TestAskAnswered(t *testing.T) {
	s := New()
	done := make(chan Result, 1)
	go func() {
		r, err := s.Ask(context.Background(), app, "c1", Spec{Kind: KindChoice, Title: "Куда?", Options: []string{"Стенд", "Тест"}, Own: true})
		if err != nil {
			t.Error(err)
		}
		done <- r
	}()
	var id string
	for id == "" {
		for _, n := range s.Pending() {
			id = n.ID
		}
		time.Sleep(time.Millisecond)
	}
	if err := s.Answer(id, json.RawMessage(`{"choice":5}`)); !errors.Is(err, ErrAnswer) {
		t.Fatalf("out of range: %v", err)
	}
	own, _ := json.Marshal(map[string]string{"text": "мой " + fake("gh"+"p_", "x1Y2", 5)})
	if err := s.Answer(id, own); !errors.Is(err, ErrAnswerSecret) {
		t.Fatalf("secret own answer: %v", err)
	}
	if err := s.Answer(id, json.RawMessage(`{"choice":1}`)); err != nil {
		t.Fatal(err)
	}
	r := <-done
	if r.Status != StatusAnswered || string(r.Value) != `{"choice":1,"option":"Тест"}` {
		t.Fatalf("got %+v %s", r, r.Value)
	}
}

func TestQueueLimitsJoinAndMute(t *testing.T) {
	now := time.Unix(1000, 0)
	s := New()
	s.Now = func() time.Time { return now }
	q := func(title, conv string) Notice {
		n, err := s.Open(app, conv, Spec{Kind: KindConfirm, Title: title})
		if err != nil {
			t.Fatal(err)
		}
		now = now.Add(time.Millisecond)
		return n
	}
	a := q("Первый", "c1")
	if again := q("Первый", "c1"); again.ID != a.ID {
		t.Fatal("the same question should join the open one")
	}
	q("Второй", "c1")
	q("Третий", "c2")
	q("Четвёртый", "c3")
	q("Пятый", "c4")
	if _, err := s.Open(app, "c5", Spec{Kind: KindConfirm, Title: "Шестой"}); !errors.Is(err, ErrRate) {
		t.Fatalf("the sixth in a minute: want ErrRate, got %v", err)
	}
	now = now.Add(time.Minute)
	q("Шестой", "c5")
	shown := map[string]bool{}
	for _, n := range s.Pending() {
		shown[n.Title] = !n.Queued
	}
	// One per chat, three of a source on the machine: the fourth chat's and the fifth's wait their turn.
	want := map[string]bool{"Первый": true, "Второй": false, "Третий": true, "Четвёртый": true, "Пятый": false, "Шестой": false}
	for k, v := range want {
		if shown[k] != v {
			t.Errorf("%s shown=%v, want %v", k, shown[k], v)
		}
	}
	s.MuteSource(app)
	if r, _ := s.Wait(context.Background(), a.ID); r.Status != StatusMuted {
		t.Fatalf("muted: %+v", r)
	}
	s.Muted = func(Source) bool { return true }
	if _, err := s.Open(app, "c1", Spec{Kind: KindStatus, Title: "x"}); !errors.Is(err, ErrMuted) {
		t.Fatalf("want ErrMuted, got %v", err)
	}
}

func TestTimeoutAndProgress(t *testing.T) {
	now := time.Unix(1000, 0)
	s := New()
	s.Now = func() time.Time { return now }
	n, _ := s.Open(app, "c1", Spec{Kind: KindField, Title: "Имя", TimeoutSec: 30})
	p := 0.2
	pr, _ := s.Open(app, "c1", Spec{Kind: KindProgress, Title: "Сборка", Progress: &p})
	next := 0.7
	if err := s.Update(Source{Kind: SourceAgent, ID: "x"}, pr.ID, &next, nil, nil); !errors.Is(err, ErrGone) {
		t.Fatal("another source must not move it")
	}
	if err := s.Update(app, pr.ID, &next, nil, nil); err != nil {
		t.Fatal(err)
	}
	now = now.Add(31 * time.Second)
	if r, done := s.Wait(context.Background(), n.ID); !done || r.Status != StatusTimeout {
		t.Fatalf("timeout: %+v %v", r, done)
	}
	list := s.Pending()
	if len(list) != 1 || *list[0].Progress != 0.7 {
		t.Fatalf("progress: %+v", list)
	}
}

func TestMutesFile(t *testing.T) {
	path := t.TempDir() + "/mutes.json"
	m := &Mutes{Path: path}
	if err := m.Set(app, true); err != nil {
		t.Fatal(err)
	}
	again := &Mutes{Path: path}
	if !again.Has(app) || len(again.List()) != 1 {
		t.Fatal("mute should survive")
	}
	_ = again.Set(app, false)
	if (&Mutes{Path: path}).Has(app) {
		t.Fatal("unmute should survive")
	}
}
