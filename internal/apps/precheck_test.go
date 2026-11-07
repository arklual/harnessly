package apps

import (
	"encoding/json"
	"testing"
)

// Every kind of refusal is found before the download, each with its own code; an update of the app itself is none.
func TestPrecheckEntry(t *testing.T) {
	deck := &Manifest{ID: "acme.deck", Activation: &Activation{On: []string{"command:/deck"}}}
	deck.Sinopia, _ = json.Marshal(Sinopia{Kinds: []SinopiaKind{{ID: "acme.deck.slides"}}})
	installed := []Installed{{ID: "acme.deck", Manifest: deck}, {ID: "a-b.c", Manifest: &Manifest{ID: "a-b.c"}}}
	codes := func(p Precheck) map[string]bool {
		out := map[string]bool{}
		for _, r := range p.Refusals {
			out[r.Code] = true
		}
		return out
	}
	ok := PrecheckEntry(CatalogEntry{ID: "acme.notes", Trust: TrustSandboxed, Harnessly: ">=0.16", Secrets: []string{"token"}}, "0.17.0", installed)
	if len(ok.Refusals) != 0 || len(ok.Secrets) != 1 {
		t.Fatalf("a fine entry: %+v", ok)
	}
	bad := PrecheckEntry(CatalogEntry{ID: "a.b-c", Trust: TrustTrusted, Harnessly: ">=0.99", Command: "/deck", Kinds: []string{"acme.deck.slides"}}, "0.17.0", installed)
	for _, c := range []string{RefuseTrust, RefuseHost, RefuseName, RefuseCommand, RefuseKind} {
		if !codes(bad)[c] {
			t.Errorf("missing %s: %+v", c, bad)
		}
	}
	update := PrecheckEntry(CatalogEntry{ID: "acme.deck", Command: "/deck", Kinds: []string{"acme.deck.slides"}}, "0.17.0", installed)
	if len(update.Refusals) != 0 {
		t.Fatalf("an update of the app itself: %+v", update)
	}
}
