// Package storerev is the version id of a store revision: 40 hex digits like a git commit, so every place that
// takes a version (the hub's and the gateway's links, --at) takes it as it is, and no git commit starts with so many
// zeros. It has no dependencies: the hub reads the ids without the runtime's design code.
package storerev

import (
	"fmt"
	"strconv"
	"strings"
)

const zeros = "000000000000000000000000"

// ID is the version id of a store revision.
func ID(rev int64) string { return fmt.Sprintf("%040x", rev) }

// Of reads a version id made by ID: the store's revision, false for a version of git.
func Of(id string) (int64, bool) {
	if len(id) != 40 || !strings.HasPrefix(id, zeros) {
		return 0, false
	}
	n, err := strconv.ParseInt(id[len(zeros):], 16, 64)
	return n, err == nil && n > 0
}
