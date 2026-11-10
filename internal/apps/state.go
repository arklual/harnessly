package apps

import "strings"

// The state of an installed app with its reason (HRN-693): the person sees why an app does nothing, not only that it
// is installed.
const (
	StateReady    = "ready"
	StateWaiting  = "waiting"
	StateFailed   = "failed"
	StateDisabled = "disabled"
)

// Reasons of a state other than ready.
const (
	ReasonOff        = "off"        // the person or the environment turned it off
	ReasonSafe       = "safe"       // safe mode is on: the app does not start in new chats
	ReasonSecret     = "secret"     // a required secret has no value; Detail names them
	ReasonConnection = "connection" // a connection its servers need is not made or not signed in; Detail names them
	ReasonHost       = "host"       // this harnessly is outside the range the app wants; Detail is the range
	ReasonPackage    = "package"    // its installed copy cannot be read; Detail says why
	ReasonConsent    = "consent"    // it waits for the person to agree to what it reaches (consent.go)
)

// AppState is an app's state as the page shows it.
type AppState struct {
	State  string `json:"state"`
	Reason string `json:"reason,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// StateOf is the state of an install: off first, then a copy that cannot be read, then what it waits for.
func StateOf(a Installed, host string, packageErr error, secrets, connections []string) AppState {
	return StateIn(a, false, host, packageErr, secrets, connections)
}

// StateIn is StateOf with safe mode: on, every app but a pinned one is disabled.
func StateIn(a Installed, safe bool, host string, packageErr error, secrets, connections []string) AppState {
	switch {
	case a.Muted || a.Off:
		return AppState{State: StateDisabled, Reason: ReasonOff}
	case a.NeedsConsent():
		// The detail names what waits (HRN-913): the codes of the manifest, as the author wrote them.
		return AppState{State: StateWaiting, Reason: ReasonConsent, Detail: strings.Join(AccessCodes(a.WaitingAccess()), ", ")}
	case safe && !SafeAllows(a):
		return AppState{State: StateDisabled, Reason: ReasonSafe}
	case a.Manifest == nil:
		return AppState{State: StateFailed, Reason: ReasonPackage, Detail: "no manifest"}
	case packageErr != nil:
		return AppState{State: StateFailed, Reason: ReasonPackage, Detail: packageErr.Error()}
	}
	if host != "" {
		if rng, err := ParseRange(a.Manifest.Harnessly); err == nil && !rng.Allows(host) {
			return AppState{State: StateWaiting, Reason: ReasonHost, Detail: a.Manifest.Harnessly}
		}
	}
	if len(secrets) > 0 {
		return AppState{State: StateWaiting, Reason: ReasonSecret, Detail: strings.Join(secrets, ", ")}
	}
	if len(connections) > 0 {
		return AppState{State: StateWaiting, Reason: ReasonConnection, Detail: strings.Join(connections, ", ")}
	}
	return AppState{State: StateReady}
}
