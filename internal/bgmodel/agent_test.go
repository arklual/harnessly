package bgmodel

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/extproviders"
)

// a feature runs on a model of the machine's agent (Claude Code, Codex) when no provider's key is there.
func TestAFeatureRunsOnTheMachinesAgent(t *testing.T) {
	type asked struct{ agent, model, system, prompt string }
	var got []asked
	r := &Runner{
		Store:     &Store{Path: filepath.Join(t.TempDir(), "v2", "background-tasks.json")},
		Providers: fakeProviders{},
		Client:    &Client{},
		Agent: func(_ context.Context, agent, model string, call Call, onDelta func(string)) (string, error) {
			got = append(got, asked{agent, model, call.System, call.Prompt})
			if onDelta != nil {
				onDelta("Отвечаю ")
				onDelta("по найденному [1]")
			}
			return "Отвечаю по найденному [1]", nil
		},
	}
	if _, err := r.Pick(FeatureSearchAnswer, &Choice{Provider: AgentClaude, Model: "haiku", Effort: "high"}); err != nil {
		t.Fatalf("an agent's model is refused: %v", err)
	}
	if c, _ := r.Store.Get(FeatureSearchAnswer); c.Effort != "" {
		t.Fatalf("an agent's model keeps an effort: %+v", c)
	}
	var streamed strings.Builder
	text, _, used, err := r.SearchAnswerStream(context.Background(), "как выкатить?", []SearchSource{{Title: "Выкат", Text: "git push"}},
		func(s string) { streamed.WriteString(s) })
	if !used || err != nil || text == "" || streamed.String() == "" {
		t.Fatalf("the search's answer did not come from the agent: %q used=%v err=%v streamed=%q", text, used, err, streamed.String())
	}
	if len(got) != 1 || got[0].agent != AgentClaude || got[0].model != "haiku" || got[0].system == "" {
		t.Fatalf("the agent was asked %+v", got)
	}
	if out := r.Last()[FeatureSearchAnswer]; !out.OK {
		t.Fatalf("the call is not on the record as gone well: %+v", out)
	}
}

func TestAnAgentIsRefusedWhereNoneCanBeAsked(t *testing.T) {
	r := &Runner{Store: &Store{Path: filepath.Join(t.TempDir(), "x.json")}, Providers: fakeProviders{}}
	if _, err := r.Pick(FeatureUIAgent, &Choice{Provider: AgentCodex, Model: "gpt-5-mini"}); !errors.Is(err, ErrUnknownProvider) {
		t.Fatalf("an agent was taken with no way to ask it: %v", err)
	}
}

func TestAConnectedProviderNamedLikeAnAgentWins(t *testing.T) {
	called := false
	r := &Runner{
		Store:     &Store{Path: filepath.Join(t.TempDir(), "x.json")},
		Providers: fakeProviders{"claude": {ID: "claude", BaseURL: "http://127.0.0.1:1", API: extproviders.APIOpenAI}},
		Client:    &Client{},
		Agent: func(context.Context, string, string, Call, func(string)) (string, error) {
			called = true
			return "x", nil
		},
	}
	if _, err := r.Pick(FeatureNotifySummary, &Choice{Provider: "claude", Model: "m"}); err != nil {
		t.Fatal(err)
	}
	_, _, _ = r.Run(context.Background(), FeatureNotifySummary, Call{Prompt: "p"})
	if called {
		t.Fatal("the agent was asked though the person's provider has that name")
	}
}

func TestAnAgentsEmptyAnswerIsAFailure(t *testing.T) {
	r := &Runner{
		Store:     &Store{Path: filepath.Join(t.TempDir(), "x.json")},
		Providers: fakeProviders{},
		Agent:     func(context.Context, string, string, Call, func(string)) (string, error) { return "  ", nil },
	}
	if _, err := r.Pick(FeatureUIAgent, &Choice{Provider: AgentCodex, Model: "gpt-5-mini"}); err != nil {
		t.Fatal(err)
	}
	if _, used, err := r.Run(context.Background(), FeatureUIAgent, Call{Prompt: "p"}); !used || ReasonOf(err) != ReasonEmpty {
		t.Fatalf("an empty answer went through: used=%v err=%v", used, err)
	}
}
