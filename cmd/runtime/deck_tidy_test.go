package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/design"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// A deck's model call and a chat's title take the machine's one turn, one after the other.
func TestOneShotCallsTakeTurns(t *testing.T) {
	release := make(chan struct{})
	started := make(chan string, 2)
	fake := func(_ context.Context, _ wire.Provider, prompt string) (string, error) {
		started <- prompt
		<-release
		return "ok " + prompt, nil
	}
	first, second := newV2Titler("", ""), newV2Titler("", "")
	first.run, second.run = fake, fake
	done := make(chan string, 2)
	go func() { out, _ := first.oneShot(context.Background(), wire.ProviderClaude, "first"); done <- out }()
	if got := <-started; got != "first" {
		t.Fatal(got)
	}
	go func() { out, _ := second.oneShot(context.Background(), wire.ProviderClaude, "second"); done <- out }()
	select {
	case got := <-started:
		t.Fatalf("%q ran while the first held the turn", got)
	case <-time.After(50 * time.Millisecond):
	}
	release <- struct{}{}
	if got := <-done; got != "ok first" {
		t.Fatal(got)
	}
	if got := <-started; got != "second" {
		t.Fatal(got)
	}
	close(release)
	if got := <-done; got != "ok second" {
		t.Fatal(got)
	}
	// A call that gives up while it waits does not run.
	oneShotSlot <- struct{}{}
	defer func() { <-oneShotSlot }()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := first.oneShot(ctx, wire.ProviderClaude, "late"); err == nil {
		t.Fatal("ran without a turn")
	}
}

func TestDeckOneShotIsWired(t *testing.T) {
	if deckOneShot(deps{DataDir: t.TempDir()}) == nil {
		t.Fatal("the tidy route would answer 503")
	}
}

func deckCreate() wire.DesignlyCreate {
	slides := 8
	return wire.DesignlyCreate{Title: "Pitch", Brief: "About acme", Kind: wire.DesignSlides, Provider: wire.ProviderClaude,
		Deck: &wire.DesignlyDeck{Minutes: 15, Slides: &slides, Tone: "biz", Script: true,
			Style: &wire.DesignlyDeckStyle{Source: "Acme template.pptx", Layouts: 9, Vars: map[string]string{"--deck-acc": "#e4002b"}}}}
}

func TestTheCreatePromptOfADeckCarriesTheSettingsAndTheRules(t *testing.T) {
	t.Setenv("RUNTIME_LOCALE", "")
	root := t.TempDir()
	lib := design.New(design.Config{LibraryDir: root, DataDir: t.TempDir(), Texts: design.Texts{Tag: "test-design", CLI: "acme"}})
	t.Cleanup(lib.StopWatching)
	c := deckCreate()
	task := createTask(lib, "pitch", "Pitch", "About acme", c, "Build it on the kit.")
	for _, want := range []string{
		"every board is one 16:9 slide", "project/deck-theme.css", "--deck-*", `data-block="<Name>"`, `data-step="1"`, "translate:",
		"15 minutes", "exactly 8 slides", "businesslike", "130 words per minute", "1950 words", "acme design notes set pitch",
		"Acme template.pptx", "9 slide layouts", "meta/deck.json", "Build it on the kit.",
	} {
		if !strings.Contains(task, want) {
			t.Errorf("the task lacks %q:\n%s", want, task)
		}
	}
	// Without notes the agent is told not to write them; without a count it picks.
	c.Deck.Script, c.Deck.Slides, c.Deck.Style = false, nil, nil
	task = createTask(lib, "pitch", "Pitch", "About acme", c, "")
	if strings.Contains(task, "design notes set") || !strings.Contains(task, "Do not write notes") || !strings.Contains(task, "yours to pick") || strings.Contains(task, "Acme template") {
		t.Errorf("no script:\n%s", task)
	}
	// A length and a count nobody asked for are left to the agent, with no number of words to hit.
	c.Deck.Script, c.Deck.Minutes = true, 0
	task = createTask(lib, "pitch", "Pitch", "About acme", c, "Build it on the kit.")
	for _, want := range []string{"choose both yourself", "as long as each slide needs", "acme design notes set pitch", "carry them into the --deck-* variables"} {
		if !strings.Contains(task, want) {
			t.Errorf("an open deck lacks %q:\n%s", want, task)
		}
	}
	for _, not := range []string{"0 minutes", "words for the whole deck", "exactly"} {
		if strings.Contains(task, not) {
			t.Errorf("an open deck says %q:\n%s", not, task)
		}
	}
	nine := 9
	c.Deck.Slides = &nine
	if task = createTask(lib, "pitch", "Pitch", "x", c, ""); !strings.Contains(task, "exactly 9 slides, the length of the talk is yours to choose") {
		t.Errorf("a count without a length:\n%s", task)
	}
	// A deck from a page that does not know decks, and other kinds, get no deck line.
	c.Deck = nil
	if task = createTask(lib, "pitch", "Pitch", "x", c, ""); strings.Contains(task, "meta/deck.json") || !strings.Contains(task, "16:9 slide") {
		t.Errorf("no deck:\n%s", task)
	}
	c.Kind = wire.DesignSite
	if task = createTask(lib, "pitch", "Pitch", "x", c, ""); strings.Contains(task, "slide") || !strings.Contains(task, "website") {
		t.Errorf("site:\n%s", task)
	}
	t.Setenv("RUNTIME_LOCALE", "ru")
	c = deckCreate()
	task = createTask(lib, "pitch", "Pitch", "x", c, "")
	for _, want := range []string{"Презентация: 15 минут, ровно 8 слайдов", "по-деловому", "130 слов в минуту", "design notes set pitch"} {
		if !strings.Contains(task, want) {
			t.Errorf("ru task lacks %q:\n%s", want, task)
		}
	}
}

func TestADeckIsRefusedBeforeAnythingIsMadeWhenItsTemplateIsNotPlain(t *testing.T) {
	c := deckCreate()
	c.Deck.Style.Vars = map[string]string{"--deck-acc": "red;}body{x:y"}
	if err := design.ValidateDeckSettings(design.BuildDeckSettings(deckInit(c.Deck))); err == nil {
		t.Fatal("accepted")
	}
	err := deckCreateFailure(design.ValidateDeckSettings(design.BuildDeckSettings(deckInit(c.Deck))))
	if _, ok := err.(*design.CreateError); !ok {
		t.Fatalf("%T", err)
	}
}

func TestInitDeckWritesTheSettingsTheAgentAndThePageRead(t *testing.T) {
	t.Setenv("RUNTIME_LOCALE", "")
	root := t.TempDir()
	engine := filepath.Join(t.TempDir(), "engine.js")
	if err := os.WriteFile(engine, []byte("/* engine */"), 0o644); err != nil {
		t.Fatal(err)
	}
	lib := design.New(design.Config{LibraryDir: root, DataDir: t.TempDir(), EngineFile: engine, Texts: design.Texts{Tag: "test-design", CLI: "acme"}})
	t.Cleanup(lib.StopWatching)
	p, err := lib.CreateProject("Pitch")
	if err != nil {
		t.Fatal(err)
	}
	if err := lib.InitDeck(p.ID, deckInit(deckCreate().Deck)); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, p.ID, "meta", "deck.json"))
	if err != nil || !strings.Contains(string(raw), `"minutes": 15`) || !strings.Contains(string(raw), `"tone": "biz"`) || !strings.Contains(string(raw), `"source": "Acme template.pptx"`) {
		t.Fatalf("%s %v", raw, err)
	}
	css, err := os.ReadFile(filepath.Join(root, p.ID, "project", "deck-theme.css"))
	if err != nil || !strings.Contains(string(css), "--deck-acc:#e4002b;") {
		t.Fatalf("%s %v", css, err)
	}
}

func TestADeckWithoutSettingsGetsTheDefaultsAndOlderOnesReadWithoutTheFile(t *testing.T) {
	c := deckCreate()
	c.Deck = nil
	in := deckInitOf(c)
	if s := design.BuildDeckSettings(in); s.Minutes != 10 || s.Tone != design.DeckToneTalk || !s.Script || s.Theme != "co" {
		t.Fatalf("%+v", s)
	}
	if err := design.ValidateDeckSettings(design.BuildDeckSettings(in)); err != nil {
		t.Fatal(err)
	}
}
