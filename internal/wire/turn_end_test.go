package wire

import "testing"

// Only an ended turn has a terminal event: a live one gets none, so no turn_finished with status live reaches a page
// (GitHub #118); each event it does give passes its own validation.
func TestTurnEndEv(t *testing.T) {
	for st, want := range map[TurnStatus]EventType{TurnDone: EvTurnFinished, TurnInterrupted: EvTurnFinished, TurnFailed: EvTurnFailed, TurnLive: ""} {
		ev, ok := TurnEndEv(Turn{Status: st})
		if want == "" {
			if ok || ev != nil {
				t.Fatalf("%s: %v", st, ev)
			}
			continue
		}
		if !ok || ev.EventType() != want {
			t.Fatalf("%s: %v %v", st, ev, ok)
		}
	}
}
