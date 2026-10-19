package coreapi

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	// The goldens are the previous runtime's answers, which were in Russian.
	os.Setenv("RUNTIME_LOCALE", "ru")
	os.Exit(m.Run())
}

func TestSessionProcessInEnglish(t *testing.T) {
	t.Setenv("RUNTIME_LOCALE", "en")
	r := newRig(t)
	expect(t, r.post("/api/app/session-process", `{"tabId":"tab-1","action":"stop"}`), 400, `{"error":"tabId and action are required"}`)
	expect(t, r.post("/api/app/session-process", `{"tabId":"nope","action":"kill"}`), 404, `{"error":"Conversation not found"}`)
}
