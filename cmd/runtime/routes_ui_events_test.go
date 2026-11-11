package main

import (
	"fmt"
	"testing"
	"time"
)

func TestUIEventKeepsOnlyListedWords(t *testing.T) {
	now := time.UnixMilli(1_800_000_000_000)
	e, ok := uiEvent(uiEventIn{Name: "harnessly_notice", At: now.UnixMilli() - 1000, Props: map[string]any{
		"action": "answered",
		"kind":   "question",
		"input":  "trackpad",
		"stack":  "2-3",
		"ms":     float64(4200),
		"title":  "Лендинг кофейни",
		"text":   "Какой вариант шапки?",
		"place":  "/home/someone",
	}}, now)
	if !ok {
		t.Fatal("a listed event was dropped")
	}
	for _, k := range []string{"title", "text", "place"} {
		if e.Properties.Has(k) {
			t.Fatalf("%s left the machine", k)
		}
	}
	if v, _ := e.Properties.Get("ms"); fmt.Sprint(v) != "4200" {
		t.Fatalf("ms = %v", v)
	}
	if v, _ := e.Properties.Get("action"); v != "answered" {
		t.Fatalf("action = %v", v)
	}
	if !e.At.Equal(now.Add(-time.Second)) {
		t.Fatalf("at = %v", e.At)
	}
}

func TestUIEventRefusesUnknownAndFreeText(t *testing.T) {
	now := time.Now()
	if _, ok := uiEvent(uiEventIn{Name: "anything", Props: map[string]any{"action": "shown"}}, now); ok {
		t.Fatal("an unknown event passed")
	}
	if _, ok := uiEvent(uiEventIn{Name: "harnessly_notice", Props: map[string]any{"action": "my secret"}}, now); ok {
		t.Fatal("an action out of the list passed")
	}
	e, ok := uiEvent(uiEventIn{Name: "harnessly_notice", At: now.Add(time.Hour).UnixMilli(),
		Props: map[string]any{"action": "shown", "kind": "secret", "ms": float64(-5)}}, now)
	if !ok || e.Properties.Has("kind") || e.Properties.Has("ms") {
		t.Fatalf("bad words passed: %v", e.Properties.Keys())
	}
	if !e.At.Equal(now) {
		t.Fatal("an event from the future kept its date")
	}
}
