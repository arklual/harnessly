package apps

import (
	"strings"
	"testing"
)

const cashback = `{
  "id": "acme.cashback",
  "name": "Кешбэки",
  "description": "Заводит кешбэки в приложении банка",
  "version": "1.0.0",
  "harnessly": ">=0.17",
  "trust": "sandboxed",
  "targets": ["chat", "pane", "sinopia.site"],
  "activation": { "on": ["manual", "command:/cashback"], "openView": "main" },
  "permissions": ["chat.prompt", "chat.card", "pane.open", "storage", "secrets:bank-token", "sinopia.read", "sinopia.write"],
  "network": { "connect": ["cashback-api.bank.example"] },
  "secrets": [{ "name": "bank-token", "description": "Токен API кешбэков банка" }],
  "agent": {
    "instructions": "instructions.md",
    "skills": ["cashback-docs"],
    "mcp": ["bank-cashback"],
    "tools": { "allow": ["bank-cashback.*", "app.*", "Read"], "deny": ["Bash", "WebFetch", "mcp:*"], "confirm": ["bank-cashback.create"] }
  },
  "views": [
    { "id": "main", "title": "Кешбэки", "entry": "ui/index.html", "targets": ["pane"] },
    { "id": "card", "entry": "ui/card.html", "targets": ["chat"] },
    { "id": "inspector", "entry": "ui/inspector.html", "targets": ["sinopia.site"] }
  ],
  "chat": { "cards": ["card"] },
  "sinopia": { "bindings": { "inspector": "inspector" } }
}`

const review = `{
  "id": "acme.mr-review",
  "name": "Ревью MR",
  "description": "Ревью по чек-листу команды, только чтение",
  "version": "0.1.0",
  "harnessly": "^0.17",
  "trust": "declarative",
  "targets": ["chat"],
  "activation": { "on": ["command:/mr-review"] },
  "permissions": [],
  "agent": { "instructions": "instructions.md", "skills": ["review-checklist"], "tools": { "deny": ["Edit", "Write", "Bash(git push*)"] } }
}`

func TestParseManifestExamples(t *testing.T) {
	for name, raw := range map[string]string{"cashback": cashback, "review": review} {
		m, ps := ParseManifest([]byte(raw))
		if m == nil || len(ps) != 0 {
			t.Fatalf("%s: %v", name, ps)
		}
	}
}

func TestParseManifestSyntaxLine(t *testing.T) {
	_, ps := ParseManifest([]byte("{\n  \"id\": \"a.b\",\n  \"name\" \"x\"\n}"))
	if len(ps) != 1 || !strings.Contains(ps[0].Message, "line 3") {
		t.Fatalf("want the line of the error, got %v", ps)
	}
	_, ps = ParseManifest([]byte(`{"id": "a.b", "colour": "red"}`))
	if len(ps) != 1 || !strings.Contains(ps[0].Message, `unknown field "colour"`) {
		t.Fatalf("want the unknown field named, got %v", ps)
	}
	_, ps = ParseManifest([]byte(`{"targets": "chat"}`))
	if len(ps) != 1 || !strings.Contains(ps[0].Message, "targets") {
		t.Fatalf("want the field of the wrong type named, got %v", ps)
	}
}

func problemAt(ps Problems, path string) *Problem {
	for i := range ps {
		if ps[i].Path == path {
			return &ps[i]
		}
	}
	return nil
}

func TestCheckRules(t *testing.T) {
	cases := []struct {
		name, from, to, path string
	}{
		{"bad id", `"acme.cashback"`, `"Acme Cashback"`, "id"},
		{"bad version", `"1.0.0"`, `"1.0"`, "version"},
		{"bad range", `">=0.17"`, `">=zero"`, "harnessly"},
		{"bad trust", `"sandboxed"`, `"root"`, "trust"},
		{"bad target", `"sinopia.site"]`, `"sinopia.Site"]`, "targets[2]"},
		{"bad activation", `"command:/cashback"`, `"command:cashback"`, "activation.on[1]"},
		{"missing open view", `"openView": "main"`, `"openView": "nope"`, "activation.openView"},
		{"unknown permission", `"chat.prompt",`, `"root",`, "permissions[0]"},
		{"secret without permission", `"secrets:bank-token", `, ``, "secrets[0]"},
		{"bad host", `"cashback-api.bank.example"`, `"http://x"`, "network.connect[0]"},
		{"mask in the middle", `"bank-cashback.*"`, `"bank*cashback.create"`, "agent.tools.allow[0]"},
		{"view outside targets", `"targets": ["pane"] }`, `"targets": ["app"] }`, "views[0].targets[0]"},
		{"card not a chat view", `"cards": ["card"]`, `"cards": ["main"]`, "chat.cards[0]"},
		{"duplicate view", `"id": "card"`, `"id": "main"`, "views[1].id"},
	}
	for _, c := range cases {
		raw := strings.Replace(cashback, c.from, c.to, 1)
		if raw == cashback {
			t.Fatalf("%s: the case does not change the manifest", c.name)
		}
		_, ps := ParseManifest([]byte(raw))
		if problemAt(ps, c.path) == nil {
			t.Errorf("%s: want a problem at %s, got %v", c.name, c.path, ps)
		}
	}
}

func TestTargetWithoutContribution(t *testing.T) {
	raw := strings.Replace(review, `"targets": ["chat"]`, `"targets": ["chat", "app"]`, 1)
	_, ps := ParseManifest([]byte(raw))
	p := problemAt(ps, "targets")
	if p == nil || !strings.Contains(p.Message, `"app"`) {
		t.Fatalf("want the empty app target named, got %v", ps)
	}
}

func TestDeclarativeRunsNoScripts(t *testing.T) {
	raw := strings.Replace(cashback, `"sandboxed"`, `"declarative"`, 1)
	_, ps := ParseManifest([]byte(raw))
	if problemAt(ps, "network") == nil {
		t.Fatalf("want network refused for a declarative app, got %v", ps)
	}
}

func TestUnusedRightIsWarning(t *testing.T) {
	raw := strings.Replace(review, `"permissions": []`, `"permissions": ["pane.open"]`, 1)
	_, ps := ParseManifest([]byte(raw))
	p := problemAt(ps, "permissions")
	if p == nil || !p.Warning || len(ps.Errors()) != 0 {
		t.Fatalf("want one warning, got %v", ps)
	}
}

func TestHasTarget(t *testing.T) {
	m := &Manifest{Targets: []string{"chat", "sinopia.*"}}
	for target, want := range map[string]bool{"chat": true, "pane": false, "sinopia.slides": true, "sinopia.deck-x": true} {
		if got := m.HasTarget(target); got != want {
			t.Errorf("%s: got %v", target, got)
		}
	}
}

func TestRange(t *testing.T) {
	cases := []struct {
		r, v string
		want bool
	}{
		{">=0.17", "0.17.0", true},
		{">=0.17", "0.16.31", false},
		{">=0.17 <1", "0.99.1", true},
		{">=0.17 <1", "1.0.0", false},
		{"^0.17", "0.17.9", true},
		{"^0.17", "0.18.0", false},
		{"^1.2", "1.9.0", true},
		{"~1.2", "1.3.0", false},
		{"0.16 || >=0.18", "0.17.0", false},
		{"0.16 || >=0.18", "0.18.2", true},
		{"*", "3.0.0", true},
		{">=9", "dev", true},
	}
	for _, c := range cases {
		r, err := ParseRange(c.r)
		if err != nil {
			t.Fatalf("%s: %v", c.r, err)
		}
		if got := r.Allows(c.v); got != c.want {
			t.Errorf("%s allows %s: got %v", c.r, c.v, got)
		}
	}
	for _, bad := range []string{">=", "1.2.3.4", "a", "||"} {
		if _, err := ParseRange(bad); err == nil {
			t.Errorf("%q: want an error", bad)
		}
	}
	if CompareVersions("1.2.0", "1.10.0") >= 0 {
		t.Error("1.2.0 sorts before 1.10.0")
	}
}

func TestFilePermissionMasks(t *testing.T) {
	with := func(perm string) Problems {
		m, ps := ParseManifest([]byte(strings.Replace(review, `"permissions": []`, `"permissions": ["`+perm+`"]`, 1)))
		if m == nil {
			t.Fatalf("%s: %v", perm, ps)
		}
		return m.Check()
	}
	for _, ok := range []string{"files.read:docs/**", "files.write:out/*.md", "files.read:src/**/*.go"} {
		if ps := with(ok); len(ps) != 0 {
			t.Errorf("%s: %v", ok, ps)
		}
	}
	for _, bad := range []string{"files.read:/etc/**", "files.read:../x", "files.write:docs/../../x", "files.read:~/x", "files.write:"} {
		if ps := with(bad); len(ps.Errors()) == 0 {
			t.Errorf("%s passed", bad)
		}
	}
	if ps := with("files.write:**"); len(ps.Errors()) != 0 || len(ps) == 0 {
		t.Errorf("a write over the whole workspace is a warning: %v", ps)
	}
}
