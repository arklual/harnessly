package typist

import (
	"os"
	"testing"
)

// «Switch model?» is numbered Yes/No like a permission prompt, but it is the typist's own question to answer: read as a
// permission it opened a gate nobody could close and sent the chat's state to working with no Stop to bring it back.
func TestLaunchConfirmIsNotAPermission(t *testing.T) {
	raw, err := os.ReadFile("testdata/tui/claude-switch-model-confirm.txt")
	if err != nil {
		t.Fatal(err)
	}
	if got := Read(string(raw), "claude"); got.Menu == MenuPermission || got.Menu == NoMenu {
		t.Fatalf("the model confirm is read as %q, want another menu", got.Menu)
	}
}
