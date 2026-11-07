package apps

import (
	"path/filepath"
	"slices"
	"testing"
)

// A launch keeps only the apps that are installed; in safe mode none but a pinned one.
func TestLauncherActiveAppsAndSafeMode(t *testing.T) {
	reg := Open(t.TempDir())
	pkg := filepath.Join(t.TempDir(), "demo")
	if err := Scaffold(pkg, "acme.demo", "Демо", "chat"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := reg.Install(pkg, InstallOptions{HostVersion: "0.17.0"}); err != nil {
		t.Fatal(err)
	}
	l := &Launcher{Registry: reg}
	if got := l.Active([]string{"acme.demo", "acme.removed"}); !slices.Equal(got, []string{"acme.demo"}) {
		t.Fatalf("a removed app is left out: %v", got)
	}
	if err := reg.SetSafeMode(true); err != nil || !reg.SafeMode() {
		t.Fatalf("safe mode on: %v", err)
	}
	if got := l.Active([]string{"acme.demo"}); len(got) != 0 {
		t.Fatalf("safe mode keeps no app of the person's: %v", got)
	}
	in, _ := reg.Get("acme.demo")
	if s := StateIn(in, true, "0.17.0", nil, nil, nil); s.State != StateDisabled || s.Reason != ReasonSafe {
		t.Fatalf("state in safe mode: %+v", s)
	}
	if !SafeAllows(Installed{Env: true, Locked: true}) || SafeAllows(Installed{Env: true}) {
		t.Error("only a pinned app of the environment works in safe mode")
	}
	_ = reg.SetSafeMode(false)
	if reg.SafeMode() || len(l.Active([]string{"acme.demo"})) != 1 {
		t.Fatal("safe mode off gives the app back")
	}
}
