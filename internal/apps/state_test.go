package apps

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestStateOf(t *testing.T) {
	m := &Manifest{ID: "acme.a", Harnessly: ">=0.16"}
	cases := []struct {
		a      Installed
		host   string
		pkg    error
		s, c   []string
		state  string
		reason string
	}{
		{Installed{Manifest: m}, "0.17.0", nil, nil, nil, StateReady, ""},
		{Installed{Manifest: m, Muted: true}, "0.17.0", nil, []string{"key"}, nil, StateDisabled, ReasonOff},
		{Installed{Manifest: m}, "0.17.0", errors.New("gone"), nil, nil, StateFailed, ReasonPackage},
		{Installed{Manifest: m}, "0.15.0", nil, nil, nil, StateWaiting, ReasonHost},
		{Installed{Manifest: m}, "0.17.0", nil, []string{"key"}, []string{"jira"}, StateWaiting, ReasonSecret},
		{Installed{Manifest: m}, "0.17.0", nil, nil, []string{"jira"}, StateWaiting, ReasonConnection},
	}
	for i, c := range cases {
		got := StateOf(c.a, c.host, c.pkg, c.s, c.c)
		if got.State != c.state || got.Reason != c.reason {
			t.Errorf("%d: %+v", i, got)
		}
	}
}

// An update that fails its check leaves the installed version as it was and working.
func TestFailedUpdateKeepsThePreviousVersion(t *testing.T) {
	reg := Open(t.TempDir())
	pkg := filepath.Join(t.TempDir(), "demo")
	if err := Scaffold(pkg, "acme.demo", "Демо", "chat"); err != nil {
		t.Fatal(err)
	}
	first, _, err := reg.Install(pkg, InstallOptions{HostVersion: "0.17.0"})
	if err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(pkg, ManifestPath)
	raw, _ := os.ReadFile(manifest)
	bad := []byte(string(raw[:len(raw)-2]) + `,"permissions":["no.such.right"]}`)
	_ = os.WriteFile(manifest, bad, 0o600)
	if _, _, err := reg.Install(pkg, InstallOptions{HostVersion: "0.17.0"}); err == nil {
		t.Fatal("a broken update is refused")
	}
	now, err := reg.Get("acme.demo")
	if err != nil || now.Version != first.Version || now.Dir != first.Dir {
		t.Fatalf("after a failed update: %+v %v", now, err)
	}
	if _, err := reg.Package("acme.demo"); err != nil {
		t.Fatalf("the previous version still loads: %v", err)
	}
}
