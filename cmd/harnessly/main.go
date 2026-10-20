// Command harnessly is what an agent in a harnessly tab calls to show things in the person's right pane and to work with
// the workspace's other tabs. It is a client of the runtime's /api/cli routes.
package main

import (
	"os"

	"github.com/ly-solutions/harnessly/internal/hlcli"
)

func main() {
	os.Exit(hlcli.Run(os.Args[1:], hlcli.OSEnv()))
}
