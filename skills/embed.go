// Package skills carries the agent skills harnessly installs for Claude Code and Codex: the files of two upstream
// repositories at the commits pinned in SOURCES, vendored under upstream/ so an install works offline. sync.sh keeps the
// copy equal to the pins; MANIFEST.sha256 lets a test notice a hand edit.
package skills

import "embed"

// Upstream holds upstream/<repository>/<path> for every path of SOURCES.
//
//go:embed all:upstream SOURCES MANIFEST.sha256
var Upstream embed.FS
