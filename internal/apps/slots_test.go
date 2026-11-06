package apps

import (
	"os"
	"strings"
	"testing"
)

// The example package of the slots must stay a valid manifest, with the four slots it shows.
func TestSlotsExampleManifest(t *testing.T) {
	raw, err := os.ReadFile("../../examples/ext-ui-slots/harnessly/app.json")
	if err != nil {
		t.Fatal(err)
	}
	m, ps := ParseManifest(raw)
	if m == nil {
		t.Fatalf("manifest: %v", ps)
	}
	for _, p := range ps {
		if !p.Warning {
			t.Errorf("problem: %s", p)
		}
	}
	for _, target := range []string{TargetSettings, TargetOnboarding, TargetObjectAction, TargetBranding} {
		if !m.HasTarget(target) {
			t.Errorf("example lacks target %s", target)
		}
	}
	if m.Branding == nil || m.Branding.Name == "" {
		t.Errorf("example has no branding: %+v", m.Branding)
	}
}

func slotManifest(extra string) string {
	return `{"id":"example-corp.slots","name":"Slots","description":"d","version":"1.0.0","harnessly":">=0.17","trust":"sandboxed",` + extra + `}`
}

func TestSlotRules(t *testing.T) {
	ok := `"targets":["settings","object-action"],"views":[
	  {"id":"s","entry":"ui/s.html","targets":["settings"],"label":"Example"},
	  {"id":"a","entry":"ui/a.html","targets":["object-action"],"objects":["machine","chat"]}]`
	if m, ps := ParseManifest([]byte(slotManifest(ok))); m == nil || len(ps) != 0 {
		t.Fatalf("valid slots: %v", ps)
	}
	cases := map[string]struct{ body, path string }{
		"objects on a settings view": {`"targets":["settings"],"views":[{"id":"s","entry":"ui/s.html","targets":["settings"],"objects":["machine"]}]`, "views[0].objects"},
		"unknown object kind":        {`"targets":["object-action"],"views":[{"id":"a","entry":"ui/a.html","targets":["object-action"],"objects":["planet"]}]`, "views[0].objects"},
		"long label":                 {`"targets":["settings"],"views":[{"id":"s","entry":"ui/s.html","targets":["settings"],"label":"` + strings.Repeat("x", 41) + `"}]`, "views[0].label"},
		"branding without target":    {`"targets":["settings"],"views":[{"id":"s","entry":"ui/s.html","targets":["settings"]}],"branding":{"name":"X"}`, "branding"},
		"branding bad accent":        {`"targets":["branding"],"branding":{"name":"X","accent":"blue"}`, "branding.accent"},
		"branding http support":      {`"targets":["branding"],"branding":{"name":"X","supportUrl":"http://example.com"}`, "branding.supportUrl"},
		"branding empty name":        {`"targets":["branding"],"branding":{"name":""}`, "branding.name"},
		"branding target without it": {`"targets":["branding"]`, "targets"},
	}
	for name, c := range cases {
		_, ps := ParseManifest([]byte(slotManifest(c.body)))
		found := false
		for _, p := range ps {
			if !p.Warning && strings.HasPrefix(p.Path, c.path) {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: no error at %s in %v", name, c.path, ps)
		}
	}
}

func TestIsSlot(t *testing.T) {
	for _, s := range []string{"settings", "onboarding", "object-action", "hub-settings", "branding"} {
		if !IsSlot(s) {
			t.Errorf("%s is a slot", s)
		}
	}
	for _, s := range []string{"chat", "pane", "app", "sinopia.site", ""} {
		if IsSlot(s) {
			t.Errorf("%s is not a slot", s)
		}
	}
}

func TestSlotDoneNeedsNoRight(t *testing.T) {
	if right, ok := methodPermission["harnessly/slot.done"]; !ok || right != "" {
		t.Fatalf("slot.done: %q %v", right, ok)
	}
}
