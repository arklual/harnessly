package design

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// Listing is the first contact on a clean account, before any preview server runs.
func TestCLIListProvidesReadableGuide(t *testing.T) {
	root := t.TempDir()
	l := New(Config{LibraryDir: root, Texts: Texts{Tag: "eval", CLI: "harnessly"}})
	a := pagesAPI(l)
	ans := a.CLIDesign(httptest.NewRequest(http.MethodGet, "/api/cli/design", nil))
	if ans.Status != http.StatusOK {
		t.Fatalf("list: %d", ans.Status)
	}
	raw, err := json.Marshal(ans.Body)
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Rules string `json:"rules"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	guide, err := os.ReadFile(body.Rules)
	if err != nil {
		t.Fatalf("advertised guide is not readable: %v", err)
	}
	if string(guide) != l.Guide() {
		t.Fatal("list did not publish the product guide")
	}
	if l.ServerPort() != 0 {
		t.Fatal("listing should not start a preview server")
	}
	// Preserve a person's edits instead of rewriting the instructions on every list.
	const custom = "My library rules\n"
	if err := os.WriteFile(body.Rules, []byte(custom), 0600); err != nil {
		t.Fatal(err)
	}
	ans = a.CLIDesign(httptest.NewRequest(http.MethodGet, "/api/cli/design", nil))
	if ans.Status != http.StatusOK {
		t.Fatalf("second list: %d", ans.Status)
	}
	guide, err = os.ReadFile(body.Rules)
	if err != nil || string(guide) != custom {
		t.Fatalf("custom guide overwritten: %q, %v", guide, err)
	}
}
