// Command harnessly is what an agent in a harnessly tab calls to show things in the person's right pane and to work with
// the workspace's other tabs. It is a client of the runtime's /api/cli routes.
package main

import (
	"os"

	"github.com/ly-solutions/harnessly/internal/designkit/kitadd"
	"github.com/ly-solutions/harnessly/internal/hlcli"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == kitadd.SandboxHelperArg {
		// The helper of a sandboxed kit build (see kitadd/sandbox.go): it only returns when it could not run the command.
		if err := kitadd.RunSandboxHelper(os.Args[2:]); err != nil {
			os.Stderr.WriteString(err.Error() + "\n")
		}
		os.Exit(3)
	}
	os.Exit(hlcli.Run(os.Args[1:], hlcli.OSEnv()))
}
