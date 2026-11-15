//go:build !linux

package agenteval

import "errors"

// SandboxAvailable is 0 off Linux: no Landlock.
func SandboxAvailable() int { return 0 }

// SandboxExec is refused off Linux.
func SandboxExec(rw, ro, argv []string) error {
	return errors.New("sandbox: only on Linux (Landlock)")
}
