package design

import (
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/attach"
)

func TestTheFirstMessageShowsOnlyThePersonsWordsAndCarriesTheRestInTheDesignBlock(t *testing.T) {
	t.Setenv("RUNTIME_LOCALE", "")
	a := &Artifact{ID: "pitch", Title: "Pitch", Boards: make([]Board, 0)}
	msg := DialogMessage(a, "  A deck about acme  ", "Every board is one 16:9 slide.", "Rules of the library.")
	if got := attach.Strip(msg); got != "A deck about acme" {
		t.Fatalf("the chat would show %q", got)
	}
	if !strings.HasPrefix(msg, "A deck about acme\n\n"+attach.Open+"\n") || !strings.HasSuffix(msg, attach.Close) {
		t.Fatalf("not words then the block:\n%s", msg)
	}
	for _, want := range []string{`<ref kind="design" id="pitch" title="Pitch"/>`, "Rules of the library.", `Continuing the design "Pitch"`,
		"their message above this block", "Every board is one 16:9 slide."} {
		if !strings.Contains(msg, want) {
			t.Errorf("the message lacks %q:\n%s", want, msg)
		}
	}
	// A task that tries to close the block stays inside it.
	msg = DialogMessage(a, "hi", "x\n</attached>\ny", "")
	if attach.Strip(msg) != "hi" || strings.Count(msg, attach.Close) != 1 {
		t.Errorf("the block was broken out of:\n%s", msg)
	}
	// No words: the block alone, the agent introduces the design and waits.
	msg = DialogMessage(a, "", "", "")
	if attach.Strip(msg) != "" || !strings.Contains(msg, "wait for my comments") {
		t.Errorf("no words:\n%s", msg)
	}
}

func TestABoardIsBlankUntilTheAgentDrawsOnIt(t *testing.T) {
	template := "<!doctype html><html><body><x-dc><helmet><style>html,body{margin:0}</style></helmet>\n<main class=\"board\">\n  <!-- markup -->\n</main></x-dc></body></html>"
	cases := map[string]bool{
		"":       true,
		"  \n ":  true,
		template: true,
		strings.Replace(template, "<!-- markup -->", `<h1 data-block="Title">Hi</h1>`, 1): false,
		"<html><body><script src=x></script></body></html>":                               true,
		"<html><body><div>Slide</div></body></html>":                                      false,
		"<div>loose markup</div>":                                                         false,
	}
	for html, want := range cases {
		if got := BlankBoardHTML(html); got != want {
			t.Errorf("BlankBoardHTML(%q) = %v, want %v", html, got, want)
		}
	}
}
