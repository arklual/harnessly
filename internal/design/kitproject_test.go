package design

import (
	"strings"
	"testing"
)

func TestKitSeedWithoutPinIsUnchanged(t *testing.T) {
	l := testLibrary(t, t.TempDir())
	seed := []byte(`<script src="./support.js"></script><main class="board"></main>`)
	if got := l.kitSeed("nope", seed); string(got) != string(seed) {
		t.Fatalf("seed changed without a pin: %s", got)
	}
}

func TestKitBriefEmptyWithoutKitsAndPin(t *testing.T) {
	l := testLibrary(t, t.TempDir())
	if b := l.KitBrief("nope"); strings.Contains(b, "design check") {
		t.Fatalf("brief for a project without a kit: %q", b)
	}
}
