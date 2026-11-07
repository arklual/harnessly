package docprovider

import (
	"encoding/base32"
	"regexp"
	"strings"
)

// Local aliases of external IDs (GitHub issue 27). The runtime's routes take a document by a slug of lowercase letters,
// digits and dashes, while an adapter's service may name its documents with case-sensitive or base64url IDs. An adapter
// keeps its own IDs; the provider shows each one to the runtime as an alias and turns the alias back into the ID on
// the way to the adapter, with no table to keep:
//
//   - an ID that already is a slug and does not start with "x-" is its own alias;
//   - any other ID is "x-" and its bytes in lowercase base32 without padding.
//
// The two forms do not overlap, so Alias and ExternalID are each other's inverse. The viewer address stays the one the
// adapter gave (Summary.URL); nothing is made up from the alias.

const aliasPrefix = "x-"

// MaxExternalID is the longest external ID that has an alias (its alias stays within the runtime's slug limit).
const MaxExternalID = 72

var (
	plainIDRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,120}$`)
	aliasCodec = base32.StdEncoding.WithPadding(base32.NoPadding)
)

// Alias is the runtime's name of an external ID, "" when the ID is empty or too long to have one.
func Alias(id string) string {
	if id == "" || len(id) > MaxExternalID {
		return ""
	}
	if plainIDRe.MatchString(id) && !strings.HasPrefix(id, aliasPrefix) {
		return id
	}
	return aliasPrefix + strings.ToLower(aliasCodec.EncodeToString([]byte(id)))
}

// ExternalID is the adapter's ID of an alias; ok is false for a string that is no alias (the caller answers not found).
func ExternalID(alias string) (string, bool) {
	if !strings.HasPrefix(alias, aliasPrefix) {
		return alias, plainIDRe.MatchString(alias)
	}
	b, err := aliasCodec.DecodeString(strings.ToUpper(strings.TrimPrefix(alias, aliasPrefix)))
	if err != nil || len(b) == 0 || len(b) > MaxExternalID || Alias(string(b)) != alias {
		return "", false
	}
	return string(b), true
}
