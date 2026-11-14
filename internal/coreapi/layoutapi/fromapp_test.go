package layoutapi

import (
	"reflect"
	"testing"

	"github.com/ly-solutions/harnessly/internal/agents"
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
	base := &agents.Launch{Apps: []string{"acme.docs"}}
	got = withFromApp(base, "acme.inbox", yes)
	if !reflect.DeepEqual(got.Apps, []string{"acme.docs", "acme.inbox"}) || !reflect.DeepEqual(base.Apps, []string{"acme.docs"}) {
		t.Fatalf("added beside the named apps, the request's launch untouched: %+v / %+v", got, base)
	}
	if again := withFromApp(got, "acme.inbox", yes); !reflect.DeepEqual(again.Apps, []string{"acme.docs", "acme.inbox"}) {
		t.Fatalf("not twice: %+v", again)
	}
}
