package settings

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	// The goldens are the previous runtime's answers, which were in Russian.
	os.Setenv("RUNTIME_LOCALE", "ru")
	os.Exit(m.Run())
}
