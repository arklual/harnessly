// Package install holds the machine installer the hub serves at /install (docs/hub/machine.md).
package install

import _ "embed"

// Script is install.sh. The hub serves it with HARNESSLY_HUB_URL (and HARNESSLY_IMAGE when it has a machine image)
// assigned on the lines right after the first one.
//
//go:embed install.sh
var Script []byte
