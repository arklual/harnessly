package apps

import (
	"errors"
	"slices"
	"testing"
	"time"
)

// seedAccess installs a small app and gives it a grant of every kind of key: rights, network, frames, the
// app's own servers, secrets and parts of a plugin.
func seedAccess(t *testing.T, id string, keys []string) *Registry {
	t.Helper()
	r := &Registry{Root: t.TempDir()}
	if _, _, err := r.Install(pkgWith(t, id, "1.0.0", "notify"), InstallOptions{Consent: ConsentGiven}); err != nil {
		t.Fatal(err)
	}
	list, err := r.read()
	if err != nil {
		t.Fatal(err)
	}
	list[0].Wants = slices.Clone(keys)
	list[0].Grant = &Grant{Keys: slices.Clone(keys), At: 1}
	if err := r.write(list); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRevokeEachKindOfAccess(t *testing.T) {
	keys := []string{"perm:notify", "net:api.example.com", "frame:app.example.com", "server:docs", "secret:TOKEN", "part:hooks/"}
	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			const id = "example.revoke"
			r := seedAccess(t, id, keys)
			if a, err := r.Find(id); err != nil || a.NeedsConsent() {
				t.Fatalf("granted everything it wants must run: %v", err)
			}
			got, err := r.Revoke(id, key, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if slices.Contains(got.Grant.Keys, key) || len(got.Grant.Keys) != len(keys)-1 {
				t.Fatalf("only %s must leave the grant: %v", key, got.Grant.Keys)
			}
			// It is kept: a fresh read of the registry sees the same, and the app waits for the person's word.
			again, err := (&Registry{Root: r.Root}).Find(id)
			if err != nil {
				t.Fatal(err)
			}
			if slices.Contains(again.Grant.Keys, key) || !again.NeedsConsent() {
				t.Fatalf("the revoke must stay and keep the app off: %v", again.Grant.Keys)
			}
			if _, err := r.Get(id); err == nil {
				t.Fatal("an app that lost an access must not be reached")
			}
			if _, err := r.Revoke(id, key, time.Now()); !errors.Is(err, ErrNotGranted) {
				t.Fatalf("a second revoke: %v", err)
			}
			// Agreeing again (the screen lists everything the app reaches) gives it back.
			back, err := r.Consent(id, "1.0.0", keys, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(back.Grant.Keys, key) || back.NeedsConsent() {
				t.Fatalf("agreed again: %v", back.Grant.Keys)
			}
			if _, err := r.Get(id); err != nil {
				t.Fatalf("the app runs again: %v", err)
			}
		})
	}
}

func TestRevokeFromAnInstallWithoutGrant(t *testing.T) {
	r := &Registry{Root: t.TempDir()}
	if _, _, err := r.Install(pkgWith(t, "example.host", "1.0.0", "notify"), InstallOptions{Consent: ConsentHost}); err != nil {
		t.Fatal(err)
	}
	got, err := r.Revoke("example.host", "perm:notify", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got.Grant == nil || len(got.Grant.Keys) != 0 || !got.NeedsConsent() {
		t.Fatalf("a host's install takes the access back too: %+v", got.Grant)
	}
	if _, err := r.Revoke("example.none", "perm:notify", time.Now()); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("not installed: %v", err)
	}
}
