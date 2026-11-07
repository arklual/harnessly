package apps

import "testing"

// A remote view is an https entry whose host network.frames lists; a package view or an unlisted host is none.
func TestRemoteView(t *testing.T) {
	m := &Manifest{ID: "acme.admin", Network: &Network{Frames: []string{"admin.acme.test"}}, Views: []View{
		{ID: "main", Entry: "https://admin.acme.test/app", Targets: []string{TargetApp}},
		{ID: "other", Entry: "https://elsewhere.test/app", Targets: []string{TargetApp}},
		{ID: "local", Entry: "ui/index.html", Targets: []string{TargetPane}},
	}}
	if u, ok := m.RemoteView("main"); !ok || u != "https://admin.acme.test/app" {
		t.Fatalf("main: %q %v", u, ok)
	}
	for _, id := range []string{"other", "local", "none"} {
		if _, ok := m.RemoteView(id); ok {
			t.Errorf("%s is no remote view", id)
		}
	}
	if _, err := m.ViewPath("main"); err == nil {
		t.Error("a remote view has no path on the view server")
	}
}
