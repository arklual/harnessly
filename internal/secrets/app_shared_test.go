package secrets_test

import (
	"path/filepath"
	"testing"

	"github.com/ly-solutions/harnessly/internal/secrets"
	"github.com/ly-solutions/harnessly/internal/secrets/secretstest"
)

// an app's token put on one machine is there on every other machine of the person.
func TestAppSecretReachesEveryMachine(t *testing.T) {
	kv := secretstest.New()
	machine := func() *secrets.Store {
		s := secrets.Open(t.TempDir(), filepath.Join(t.TempDir(), "k"))
		s.SetShared(secrets.NewRemote(kv, "me", t.TempDir()))
		return s
	}
	a, b := machine(), machine()
	if err := a.PutApp("acme.inbox", "token", "t1"); err != nil {
		t.Fatal(err)
	}
	if !b.HasApp("acme.inbox", "token") {
		t.Fatal("the other machine does not see the app's secret")
	}
	got, err := b.RevealApp("acme.inbox", []string{"token"})
	if err != nil || got["token"] != "t1" {
		t.Fatalf("reveal on the other machine: %v %v", got, err)
	}
	// A new value on b wins on a too.
	if err := b.PutApp("acme.inbox", "token", "t2"); err != nil {
		t.Fatal(err)
	}
	if got, _ := a.RevealApp("acme.inbox", []string{"token"}); got["token"] != "t2" {
		t.Fatalf("a keeps the stale value: %v", got)
	}
	// The person's list never shows it.
	if names, _ := secrets.NewRemote(kv, "me", t.TempDir()).List(); len(names) != 0 {
		t.Fatalf("an app's secret is in the person's list: %v", names)
	}
	if err := a.RemoveApp("acme.inbox", "token"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.RevealApp("acme.inbox", []string{"token"}); err == nil {
		t.Fatal("a token removed on one machine came back from another's copy")
	}
	if b.HasApp("acme.inbox", "token") {
		t.Fatal("the other machine still keeps the removed token")
	}
}

// A token that only lived in one machine's file (before HRN-1717) goes up on its next use.
func TestAppSecretOfTheFileGoesUp(t *testing.T) {
	kv := secretstest.New()
	old := secrets.Open(t.TempDir(), filepath.Join(t.TempDir(), "k"))
	if err := old.PutApp("acme.inbox", "token", "t0"); err != nil {
		t.Fatal(err)
	}
	old.SetShared(secrets.NewRemote(kv, "me", t.TempDir()))
	if _, err := old.RevealApp("acme.inbox", []string{"token"}); err != nil {
		t.Fatal(err)
	}
	other := secrets.Open(t.TempDir(), filepath.Join(t.TempDir(), "k"))
	other.SetShared(secrets.NewRemote(kv, "me", t.TempDir()))
	if got, err := other.RevealApp("acme.inbox", []string{"token"}); err != nil || got["token"] != "t0" {
		t.Fatalf("the file's token did not go up: %v %v", got, err)
	}
}
