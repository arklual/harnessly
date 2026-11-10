package apps

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Sinopia is an app of the host's own: its manifest declares its points and the older targets as its aliases (HRN-808).
func TestSinopiaIsBuiltinWithItsPoints(t *testing.T) {
	m := builtinOf(SinopiaApp)
	if m == nil || m.Delivery != DeliveryBuiltin || !slices.Contains(m.Targets, TargetApp) {
		t.Fatalf("sinopia: %+v", m)
	}
	var points []string
	for _, p := range AppPoints() {
		if p.Owner == SinopiaApp {
			points = append(points, p.Name)
		}
	}
	for _, want := range []string{"sinopia.kind", "sinopia.panel", "sinopia.check"} {
		if !slices.Contains(points, want) {
			t.Errorf("no point %s in %v", want, points)
		}
	}
	for target, scope := range map[string]string{"sinopia.slides": "slides", "sinopia.*": "*", "sinopia.acme.board": "acme.board"} {
		owner, got, ok := aliasOwner(target)
		if !ok || owner != SinopiaApp || got != scope {
			t.Errorf("%s: %q %q %v", target, owner, got, ok)
		}
	}
	for _, target := range []string{"chat", "sinopia.", "other.slides", "sinopiaslides"} {
		if _, _, ok := aliasOwner(target); ok {
			t.Errorf("%s is no alias", target)
		}
	}
	if wildcardOf("sinopia.slides") != "sinopia.*" || wildcardOf("sinopia.acme.board") != "" || wildcardOf("pane") != "" {
		t.Error("wildcard")
	}
}

// The apps written for Sinopia before it had points work as they are: their manifests are not touched, and what they
// put into Sinopia goes into its points (HRN-808, stage 4).
func TestSinopiaAppsWorkUnchangedThroughItsPoints(t *testing.T) {
	read := func(name string) *Manifest {
		raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "apps", name, ManifestPath))
		if err != nil {
			t.Fatal(err)
		}
		m, ps := ParseManifest(raw)
		for _, p := range ps {
			if !p.Warning {
				t.Fatalf("%s: %s", name, p)
			}
		}
		return m
	}
	lint, cash := read("deck-lint"), read("cashback")
	reg := NewContributions([]Installed{{Manifest: lint}, {Manifest: cash}})
	want := map[string]string{
		"harnessly.deck-lint/sinopia.panel/sinopia.slides":     "lint",
		"harnessly.deck-lint/sinopia.check/sinopia.slides":     "deck",
		"harnessly.deck-lint/sinopia.selection/sinopia.slides": "shorten",
		"harnessly.cashback/sinopia.selection/sinopia.site":    "explain",
	}
	got := map[string]string{}
	for _, r := range reg.All() {
		if !strings.HasPrefix(r.Kind, "sinopia.") {
			if r.Owner != "" {
				t.Errorf("a host kind with an owner: %+v", r)
			}
			continue
		}
		if r.Owner != SinopiaApp {
			t.Errorf("not into Sinopia's point: %+v", r)
		}
		got[r.App+"/"+r.Kind+"/"+r.Target] = r.ID
	}
	for k, id := range want {
		if got[k] != id {
			t.Errorf("%s: %q, all %v", k, got[k], got)
		}
	}
	if !lint.OnInDocument("slides") || lint.OnInDocument("site") || !cash.HasTarget("sinopia.site") {
		t.Error("the targets as aliases")
	}
	// The binding of the cashback card (data-hl-field) is read as before.
	s, err := cash.SinopiaOf()
	if err != nil || s.Bindings == nil || s.Bindings.Inspector != "inspector" {
		t.Errorf("bindings: %+v %v", s.Bindings, err)
	}
}

// A package takes nothing of the host's own: not a builtin's id, not its delivery, points or aliases.
func TestPackageCannotBeBuiltin(t *testing.T) {
	raw := `{"id":"harnessly.sinopia","name":"S","description":"d","version":"1.0.0","harnessly":">=0.16","trust":"declarative",
	"targets":["chat"],"permissions":[],"agent":{"instructions":"i.md"},"delivery":"builtin",
	"points":[{"name":"sinopia.x","doc":"x","field":"x[]"}],"aliases":[{"name":"sinopia.*","doc":"x"}]}`
	_, ps := ParseManifest([]byte(raw))
	for _, path := range []string{"id", "delivery", "points", "aliases"} {
		if !slices.ContainsFunc(ps, func(p Problem) bool { return p.Path == path && !p.Warning }) {
			t.Errorf("no problem at %s: %v", path, ps)
		}
	}
}
