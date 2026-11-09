// Package buildinfo holds what the linker puts into the hub binary.
package buildinfo

import (
	"os"
	"regexp"
	"strings"
)

// Version is set at link time: -ldflags "-X github.com/ly-solutions/harnessly/internal/hub/buildinfo.Version=1.2.3".
//
// The hub's image is built for a merge request before its release has a number (the release promotes that very image
// under the semver tag), so the link puts only "dev-<commit>" there. HUB_VERSION, set by the deployment from the
// image's tag, names the release then; a version set at link time wins over it.
var Version = "dev"

var version = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z.+_-]{0,63}$`)

func init() { Version = resolve(Version, os.Getenv("HUB_VERSION")) }

// resolve is the version the hub says: the linked one, unless it is a development build and HUB_VERSION names one.
func resolve(linked, env string) string {
	env = strings.TrimSpace(env)
	if (linked == "dev" || strings.HasPrefix(linked, "dev-")) && version.MatchString(env) {
		return env
	}
	return linked
}
