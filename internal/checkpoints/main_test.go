package checkpoints

import (
	"os"
	"syscall"
	"testing"
)

// The goldens hold file modes as the old runtime made them under the fleet's umask 077; a runner with another umask
// would make other modes.
func TestMain(m *testing.M) {
	syscall.Umask(0o077)
	// The goldens are the previous runtime's answers, which were in Russian.
	os.Setenv("RUNTIME_LOCALE", "ru")
	os.Exit(m.Run())
}
