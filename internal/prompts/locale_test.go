package prompts

import (
	"errors"
	"testing"
)

// The package's TestMain answers in Russian for the goldens; English is the default everywhere else.
func TestRoutesAnswerInEnglish(t *testing.T) {
	t.Setenv("RUNTIME_LOCALE", "en")
	r := newRig(t, "❯ ", Tab{})
	expectError(t, "empty", r.svc.PromptText(body(map[string]any{})), 400, "tabId and text are required")
	expectError(t, "unknown tab", r.svc.PromptText(body(map[string]any{"tabId": "tab-9", "text": "x"})), 404, "Tab not found")
	expectError(t, "blank", r.svc.PromptQueue(body(map[string]any{"tabId": "tab-1", "action": "add", "text": "  "})), 409, "Empty message")
	if got := msgTool("Read"); got != `Step "Read" is running and cannot be split off without cancelling it; the message will arrive right after it` {
		t.Errorf("tool: %q", got)
	}

	t.Setenv("RUNTIME_LOCALE", "ru")
	var qe *QueueError
	if _, err := NewQueue("", newFakeClock()).Add("t1", " ", nil); !errors.As(err, &qe) || qe.Message != "Пустое сообщение" {
		t.Errorf("ru blank: %v", err)
	}
}
