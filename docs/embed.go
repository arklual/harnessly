// Package docs carries the documentation in the binary, so a machine with no source checkout and no network has it: the
// `harnessly docs` and `harnessly app docs` commands read it for an agent, the runtime's /help page shows it to a person.
package docs

import "embed"

// Guide holds the whole built-in library (library.go): the sections (each folder with a section.json: start, agents,
// inside, self-host, build-apps), the repository's own pages they list (*.md, hub/), their pictures, the example packages
// of the apps guide, and the Russian translations under ru/ with the same file names. A page with no translation is read
// in English.
//
//go:embed *.md all:hub all:build-apps all:apps all:ru all:start all:agents all:inside all:self-host
var Guide embed.FS
