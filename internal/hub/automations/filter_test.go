package automations

import (
	"encoding/json"
	"testing"
)

// A filter on a fact takes an event whose facts carry it among others (docs/connectors.md: {"facts": {"kind": …}}).
func TestFilterOnOneFact(t *testing.T) {
	var event, kind, other, flat map[string]any
	_ = json.Unmarshal([]byte(`{"type":"webhook.received","facts":{"kind":"ci.failed","repo":"o/r","branch":"main"}}`), &event)
	_ = json.Unmarshal([]byte(`{"facts":{"kind":"ci.failed"}}`), &kind)
	_ = json.Unmarshal([]byte(`{"facts":{"kind":"ci.passed"}}`), &other)
	_ = json.Unmarshal([]byte(`{"type":"webhook.received","facts":{"kind":"ci.failed","repo":"o/r"}}`), &flat)
	if !matchesFilter(kind, event) || !matchesFilter(flat, event) {
		t.Fatal("a fact among others did not match")
	}
	if matchesFilter(other, event) {
		t.Fatal("another kind matched")
	}
	var noFacts map[string]any
	_ = json.Unmarshal([]byte(`{"facts":"x"}`), &noFacts)
	if matchesFilter(kind, noFacts) {
		t.Fatal("facts that are not an object matched")
	}
}
