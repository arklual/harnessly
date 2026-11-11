package ext

import "testing"

func TestUISpecValidation(t *testing.T) {
	ok := Spec{Name: "acme", Kind: "test", URL: "http://127.0.0.1:1", Protocol: "harnessly.test/v1",
		UI: []UIView{{ID: "p", Slot: SlotHubSettings, Label: "Page", URL: "https://views.example.com/p"}}}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mut := range map[string]func(s *Spec){
		"http view":       func(s *Spec) { s.UI[0].URL = "http://views.example.com/p" },
		"credentials":     func(s *Spec) { s.UI[0].URL = "https://u:p@views.example.com/p" },
		"another slot":    func(s *Spec) { s.UI[0].Slot = "settings" },
		"no label":        func(s *Spec) { s.UI[0].Label = " " },
		"bad id":          func(s *Spec) { s.UI[0].ID = "A B" },
		"twice":           func(s *Spec) { s.UI = append(s.UI, s.UI[0]) },
		"bad accent":      func(s *Spec) { s.Branding = &Branding{Name: "X", Accent: "red"} },
		"nameless":        func(s *Spec) { s.Branding = &Branding{Accent: "#112233"} },
		"javascript help": func(s *Spec) { s.Branding = &Branding{Name: "X", SupportURL: "javascript:alert(1)"} },
	} {
		s := ok
		s.UI = append([]UIView(nil), ok.UI...)
		mut(&s)
		if s.Validate() == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestRegistryUIOrigins(t *testing.T) {
	specs := []Spec{
		{Name: "b", Kind: "t", URL: "http://127.0.0.1:1", Protocol: "harnessly.t/v1",
			UI: []UIView{{ID: "x", Slot: SlotHubSettings, Label: "B", URL: "https://b.example.com/x"}}},
		{Name: "a", Kind: "t", URL: "http://127.0.0.1:2", Protocol: "harnessly.t/v1",
			UI:       []UIView{{ID: "y", Slot: SlotHubSettings, Label: "A", URL: "https://b.example.com/y"}},
			Branding: &Branding{Name: "A Corp"}},
	}
	r, err := NewRegistry(specs, Config{})
	if err != nil {
		t.Fatal(err)
	}
	if o := r.FrameOrigins(); len(o) != 1 || o[0] != "https://b.example.com" {
		t.Errorf("origins = %v", o)
	}
	if u := r.UI(); len(u.Views) != 2 || u.Views[0].Ext != "a" || u.Branding == nil || u.Branding.Name != "A Corp" {
		t.Errorf("ui = %+v", u)
	}
}
