package delegation

import (
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// A delegated chat of a chat on a backend profile stays on the profile; a task for another CLI, or a model of the CLI's
// own account, does not.
func TestDelegatedChatInheritsTheProfile(t *testing.T) {
	s := &Service{ProfileModels: func(p string) []string {
		if p == "glm" {
			return []string{"glm-5", "glm-4.5-air"}
		}
		return nil
	}}
	caller := wire.Conversation{Provider: wire.ProviderClaude, Model: &wire.Model{ID: "glm-5", Profile: "glm"}}
	m, profile, err := s.model(Task{Provider: wire.ProviderClaude}, caller)
	if err != nil || profile != "glm" || m == nil || m.ID != "glm-5" {
		t.Fatalf("%+v %q %v", m, profile, err)
	}
	if m, profile, err = s.model(Task{Provider: wire.ProviderClaude, Model: "glm-4.5-air"}, caller); err != nil || profile != "glm" || m.ID != "glm-4.5-air" {
		t.Fatalf("%+v %q %v", m, profile, err)
	}
	if m, profile, err = s.model(Task{Provider: wire.ProviderClaude, Model: "opus"}, caller); err != nil || profile != "" || m == nil || m.ID != "opus" {
		t.Fatalf("a model of the CLI's own account: %+v %q %v", m, profile, err)
	}
	if _, profile, err = s.model(Task{Provider: wire.ProviderCodex}, caller); err != nil || profile != "" {
		t.Fatalf("another CLI: %q %v", profile, err)
	}
	if _, _, err = s.model(Task{Provider: wire.ProviderClaude, Effort: wire.EffortHigh}, caller); err == nil {
		t.Fatal("a profile takes no effort")
	}
	plain := wire.Conversation{Provider: wire.ProviderClaude, Model: &wire.Model{ID: "opus"}}
	if _, profile, err = s.model(Task{Provider: wire.ProviderClaude}, plain); err != nil || profile != "" {
		t.Fatalf("%q %v", profile, err)
	}
}

// GitHub #109: a chat on the CLI's own account delegates to a profile it names, or that the model belongs to alone.
func TestTaskNamesAProfile(t *testing.T) {
	s := &Service{
		Models: func(wire.Provider) []wire.AgentModel { return []wire.AgentModel{{ID: "opus"}, {ID: "sonnet"}} },
		Profiles: func(p wire.Provider) []Profile {
			if p != wire.ProviderClaude {
				return nil
			}
			return []Profile{{ID: "kimi-code", Models: []string{"kimi-for-coding", "shared"}},
				{ID: "glm", Models: []string{"glm-5", "shared"}}}
		},
	}
	plain := wire.Conversation{Provider: wire.ProviderClaude, Model: &wire.Model{ID: "opus"}}
	claude := func(t Task) Task { t.Provider = wire.ProviderClaude; return t }
	for name, c := range map[string]struct {
		task        Task
		model, prof string
		refusalHas  string
	}{
		"profile and model":        {task: claude(Task{Profile: "glm", Model: "glm-5"}), model: "glm-5", prof: "glm"},
		"profile, its first model": {task: claude(Task{Profile: "kimi-code"}), model: "kimi-for-coding", prof: "kimi-code"},
		"model of one profile":     {task: claude(Task{Model: "kimi-for-coding"}), model: "kimi-for-coding", prof: "kimi-code"},
		"own account":              {task: claude(Task{Model: "sonnet"}), model: "sonnet"},
		"model of two profiles":    {task: claude(Task{Model: "shared"}), refusalHas: "kimi-code/shared, glm/shared"},
		"unknown model":            {task: claude(Task{Model: "gpt-9"}), refusalHas: "opus, sonnet; on a profile (profile/model): kimi-code/kimi-for-coding"},
		"unknown profile":          {task: claude(Task{Profile: "grok"}), refusalHas: `no profile "grok" set up here; it has: kimi-code, glm`},
		"model not of the profile": {task: claude(Task{Profile: "glm", Model: "kimi-for-coding"}), refusalHas: "profile glm has no model"},
		"profile with effort":      {task: claude(Task{Profile: "glm", Effort: wire.EffortHigh}), refusalHas: "takes no effort"},
		"profile of another cli":   {task: Task{Provider: wire.ProviderCodex, Profile: "glm"}, refusalHas: "it has: none"},
	} {
		m, prof, err := s.model(c.task, plain)
		if c.refusalHas != "" {
			if err == nil || !strings.Contains(err.Error(), c.refusalHas) {
				t.Errorf("%s: %v, want a refusal with %q", name, err, c.refusalHas)
			}
			continue
		}
		if err != nil || m == nil || m.ID != c.model || prof != c.prof {
			t.Errorf("%s: %+v %q %v", name, m, prof, err)
		}
	}
}
