package designkit

import "testing"

const linkEnd = `"sample": { "href": "#", "children": "Open the event" } }`
const linkWith = `"sample": { "href": "#", "children": "Open the event" }, `

func hasRule(rep *CheckReport, rule string) bool {
	for _, p := range rep.Problems {
		if p.Rule == rule {
			return true
		}
	}
	return false
}

func TestK1FieldsAreChecked(t *testing.T) {
	cases := []struct{ name, file, old, new, rule string }{
		{"kind", "design-system.json", `"source"`, `"kind": "tv", "source"`, "identity.kind"},
		{"extends", "design-system.json", `"source"`, `"extends": "nope", "source"`, "identity.extends"},
		{"extends range", "design-system.json", `"source"`, `"extends": "base@^banana", "source"`, "identity.extends"},
		{"omitted", "design-system.json", `"source"`, `"omitted": ["colours"], "source"`, "identity.omitted"},
		{"code shape", "design-system.json", linkEnd, linkWith + `"code": "x" }`, "code.shape"},
		{"code empty export", "design-system.json", linkEnd, linkWith + `"code": {"import": "@a/b", "export": ""} }`, "code.shape"},
		{"status", "design-system.json", linkEnd, linkWith + `"status": "old" }`, "status.value"},
		{"replacedBy alone", "design-system.json", linkEnd, linkWith + `"replacedBy": "Button" }`, "status.replaced-by"},
		{"replacedBy unknown", "design-system.json", linkEnd, linkWith + `"status": "deprecated", "replacedBy": "Nope" }`, "status.replaced-by"},
		{"preset shape", "design-system.json", linkEnd, linkWith + `"preset": 3 }`, "preset.shape"},
		{"preset target", "design-system.json", linkEnd, linkWith + `"preset": {"of": "Nope", "props": {}} }`, "preset.target"},
		{"preset self", "design-system.json", linkEnd, linkWith + `"preset": {"of": "Link", "props": {}} }`, "preset.target"},
		{"preset prop", "design-system.json", linkEnd, linkWith + `"preset": {"of": "Button", "props": {"colour": "red"}} }`, "preset.props"},
		{"preset enum", "design-system.json", linkEnd, linkWith + `"preset": {"of": "Button", "props": {"variant": "danger"}} }`, "preset.props"},
		{"token var", "tokens.json", `"name": "space-2"`, `"var": "space", "name": "space-2"`, "token.var"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := copyTree(t, "testdata/check/ok")
			edit(t, root, tc.file, tc.old, tc.new)
			rep := check(t, root)
			if !hasRule(rep, tc.rule) {
				t.Fatalf("want %s, got %+v", tc.rule, rep.Problems)
			}
		})
	}
}

func TestK1ValidFieldsPass(t *testing.T) {
	root := copyTree(t, "testdata/check/ok")
	edit(t, root, "design-system.json", `"source"`, `"kind": "app", "extends": "base@^1.2.0", "omitted": ["patterns", "templates"], "source"`)
	edit(t, root, "design-system.json", linkEnd, linkWith+`"code": {"import": "@a/b", "export": "Link"}, "status": "deprecated", "replacedBy": "Button" }`)
	edit(t, root, "tokens.json", `"name": "space-2"`, `"var": "--space-2", "name": "space-2"`)
	if rep := check(t, root); len(rep.Problems) != 0 {
		t.Fatalf("%+v", rep.Problems)
	}
}

func TestReadinessLevels(t *testing.T) {
	rep := check(t, "testdata/check/ok")
	if rep.Level != -1 || len(rep.Readiness) != 2 || rep.Next == "" {
		t.Fatalf("%d %+v", rep.Level, rep.Readiness)
	}
	root := copyTree(t, "testdata/check/ok")
	edit(t, root, "design-system.json", linkEnd, linkWith+`"code": {"import": "@a/b", "export": "Link"} }`)
	rep = check(t, root)
	lv := map[string]int{}
	for _, r := range rep.Readiness {
		lv[r.Name] = r.Level
	}
	if lv["Link"] != TopLevel || lv["Button"] != -1 {
		t.Fatalf("%v", lv)
	}
}

func TestReadinessOmittedAndPreset(t *testing.T) {
	root := copyTree(t, "testdata/check/ok")
	edit(t, root, "design-system.json", `"source"`, `"omitted": ["tokens", "examples", "docs"], "source"`)
	edit(t, root, "design-system.json", linkEnd, linkWith+`"code": {"import": "@a/b", "export": "Link"}, "preset": {"of": "Button", "props": {"variant": "primary"}} }`)
	rep := check(t, root)
	for _, r := range rep.Readiness {
		if r.Name == "Link" && (r.Of != "Button" || r.Level != -1) {
			t.Fatalf("a preset follows its target: %+v", r)
		}
	}
}
