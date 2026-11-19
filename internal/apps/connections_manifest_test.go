package apps

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// connectionsManifest is the review app with the given permissions and connections.
func connectionsManifest(perms, connections string) string {
	return `{"id":"acme.panel","name":"Панель","description":"d","version":"1.0.0","harnessly":">=0.17","trust":"declarative",
"targets":["chat"],"activation":{"on":["manual"]},"permissions":[` + perms + `],"connections":[` + connections + `],
"agent":{"instructions":"instructions.md"}}`
}

func TestManifestChecksConnections(t *testing.T) {
	read, act := `"connections.read"`, `"connections.read","connections.act"`
	cases := []struct{ name, perms, conns, want string }{
		{"by id", read, `{"id":"linear"}`, ""},
		{"by group", read, `{"category":"mail","access":"read"}`, ""},
		{"all with the right", act, `{"id":"linear","access":"all","reason":"Files issues"}`, ""},
		{"no right", ``, `{"id":"linear"}`, "need the permission connections.read"},
		{"all without act", read, `{"id":"linear","access":"all"}`, "all needs the permission connections.act"},
		{"unknown id", read, `{"id":"nope"}`, `"nope" is not in the catalog`},
		{"unknown group", read, `{"category":"nope"}`, `"nope" is not a group`},
		{"both", read, `{"id":"linear","category":"tasks"}`, "an id or a category, not both"},
		{"neither", read, `{"access":"read"}`, "names a catalog id"},
		{"a level that is none", read, `{"id":"linear","access":"root"}`, "is read or all"},
		{"twice", read, `{"id":"linear"},{"id":"linear"}`, "is named twice"},
		{"a reason too long", read, `{"id":"linear","reason":"` + strings.Repeat("я", 201) + `"}`, "up to 200"},
	}
	for _, c := range cases {
		m, ps := ParseManifest([]byte(connectionsManifest(c.perms, c.conns)))
		var errs []string
		for _, p := range ps {
			if !p.Warning {
				errs = append(errs, p.Path+" "+p.Message)
			}
		}
		got := strings.Join(errs, "\n")
		if c.want == "" && got != "" || c.want != "" && !strings.Contains(got, c.want) {
			t.Errorf("%s: problems %q, want %q", c.name, got, c.want)
		}
		_ = m
	}
	// A right with nothing named does nothing: said so.
	_, ps := ParseManifest([]byte(connectionsManifest(read, ``)))
	if !strings.Contains(ps.Error(), "connections.read needs a `connections` list") {
		t.Errorf("the right alone: %v", ps)
	}
}

func installPanel(t *testing.T, manifest, mcp string) (*Registry, string) {
	t.Helper()
	reg := &Registry{Root: filepath.Join(t.TempDir(), "apps")}
	pkg := filepath.Join(t.TempDir(), "panel")
	if err := Scaffold(pkg, "acme.panel", "Panel", "chat"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, ManifestPath), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if mcp != "" {
		if err := os.WriteFile(filepath.Join(pkg, ".mcp.json"), []byte(mcp), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, ps, err := reg.Install(pkg, InstallOptions{Dev: true, HostVersion: "0.17.0"}); err != nil {
		t.Fatalf("%v %v", err, ps)
	}
	return reg, pkg
}

func TestConnectionAgreementsKeepTheLevel(t *testing.T) {
	reg, pkg := installPanel(t, connectionsManifest(`"connections.read","connections.act"`,
		`{"id":"linear","access":"all"},{"category":"mail"}`), "")
	const app = "acme.panel"
	// The default is as far as the app asks; an app is given no more than it asked.
	if err := reg.GrantConnection(app, "linear"); err != nil || reg.ConnectionAccess(app, "linear") != AccessAll || !reg.ConnectionGranted(app, "linear") {
		t.Fatalf("default: %v %q", err, reg.ConnectionAccess(app, "linear"))
	}
	if err := reg.GrantConnectionAccess(app, "linear", AccessRead); err != nil || reg.ConnectionAccess(app, "linear") != AccessRead {
		t.Fatalf("read: %v %q", err, reg.ConnectionAccess(app, "linear"))
	}
	// Read does not stand for the whole connection: a server of .mcp.json through it is not agreed to.
	if reg.ConnectionGranted(app, "linear") {
		t.Fatal("a read agreement was taken for the whole connection")
	}
	// A group in the manifest covers each connection of it, each agreed to apart; the entry's group is the catalog's.
	if err := reg.GrantConnectionAccess(app, "yandex-mail", AccessAll); err == nil {
		t.Fatal("mail was named for reading: all is more than it asked")
	}
	if err := reg.GrantConnection(app, "yandex-mail"); err != nil || reg.ConnectionAccess(app, "yandex-mail") != AccessRead {
		t.Fatalf("group: %v %q", err, reg.ConnectionAccess(app, "yandex-mail"))
	}
	if err := reg.GrantConnection(app, "notion"); err == nil {
		t.Fatal("agreed to a connection the app does not name")
	}
	if err := reg.GrantConnectionAccess(app, "linear", "root"); err == nil {
		t.Fatal("a level that is none")
	}
	if err := reg.RevokeConnection(app, "linear"); err != nil || reg.ConnectionAccess(app, "linear") != "" {
		t.Fatalf("revoke: %v", err)
	}
	// An update that drops or narrows what was agreed to ends it: the agreement is cut to the declaration.
	if err := reg.GrantConnection(app, "linear"); err != nil {
		t.Fatal(err)
	}
	narrow := connectionsManifest(`"connections.read"`, `{"id":"linear"}`)
	if err := os.WriteFile(filepath.Join(pkg, ManifestPath), []byte(narrow), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ps, err := reg.Install(pkg, InstallOptions{Dev: true, HostVersion: "0.17.0"}); err != nil {
		t.Fatalf("%v %v", err, ps)
	}
	if got := reg.EffectiveConnectionAccess(app, "linear"); got != AccessRead {
		t.Fatalf("narrowed by the update: %q", got)
	}
	if got := reg.EffectiveConnectionAccess(app, "yandex-mail"); got != "" {
		t.Fatalf("dropped by the update: %q", got)
	}
	// Removing the app ends its agreements.
	if err := reg.Remove(app); err != nil {
		t.Fatal(err)
	}
	if reg.ConnectionAccess(app, "linear") != "" {
		t.Fatal("the agreement outlived the app")
	}
}

func TestMCPServerOfAConnectionIsDeclaredAll(t *testing.T) {
	reg, _ := installPanel(t, connectionsManifest(`"connections.read"`, `{"id":"linear"}`), connectionMCP)
	pkg, err := reg.Package("acme.panel")
	if err != nil {
		t.Fatal(err)
	}
	// The server of .mcp.json is the whole connection, whatever the manifest says of reading.
	if got := pkg.DeclaredAccess("linear"); got != AccessAll {
		t.Fatalf("declared: %q", got)
	}
	if got := pkg.DeclaredAccess("notion"); got != "" {
		t.Fatalf("not named: %q", got)
	}
	if len(pkg.ConnectionNeeds()) != 1 {
		t.Fatalf("one connection, named twice: %+v", pkg.ConnectionNeeds())
	}
}

// The bridge's methods of hl.connections need the right, and the target the view is shown in.
func TestConnectionMethodsNeedTheRight(t *testing.T) {
	m, ps := ParseManifest([]byte(connectionsManifest(`"connections.read"`, `{"id":"linear"}`)))
	if m == nil {
		t.Fatal(ps)
	}
	for _, method := range []string{"harnessly/connections.list", "harnessly/connections.tools", "harnessly/connections.call", "harnessly/connections.api", "harnessly/connections.request"} {
		if err := m.Authorize(method, TargetChat, nil); err != nil {
			t.Errorf("%s with the right: %v", method, err)
		}
		if err := m.Authorize(method, TargetPane, nil); err == nil || !strings.Contains(err.Error(), "not_in_target") {
			t.Errorf("%s in a target the app lacks: %v", method, err)
		}
	}
	none, _ := ParseManifest([]byte(review))
	if err := none.Authorize("harnessly/connections.call", TargetChat, nil); err == nil || !strings.Contains(err.Error(), "connections.read") {
		t.Errorf("without the right: %v", err)
	}
}
