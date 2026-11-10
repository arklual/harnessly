package apps

import "strings"

// LoadCheck is what `harnessly app check` and the agent's app_check tool answer: did the app's view load in
// a browser of the machine, how long its handshake took, and every line the load wrote to the app's log.
type LoadCheck struct {
	App  string `json:"app"`
	View string `json:"view,omitempty"`
	// State is ok (the handshake came and nothing failed), failed (a failure line, or no handshake), waiting (the app
	// waits for the person's consent), off (turned off) or unreachable (no browser, or the page did not open).
	State     string        `json:"state"`
	Handshake bool          `json:"handshake"`
	Ms        int64         `json:"ms,omitempty"`
	Errors    int           `json:"errors"`
	Problem   string        `json:"problem,omitempty"`
	Lines     []ViewLogLine `json:"lines"`
}

// The states of a check.
const (
	CheckOK          = "ok"
	CheckFailed      = "failed"
	CheckWaiting     = "waiting"
	CheckOff         = "off"
	CheckUnreachable = "unreachable"
)

// OK says whether the view loaded cleanly.
func (c LoadCheck) OK() bool { return c.State == CheckOK }

// Judge fills a result from the lines a load wrote (those since its start): the handshake, the time, the failures.
func Judge(app, view string, lines []ViewLogLine) LoadCheck {
	r := LoadCheck{App: app, View: view, State: CheckOK, Lines: lines}
	if r.Lines == nil {
		r.Lines = []ViewLogLine{}
	}
	var opened int64
	for _, l := range lines {
		if l.Level == ViewLogLoad && strings.HasPrefix(l.Text, "check page opened") {
			opened = l.At
		}
		if l.Level == ViewLogLoad && strings.HasPrefix(l.Text, "handshake ok") {
			r.Handshake = true
			if opened > 0 {
				r.Ms = l.At - opened
			}
		}
		if ViewLogFailure(l) {
			r.Errors++
		}
	}
	switch {
	case !r.Handshake:
		r.State, r.Problem = CheckFailed, "the view did not answer the handshake: it never became ready"
	case r.Errors > 0:
		r.State, r.Problem = CheckFailed, "the view loaded, with errors"
	}
	return r
}
