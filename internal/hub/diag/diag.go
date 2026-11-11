// Package diag gives the hub's problems a code: what is wrong, what to do about it, and the section of docs/hub/doctor.md
// that explains it. `hub doctor` prints them, and so do the errors that stop the hub at start.
package diag

import (
	"errors"
	"fmt"
	"strings"
)

// DocsPath is the file the codes are explained in; each code is an anchor in it (its lower-case name).
const DocsPath = "docs/hub/doctor.md"

// Problem is an error with a code. Fix says what the operator does next.
type Problem struct {
	Code string
	What string
	Fix  string
	Err  error
}

func (p *Problem) Error() string {
	if p.Err != nil {
		return p.What + ": " + p.Err.Error()
	}
	return p.What
}

func (p *Problem) Unwrap() error { return p.Err }

// Link is where the code is explained.
func (p *Problem) Link() string { return Link(p.Code) }

// Link is the documentation address of a code.
func Link(code string) string { return DocsPath + "#" + strings.ToLower(code) }

// Format prints the problem as the operator reads it: the code and the cause, what to do, and where to read more.
func (p *Problem) Format() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s  %s\n", p.Code, p.Error())
	if p.Fix != "" {
		fmt.Fprintf(&b, "  what to do: %s\n", p.Fix)
	}
	fmt.Fprintf(&b, "  docs:       %s\n", p.Link())
	return b.String()
}

// Describe returns the text for any error: a Problem prints in full; another error is wrapped as the generic start failure.
func Describe(err error) string {
	var p *Problem
	if errors.As(err, &p) {
		return p.Format()
	}
	return (&Problem{Code: StartFailed, What: err.Error(), Fix: "read the message above; if it names a variable, check it with `hub check-config`"}).Format()
}

// The codes. They are part of the documentation: never renumber, add new ones at the end of their group.
const (
	// Configuration and address.
	ConfigInvalid  = "HUB-C001" // a variable is missing or wrong
	AddressInvalid = "HUB-A001" // HUB_PUBLIC_URL or HUB_AGENT_URL is not a usable address
	AddressUnreach = "HUB-A002" // the public address does not reach this hub
	BaseNoRuntime  = "HUB-A003" // a base path without HUB_RUNTIME_DOMAIN: machine pages do not work
	ProxyNoTrusted = "HUB-P001" // https address and no HUB_TRUSTED_PROXIES
	ProxyWebSocket = "HUB-P002" // the proxy does not pass WebSocket upgrades
	ClockSkew      = "HUB-T001" // the clock differs from the proxy's / the world's
	// Database.
	DBUnreachable   = "HUB-D001"
	MigrationsLeft  = "HUB-D002" // migrations not applied (the hub applies them at start)
	MigrationsNewer = "HUB-D003" // the database was written by a newer hub
	// Storage and sign-in.
	S3Unreachable   = "HUB-S001"
	OIDCUnreachable = "HUB-O001"
	// Extensions.
	ExtensionDown  = "HUB-E001"
	AppServiceDown = "HUB-E002" // the documents or boards service does not answer /readyz
	// The move of the machines' data into the store (docs/migrations).
	MoveFailed = "HUB-M001" // a machine's move stopped on objects with errors
	// Start.
	StartFailed  = "HUB-X001" // any other error that stopped the hub
	PortBusy     = "HUB-X002" // HUB_LISTEN is taken or not allowed
	DataDirError = "HUB-X003" // HUB_DATA_DIR cannot be made or written
)
