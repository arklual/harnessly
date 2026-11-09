package api_test

import (
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
)

func TestSwitchesAdminOnly(t *testing.T) {
	s := hubtest.New(t)
	admin := s.SignIn("owner@example.com")
	other := s.SignIn("member@example.com")

	if r := other.Do("GET", "/api/admin/switches", nil); r.Code != 403 {
		t.Fatalf("a member must not read: %s", r)
	}
	if r := other.Do("PUT", "/api/admin/switches", map[string]any{"feature": "f", "enabled": true}); r.Code != 403 {
		t.Fatalf("a member must not set: %s", r)
	}
	if r := admin.Do("PUT", "/api/admin/switches", map[string]any{"feature": "f", "person": "A@Example.com", "enabled": true, "note": "try"}); r.Code != 204 {
		t.Fatalf("set: %s", r)
	}
	if r := admin.Do("PUT", "/api/admin/switches", map[string]any{"feature": " ", "enabled": true}); r.Code != 400 {
		t.Fatalf("a nameless switch: %s", r)
	}
	var out struct {
		Switches []struct {
			Feature, Person, Machine, Note, UpdatedBy string
			Enabled                                   bool
		}
	}
	admin.Do("GET", "/api/admin/switches?prefix=f", nil).JSON(&out)
	if len(out.Switches) != 1 || out.Switches[0].Person != "a@example.com" || out.Switches[0].Machine != "*" || !out.Switches[0].Enabled || out.Switches[0].UpdatedBy != "owner@example.com" {
		t.Fatalf("list: %+v", out)
	}
	if r := admin.Do("DELETE", "/api/admin/switches?feature=f&person=a@example.com", nil); r.Code != 204 {
		t.Fatalf("remove: %s", r)
	}
	out.Switches = nil
	admin.Do("GET", "/api/admin/switches", nil).JSON(&out)
	if len(out.Switches) != 0 {
		t.Fatalf("after remove: %+v", out)
	}
}
