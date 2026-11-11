package machines_test

import (
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/machines"
)

// A link that comes back inside probeValid does not make the machine run its probes again: they start the machine's
// CLIs, and a link that flickers would run them every time. Past the interval the next link is probed.
func TestRelinkInsideTheIntervalRunsNoProbes(t *testing.T) {
	f := setup(t)
	a := f.owner.Machine(f.spaceID, nil)
	probes := func() (int, int) { return len(a.Calls("preflight.run")), len(a.Calls("agents.status")) }
	if p, s := probes(); p != 1 || s != 1 {
		t.Fatalf("a first link: %d preflights, %d agents statuses, want one of each", p, s)
	}
	relink := func() {
		t.Helper()
		hellos := len(a.Calls("proxy.get"))
		a.Close()
		f.s.Eventually("the link to go", func() bool { return f.view(t, a.MachineID)["status"] == "offline" })
		a.Connect()
		f.s.Eventually("the new link's routine to end", func() bool { return len(a.Calls("proxy.get")) > hellos })
	}

	f.s.Clock.Advance(2 * time.Minute)
	relink()
	if p, s := probes(); p != 1 || s != 1 {
		t.Errorf("a link back after two minutes: %d preflights, %d agents statuses, want no new ones", p, s)
	}
	if pre, _ := f.view(t, a.MachineID)["preflight"].(map[string]any); pre["osName"] != "Ubuntu 24.04" {
		t.Errorf("the remembered report is gone: %v", pre)
	}

	f.s.Clock.Advance(machines.ProbeValid)
	relink()
	if p, s := probes(); p != 2 || s != 2 {
		t.Errorf("a link back after the interval: %d preflights, %d agents statuses, want two of each", p, s)
	}
}
