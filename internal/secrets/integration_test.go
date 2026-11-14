package secrets

import (
	"path/filepath"
	"testing"
)

func TestIntegrationSecretsStayOutOfTheList(t *testing.T) {
	dir := t.TempDir()
	s := Open(filepath.Join(dir, "safe"), filepath.Join(dir, "key"))
	if err := s.PutIntegration("chatgpt", "accounts", `{"x":1}`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Put("github-token", "ghp_x"); err != nil {
		t.Fatal(err)
	}
	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Name != "github-token" {
		t.Fatalf("list %+v", list)
	}
	if v, err := s.RevealIntegration("chatgpt", "accounts"); err != nil || v != `{"x":1}` {
		t.Fatalf("reveal %q %v", v, err)
	}
	// A plain name with a slash is no way in: the CLI's routes check ValidName.
	if ValidName(IntegrationName("chatgpt", "accounts")) {
		t.Fatal("an integration's name passes as a plain one")
	}
	// No app can name it.
	if IsAppName(IntegrationName("chatgpt", "accounts")) || AppName("chatgpt", "accounts") == IntegrationName("chatgpt", "accounts") {
		t.Fatal("an app reaches the integration's secret")
	}
	if IntegrationName("../x", "a") != "" || IntegrationName("chatgpt", "a/b") != "" {
		t.Fatal("a bad part passes")
	}
	if err := s.RemoveIntegration("chatgpt", "accounts"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RevealIntegration("chatgpt", "accounts"); ReasonOf(err) != ReasonNotFound {
		t.Fatalf("after remove: %v", err)
	}
}
