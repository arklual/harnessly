package apps

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestChatBudgetWarning(t *testing.T) {
	if w := ChatBudgetWarning(ChatInstructionsBudgetBytes); w != "" {
		t.Errorf("within the budget: %q", w)
	}
	w := ChatBudgetWarning(ChatInstructionsBudgetBytes + 4)
	if !strings.Contains(w, "12289") || !strings.Contains(w, "12288") {
		t.Errorf("the warning names both sizes in tokens: %q", w)
	}
	if EstimateTokens(0) != 0 || EstimateTokens(1) != 1 || EstimateTokens(4) != 1 || EstimateTokens(5) != 2 {
		t.Error("tokens are bytes / 4, rounded up")
	}
}

func TestToolsListBytes(t *testing.T) {
	raw := json.RawMessage(`{"tools":[{"name":"ab","description":"cde","inputSchema":{"type":"object"}},{"name":"f"}]}`)
	if got, want := ToolsListBytes(raw), 2+3+len(`{"type":"object"}`)+1; got != want {
		t.Errorf("got %d, want %d", got, want)
	}
	if ToolsListBytes(json.RawMessage(`nope`)) != 0 {
		t.Error("an unreadable list weighs nothing")
	}
}
