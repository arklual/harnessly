// Package docs carries the documentation in the binary, so a machine with no source checkout and no network has it: the
// `harnessly app docs` command reads it for an agent, the runtime's page shows it to a person.
package docs

import "embed"

// Guide holds the guide for authors of apps (build-apps/) and the reference it leans on, with the example packages, and
// the Russian translation (ru/build-apps/, ru/apps.md) under the same file names. A page with no translation is read in
// English (pages.go). The `all:` prefix keeps ru/ embeddable while it holds only a placeholder.
//
//go:embed all:build-apps apps.md all:apps all:ru
var Guide embed.FS
