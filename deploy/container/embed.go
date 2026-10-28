// Package container holds the entrypoint of the machine image: a script that registers the machine with its hub and
// runs the runtime and the agent side by side (docs/hub/machine.md).
package container

import _ "embed"

// Entrypoint is entrypoint.sh, for whoever builds an image from this repository without a file tree at hand.
//
//go:embed entrypoint.sh
var Entrypoint []byte
