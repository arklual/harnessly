package main

import (
	"path/filepath"
	"testing"

	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// with the apps' permissions off an app with rules goes live in a running chat even where the deployment
// requires the wall, and its page shows no rights; the package's other tests run with them on (main_test.go).
func TestAppsPermissionsOff(t *testing.T) {
	apps.SetPermissions(false)
	t.Cleanup(func() { apps.SetPermissions(true) })
	reg := &apps.Registry{Root: filepath.Join(t.TempDir(), "apps")}
	installLiveApp(t, reg, "acme.ruled", "Строгое", `{"deny":["Bash"]}`)
	l := &liveApps{reg: reg, launched: func(string) []string { return nil }, requireWall: true}
	if !l.changeIsLive(wire.Conversation{ID: "c1", Session: "s1", Provider: wire.ProviderClaude}, []string{"acme.ruled"}) {
		t.Fatal("an app with rules goes live with the permissions off")
	}
	if why := l.hostRefusal("s1")("acme.ruled"); why != "" {
		t.Fatalf("no refusal: %q", why)
	}
	a, err := reg.Get("acme.ruled")
	if err != nil {
		t.Fatal(err)
	}
	v := viewOfApp(a, false)
	if !v.PermissionsOff || v.Access != nil || v.Granted != nil || v.Consent || v.Pending != nil {
		t.Fatalf("view: off=%v access=%v granted=%v consent=%v", v.PermissionsOff, v.Access, v.Granted, v.Consent)
	}
}
