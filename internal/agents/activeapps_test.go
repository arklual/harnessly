package agents

import (
	"slices"
	"testing"
)

type keepFirst struct{ AppLauncher }

func (keepFirst) Active(ids []string) []string { return ids[:1] }

// The launch carries only the apps the launcher says are on; one that cannot say leaves the launch as it is.
func TestWithActiveApps(t *testing.T) {
	l := &Launch{Apps: []string{"acme.a", "acme.b"}}
	if got := WithActiveApps(l, keepFirst{}); !slices.Equal(got.Apps, []string{"acme.a"}) || len(l.Apps) != 2 {
		t.Fatalf("%v (the given launch is not changed: %v)", got.Apps, l.Apps)
	}
	if got := WithActiveApps(l, nil); got != l {
		t.Fatal("no launcher: the launch as it is")
	}
	if WithActiveApps(nil, keepFirst{}) != nil {
		t.Fatal("no launch")
	}
}
