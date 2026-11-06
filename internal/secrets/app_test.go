package secrets

import (
	"path/filepath"
	"testing"
)

func TestAppSecretsAreTheAppsOwn(t *testing.T) {
	s := Open(t.TempDir(), filepath.Join(t.TempDir(), "k"))
	if err := s.PutApp("acme.pay", "api-key", "v1-value"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Put("plain", "p"); err != nil {
		t.Fatal(err)
	}
	// A plain name cannot be an app's: the routes that take names (use, file, ask) refuse the slash.
	if ValidName(AppName("acme.pay", "api-key")) {
		t.Fatal("an app's secret must not be a valid plain name")
	}
	if _, _, err := s.Put(AppName("acme.pay", "api-key"), "x"); ReasonOf(err) != ReasonBadName {
		t.Fatalf("a plain put of an app name: %v", err)
	}
	views, _ := s.List()
	if len(views) != 1 || views[0].Name != "plain" {
		t.Fatalf("the safe's list shows only its own: %+v", views)
	}
	if !s.HasApp("acme.pay", "api-key") || s.HasApp("acme.other", "api-key") {
		t.Fatal("HasApp")
	}
	got, err := s.RevealApp("acme.pay", []string{"api-key"})
	if err != nil || got["api-key"] != "v1-value" {
		t.Fatalf("reveal: %v %v", got, err)
	}
	// A new value replaces the old one with no other step.
	if err := s.PutApp("acme.pay", "api-key", "v2-value"); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.RevealApp("acme.pay", []string{"api-key"}); got["api-key"] != "v2-value" {
		t.Fatalf("replaced: %v", got)
	}
	if _, err := s.RevealApp("acme.other", []string{"api-key"}); ReasonOf(err) != ReasonNotFound {
		t.Fatalf("another app's name: %v", err)
	}
	if err := s.RemoveApp("acme.pay", "api-key"); err != nil || s.HasApp("acme.pay", "api-key") {
		t.Fatalf("remove: %v", err)
	}
	if err := s.PutApp("../x", "k", "v"); ReasonOf(err) != ReasonBadName {
		t.Fatalf("bad app id: %v", err)
	}
}
