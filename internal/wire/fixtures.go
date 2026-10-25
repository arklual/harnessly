package wire

import "embed"

// Fixtures are the shared fixtures of testdata, for the fake runtime (wirefake) a page's end-to-end tests run on.
//
//go:embed testdata/*.json
var Fixtures embed.FS
