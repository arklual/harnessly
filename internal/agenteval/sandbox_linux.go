//go:build linux

package agenteval

import "github.com/ly-solutions/harnessly/internal/confine"

// SandboxAvailable says whether Landlock confines a stand here (its ABI version; 0 for none).
func SandboxAvailable() int { return confine.Available() }

// SandboxExec confines this process (rw: read and write beneath; ro: read and run beneath) and becomes argv.
func SandboxExec(rw, ro, argv []string) error { return confine.SandboxExec(rw, ro, argv) }
