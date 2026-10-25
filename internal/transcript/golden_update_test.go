package transcript

import (
	"os"
	"testing"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// updatingGolden is HARNESSLY_UPDATE_GOLDEN=1: a golden test writes what this code answers into its golden file, in the
// file's own format, instead of comparing. Run it when the answers change on purpose, and read the diff.
func updatingGolden() bool { return os.Getenv("HARNESSLY_UPDATE_GOLDEN") != "" }

// writeGolden writes doc as the generator did: JSON.stringify(doc, null, 1) and a newline.
func writeGolden(t *testing.T, path string, doc any) {
	t.Helper()
	if err := os.WriteFile(path, append(jsvalue.Stringify(doc, " "), '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}
