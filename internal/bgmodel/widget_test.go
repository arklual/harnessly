package bgmodel

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// a smart widget runs on the machine's Claude Haiku when nothing is picked for it, and its answer is short.
func TestAWidgetRunsOnTheMachinesClaudeByDefault(t *testing.T) {
	var agent, model, prompt string
	r := &Runner{
		Store:     &Store{Path: filepath.Join(t.TempDir(), "v2", "background-tasks.json")},
		Providers: fakeProviders{},
		Client:    &Client{},
		Agent: func(_ context.Context, a, m string, call Call, _ func(string)) (string, error) {
			agent, model, prompt = a, m, call.Prompt
			return strings.Repeat("я", WidgetAnswerMax+50), nil
		},
	}
	if !r.Usable(FeatureWidget) {
		t.Fatal("a widget has no model with the machine's agent there")
	}
	text, used, err := r.Widget(context.Background(), "Что с лимитами?", `{"5h": 40}`)
	if err != nil || !used {
		t.Fatalf("used=%v err=%v", used, err)
	}
	if agent != AgentClaude || model != WidgetDefaultModel {
		t.Fatalf("ran on %s/%s", agent, model)
	}
	if !strings.Contains(prompt, "Что с лимитами?") || !strings.Contains(prompt, `{"5h": 40}`) {
		t.Fatalf("the prompt lacks the request or the data: %q", prompt)
	}
	if n := len([]rune(text)); n != WidgetAnswerMax {
		t.Fatalf("the answer is %d runes", n)
	}
	if (&Runner{Store: r.Store}).Usable(FeatureWidget) {
		t.Fatal("a runner without the machine's agent says it can answer a widget")
	}
}
