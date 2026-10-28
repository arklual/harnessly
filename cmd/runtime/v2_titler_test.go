package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/wire"
)

func TestCleanTitleTakesTheTitleOutOfWhatTheAgentSaid(t *testing.T) {
	for in, want := range map[string]string{
		"Доступ к dev-машине\n":                 "Доступ к dev-машине",
		"\"Редизайн дашборда.\"":                "Редизайн дашборда",
		"Title: «Правки лендинга»\n\nПочему...": "Правки лендинга",
		"## Fix login flow":                     "Fix login flow",
		"  \n\n":                                "",
	} {
		if got := cleanTitle(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
	if got := cleanTitle(strings.Repeat("я", 200)); len([]rune(got)) != titleMaxRunes+1 {
		t.Errorf("a long one is cut: %d", len([]rune(got)))
	}
}

func TestTitlerAsksOnceWithTheFirstExchangeAndCleansTheAnswer(t *testing.T) {
	tt := newV2Titler("", "")
	var prompt string
	tt.run = func(_ context.Context, p wire.Provider, pr string) (string, error) {
		prompt = pr
		return "\"Новый чат.\"\n", nil
	}
	got, err := tt.Title(wire.ProviderClaude, "привет, идея про дизайн", "расскажи")
	if err != nil || got != "Новый чат" || !strings.Contains(prompt, "привет, идея про дизайн") || !strings.Contains(prompt, "расскажи") {
		t.Fatalf("%q %v %q", got, err, prompt)
	}
	tt.run = func(context.Context, wire.Provider, string) (string, error) { return "", errors.New("boom") }
	if _, err := tt.Title(wire.ProviderCodex, "a", "b"); err == nil {
		t.Fatal("an error is not a title")
	}
	tt.run = func(context.Context, wire.Provider, string) (string, error) { return "\n", nil }
	if _, err := tt.Title(wire.ProviderClaude, "a", "b"); err == nil {
		t.Fatal("an empty answer is not a title")
	}
}
