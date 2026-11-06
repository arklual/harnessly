package tools

import (
	"os"
	"testing"
)

// skipWithoutChrome skips a test that drives Chrome on a machine without one (the GitLab runners' Go image has none).
// The public CI sets HARNESSLY_REQUIRE_CHROME=1, so there a missing Chrome fails the test instead of hiding it.
func skipWithoutChrome(t *testing.T, err error) {
	t.Helper()
	if os.Getenv("HARNESSLY_REQUIRE_CHROME") != "" {
		t.Fatal(err)
	}
	t.Skip(err)
}
