package main

import "testing"

// The events are followed unless the variable is exactly 0: that is the way back to the poll without a release.
func TestTmuxEventsSwitch(t *testing.T) {
	for value, want := range map[string]bool{"": true, "1": true, "on": true, "0": false} {
		if got := tmuxEventsOn(func(string) string { return value }); got != want {
			t.Errorf("%s=%q: on=%v", tmuxEventsEnv, value, got)
		}
	}
}
