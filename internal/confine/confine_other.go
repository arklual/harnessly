//go:build !linux

package confine

import "errors"

// Available is 0 off Linux: no Landlock.
func Available() int { return 0 }

type Net int

const (
	NetOpen Net = iota
	NetPorts
	NetNone
)

type Rules struct {
	RW, RO []string
	Net    Net
	Ports  []uint16
}

// Exec is refused off Linux.
func Exec(Rules, []string) error { return errors.New("sandbox: only on Linux (Landlock)") }

// SandboxExec is refused off Linux.
func SandboxExec(rw, ro, argv []string) error { return Exec(Rules{}, argv) }
