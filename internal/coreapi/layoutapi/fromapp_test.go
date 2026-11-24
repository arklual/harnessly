package layoutapi

import (
	"reflect"
	"testing"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// The app that starts a chat for the person is on in it when the machine lets it, beside the apps the launch names,
// once.
func TestWithFromApp(t *testing.T) {
	yes := func(string) bool { return true }
	no := func(string) bool { return false }
	if got := withFromApp(nil, "acme.inbox", nil); got != nil {
		t.Fatalf("without a check nothing is turned on: %+v", got)
	}
	if got := withFromApp(nil, "acme.inbox", no); got != nil {
		t.Fatalf("an app the machine refuses stays off: %+v", got)
	}
	got := withFromApp(nil, "acme.inbox", yes)
	if got == nil || !reflect.DeepEqual(got.Apps, []string{"acme.inbox"}) {
		t.Fatalf("the app is on: %+v", got)
	}
	model := "gpt-6.2-sol"
	base := &agents.Launch{Profile: "gpt", Model: &model, Apps: []string{"acme.docs"}}
	got = withFromApp(base, "acme.inbox", yes)
	if got.Profile != "gpt" || got.Model == nil || *got.Model != model {
		t.Fatalf("adding the app lost the model choice: %+v", got)
	}
	if !reflect.DeepEqual(got.Apps, []string{"acme.docs", "acme.inbox"}) || !reflect.DeepEqual(base.Apps, []string{"acme.docs"}) {
		t.Fatalf("added beside the named apps, the request's launch untouched: %+v / %+v", got, base)
	}
	if again := withFromApp(got, "acme.inbox", yes); !reflect.DeepEqual(again.Apps, []string{"acme.docs", "acme.inbox"}) {
		t.Fatalf("not twice: %+v", again)
	}
}

// A hub run's chat keeps its mode and takes the hub's mark; without a mark it is an automation chat as before.
func TestAutomationMeta(t *testing.T) {
	plain := automationMeta(nil)
	if plain.String("mode") != "automation" || plain.Object("from") != nil {
		t.Fatalf("without a mark: %v", plain)
	}
	m := automationMeta((&wire.ChatFrom{App: "routines", Label: "Регулярные", Title: " Разбор алертов "}).Clean())
	f := m.Object("from")
	if m.String("mode") != "automation" || f == nil || f.String("app") != "routines" || f.String("label") != "Регулярные" ||
		f.String("title") != "Разбор алертов" {
		t.Fatalf("with the hub's mark: %v", m)
	}
}
