package delegation

import (
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
