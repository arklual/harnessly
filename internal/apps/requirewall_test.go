package apps

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A deployment that requires the wall (RUNTIME_APPS_REQUIRE_WALL) does not start a chat with apps where the wall cannot
// be made, and says why and how to put it right; without the flag the chat starts without it, as before (HRN-798).
func TestLauncherRequireWall(t *testing.T) {
	data := t.TempDir()
	reg := &Registry{Root: filepath.Join(data, "apps")}
	pkg := filepath.Join(t.TempDir(), "review")
	if err := Scaffold(pkg, "acme.review", "Ревью", "chat"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := reg.Install(pkg, InstallOptions{Dev: true, HostVersion: "0.17.0"}); err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(t.TempDir(), "hooks.json")
	if err := os.WriteFile(base, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	noBwrap := func() *Boundary { return &Boundary{} }

	l := &Launcher{Registry: reg, GuardCommand: "/bin/harnessly hook app-guard", Boundary: noBwrap()}
	got, err := l.Claude([]string{"acme.review"}, base)
	if err != nil || got.Wrap != "" {
		t.Fatalf("without the flag: wrap %q, err %v", got.Wrap, err)
	}

	l = &Launcher{Registry: reg, GuardCommand: "/bin/harnessly hook app-guard", Boundary: noBwrap(), RequireWall: true}
	_, err = l.Claude([]string{"acme.review"}, base)
	var req ErrWallRequired
	if !errors.As(err, &req) || !errors.Is(err, ErrNoBwrap) {
		t.Fatalf("required, no bwrap: %v", err)
	}
	if msg := err.Error(); !strings.Contains(msg, "apt-get install -y bubblewrap") || !strings.Contains(msg, "RUNTIME_APPS_REQUIRE_WALL") {
		t.Fatalf("message: %s", msg)
	}

	userns := ErrWallRequired{Cause: errors.New("bwrap: setting up uid map: Permission denied")}
	if !strings.Contains(userns.Error(), "unprivileged_userns") {
		t.Fatalf("userns message: %s", userns.Error())
	}
	l = &Launcher{Registry: reg, GuardCommand: "/bin/harnessly hook app-guard", RequireWall: true}
	if _, err := l.Claude([]string{"acme.review"}, base); !errors.As(err, &req) {
		t.Fatalf("required, no boundary: %v", err)
	}
}
