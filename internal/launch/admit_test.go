package launch

import (
	"testing"

	"github.com/ly-solutions/harnessly/internal/agents"
)

func TestStartIsRefusedByAdmit(t *testing.T) {
	model := "claude-opus-4-1"
	var gotProvider, gotModel string
	s := New(Config{Admit: func(provider string, l *agents.Launch) string {
		gotProvider = provider
		if l != nil && l.Model != nil {
			gotModel = *l.Model
		}
		return "Limit reached. Resets 01.11 09:30."
	}})
	res, err := s.Start(Options{Provider: agents.Claude, Launch: &agents.Launch{Model: &model}})
	if err != nil || res.OK || res.Status != 403 || res.Body.String("error") != "Limit reached. Resets 01.11 09:30." {
		t.Fatalf("%+v %v", res, err)
	}
	if gotProvider != agents.Claude || gotModel != model {
		t.Errorf("%q %q", gotProvider, gotModel)
	}
}

func TestStartGoesOnWhenAdmitAllows(t *testing.T) {
	// An empty answer is no refusal: the start goes on to its own checks, and a relative folder fails there with a 400.
	for _, admit := range []func(string, *agents.Launch) string{nil, func(string, *agents.Launch) string { return "" }} {
		res, err := New(Config{Admit: admit}).Start(Options{Provider: agents.Codex, Cwd: "rel"})
		if err != nil || res.OK || res.Status != 400 {
			t.Fatalf("%+v %v", res, err)
		}
	}
}
