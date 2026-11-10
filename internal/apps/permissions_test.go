package apps

import (
	"path/filepath"
	"strings"
	"testing"
)

// permissionsOff turns the apps' permissions off for one test (the package's tests run with them on, TestMain).
func permissionsOff(t *testing.T) {
	t.Helper()
	SetPermissions(false)
	t.Cleanup(func() { SetPermissions(true) })
}

const permManifest = `{"id":"acme.cash","name":"Кэшбэк","description":"x","version":"0.1.0","harnessly":">=0.16.0","trust":"sandboxed",
"targets":["chat","pane"],"permissions":["storage"],
"agent":{"instructions":"instructions.md","tools":{"deny":["Bash"]}},
"views":[{"id":"main","title":"x","entry":"ui/index.html","targets":["pane"]}]}`

func permFiles() map[string]string {
	return map[string]string{"instructions.md": "x", "ui/index.html": "<p>x</p>"}
}

// with the permissions off an app installed by nobody's click works at once, its view reaches what its
// manifest lists and nothing more, its rules for the agent's tools are not taken by a launch, and it goes live in a running chat.
func TestPermissionsOff(t *testing.T) {
	permissionsOff(t)
	pkg := writeApp(t, permManifest, permFiles())
	reg := &Registry{Root: filepath.Join(t.TempDir(), "apps")}
	a, _, err := reg.Install(pkg.Dir, InstallOptions{Consent: ConsentAsk, HostVersion: "0.17.0"})
	if err != nil {
		t.Fatal(err)
	}
	if a.NeedsConsent() || a.WaitsConsent() {
		t.Fatal("an install waits for no consent with the permissions off")
	}
	if on, err := reg.List(); err != nil || len(on) != 1 {
		t.Fatalf("the app runs: %v %v", on, err)
	}
	if err := pkg.Manifest.Authorize("harnessly/storage.get", TargetPane, map[string]any{}); err != nil {
		t.Fatalf("a right the manifest lists is held: %v", err)
	}
	// The rights a manifest lists still hold with the permissions off: pane.open is not in it.
	if err := pkg.Manifest.Authorize("harnessly/pane.open", TargetPane, map[string]any{"target": "chat:c1"}); err == nil ||
		!strings.Contains(err.Error(), "needs the permission") {
		t.Fatalf("a right the manifest does not list is refused: %v", err)
	}
	if toolsOf(pkg.Manifest) != nil {
		t.Fatal("a launch takes no rules for the agent's tools")
	}
	if needs := LaunchNeeds(pkg); needs != "" {
		t.Fatalf("an app with rules goes live: %q", needs)
	}
	l := &Launcher{Registry: reg, RequireWall: true}
	if words, err := l.wall([]string{"acme.cash"}, nil, false, nil); err != nil || words != "" {
		t.Fatalf("no wall is required: %q %v", words, err)
	}
	// The switch writes no grant: with the permissions back on the install waits for the person.
	SetPermissions(true)
	if a, err := reg.Find("acme.cash"); err != nil || !a.NeedsConsent() {
		t.Fatal("an install made with the permissions off waits once they are on")
	}
}

// The same with the permissions on: the person's word, the manifest's rights, the rules and the required wall hold.
func TestPermissionsOn(t *testing.T) {
	pkg := writeApp(t, permManifest, permFiles())
	reg := &Registry{Root: filepath.Join(t.TempDir(), "apps")}
	a, _, err := reg.Install(pkg.Dir, InstallOptions{Consent: ConsentAsk, HostVersion: "0.17.0"})
	if err != nil {
		t.Fatal(err)
	}
	if !a.NeedsConsent() {
		t.Fatal("an install nobody clicked through waits")
	}
	err = pkg.Manifest.Authorize("harnessly/pane.open", TargetPane, map[string]any{"target": "chat:c1"})
	if err == nil || !strings.Contains(err.Error(), "needs the permission") {
		t.Fatalf("a call needs its right: %v", err)
	}
	if toolsOf(pkg.Manifest) == nil || !strings.Contains(LaunchNeeds(pkg), "rules for the agent's tools") {
		t.Fatal("the rules come with a launch")
	}
	l := &Launcher{Registry: reg, RequireWall: true}
	if _, err := l.wall([]string{"acme.cash"}, nil, false, nil); err == nil {
		t.Fatal("a required wall the runtime cannot make refuses the launch")
	}
}

// An install that waited for the person's word while the permissions were on runs once they are off, and waits again
// when they come back on: the switch writes no grant.
func TestPermissionsOffRunsWaiting(t *testing.T) {
	pkg := writeApp(t, permManifest, permFiles())
	reg := &Registry{Root: filepath.Join(t.TempDir(), "apps")}
	if _, _, err := reg.Install(pkg.Dir, InstallOptions{Consent: ConsentAsk, HostVersion: "0.17.0"}); err != nil {
		t.Fatal(err)
	}
	if on, _ := reg.List(); len(on) != 0 {
		t.Fatal("it waits while the permissions are on")
	}
	SetPermissions(false)
	on, _ := reg.List()
	SetPermissions(true)
	if len(on) != 1 {
		t.Fatal("it runs with the permissions off")
	}
	if on, _ := reg.List(); len(on) != 0 {
		t.Fatal("it waits again with the permissions back on")
	}
}
