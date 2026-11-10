package hlcli

import (
	"os"
	"testing"

	"github.com/ly-solutions/harnessly/internal/apps"
)

// The tests hold the apps' permissions as they are when on: the CLI's consent and refusals are checked so.
func TestMain(m *testing.M) {
	apps.SetPermissions(true)
	os.Exit(m.Run())
}
