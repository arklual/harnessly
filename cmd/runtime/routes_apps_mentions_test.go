package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMentionItemsOf(t *testing.T) {
	text := func(body string) json.RawMessage {
		raw, _ := json.Marshal(map[string]any{"content": []map[string]string{{"type": "text", "text": body}}})
		return raw
	}
	got := mentionItemsOf(text(`{"items":[{"label":"ABC-1 Оплата","detail":"в работе","insert":"ABC-1"},{"label":"","insert":"x"},{"label":"без вставки","insert":"  "}]}`))
	if len(got) != 1 || got[0].Insert != "ABC-1" || got[0].Detail != "в работе" {
		t.Fatalf("text answer: %+v", got)
	}
	structured := json.RawMessage(`{"structuredContent":{"items":[{"label":"a","insert":"b"}]}}`)
	if got := mentionItemsOf(structured); len(got) != 1 {
		t.Fatalf("structured answer: %+v", got)
	}
	if got := mentionItemsOf(json.RawMessage(`{"isError":true,"structuredContent":{"items":[{"label":"a","insert":"b"}]}}`)); len(got) != 0 {
		t.Fatalf("an error is no answer: %+v", got)
	}
	if got := mentionItemsOf(text("not json")); len(got) != 0 {
		t.Fatalf("not json: %+v", got)
	}
	many := `{"items":[` + strings.TrimSuffix(strings.Repeat(`{"label":"a","insert":"b"},`, 20), ",") + `]}`
	if got := mentionItemsOf(text(many)); len(got) != mentionItems {
		t.Fatalf("at most %d, got %d", mentionItems, len(got))
	}
	long := mentionItemsOf(text(`{"items":[{"label":"` + strings.Repeat("я", 200) + `","insert":"b"}]}`))
	if len([]rune(long[0].Label)) != mentionLabelMax {
		t.Fatalf("label clipped to %d, got %d", mentionLabelMax, len([]rune(long[0].Label)))
	}
}
