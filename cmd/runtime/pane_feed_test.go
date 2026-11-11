package main

import "testing"

// The pane's data goes out unless the switch says 0: a machine goes back to the page's own reading with one variable.
func TestPaneFeedSwitch(t *testing.T) {
	for value, on := range map[string]bool{"": true, "1": true, "0": false} {
		got := paneLinks(func(k string) string {
			if k == paneFeedEnv {
				return value
			}
			return ""
		}) != nil
		if got != on {
			t.Fatalf("%s=%q: on=%v, want %v", paneFeedEnv, value, got, on)
		}
	}
}
