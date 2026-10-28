// Command seal seals a value to a machine the way the hub's page does (docs/hub/api.md, "Sealed envelope"), for
// tools/hub-e2e.sh: the value is read from standard input, the envelope is printed as JSON.
//
//	printf %s "$value" | go run ./tools/hub-e2e/seal <machine encKey, base64> <machineId> <purpose>
//
// purpose is "key:<provider>" for an API key and "env:<NAME>" for a secret.
package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

func main() {
	if len(os.Args) != 4 {
		fmt.Fprintln(os.Stderr, "usage: seal <encKey base64> <machineId> <purpose> < value")
		os.Exit(2)
	}
	encKey, err := base64.StdEncoding.DecodeString(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "encKey is not base64:", err)
		os.Exit(2)
	}
	value, err := io.ReadAll(io.LimitReader(os.Stdin, hubproto.MaxSealed+1))
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot read the value:", err)
		os.Exit(1)
	}
	env, err := hubproto.Seal(encKey, os.Args[2], os.Args[3], value)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot seal:", err)
		os.Exit(1)
	}
	if err := json.NewEncoder(os.Stdout).Encode(env); err != nil {
		os.Exit(1)
	}
}
