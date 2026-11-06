package apps

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeConnections is the person's connections as a launch sees them: transport by catalog id.
type fakeConnections map[string]string

func (f fakeConnections) Transport(id string) (string, bool) { t, ok := f[id]; return t, ok }

// connectionApp installs a dev app whose .mcp.json has a server that names a connection and one that does not.
func connectionApp(t *testing.T, mcp string) (*Registry, string) {
	t.Helper()
	data := t.TempDir()
	reg := &Registry{Root: filepath.Join(data, "apps")}
	pkg := filepath.Join(t.TempDir(), "tracker")
	if err := Scaffold(pkg, "acme.tracker", "Tracker", "chat"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, ".mcp.json"), []byte(mcp), 0o644); err != nil {
		t.Fatal(err)
	}
	return reg, pkg
}

const connectionMCP = `{"mcpServers":{"linear":{"type":"http","connection":"linear"},"free":{"type":"http","url":"https://free.example.com/mcp"}}}`

func TestValidatorChecksConnection(t *testing.T) {
	cases := []struct {
		name, mcp, want string
	}{
		{"fine", connectionMCP, ""},
		{"sse is fine", `{"mcpServers":{"a":{"type":"sse","connection":"asana"}}}`, ""},
		{"unknown id", `{"mcpServers":{"a":{"type":"http","connection":"nope"}}}`, `connection "nope" is not in the catalog`},
		{"has an url", `{"mcpServers":{"a":{"type":"http","connection":"linear","url":"https://x.example.com"}}}`, "no url or headers"},
		{"has headers", `{"mcpServers":{"a":{"type":"http","connection":"linear","headers":{"X":"1"}}}}`, "no url or headers"},
		{"has a command", `{"mcpServers":{"a":{"command":"srv","connection":"linear"}}}`, "a local server (command) needs trust trusted"},
		{"stdio type", `{"mcpServers":{"a":{"type":"stdio","connection":"linear"}}}`, "its type is http or sse"},
	}
	for _, c := range cases {
		reg, pkg := connectionApp(t, c.mcp)
		_ = reg
		_, ps := LoadPackage(pkg)
		var errs []string
		for _, p := range ps {
			if !p.Warning {
				errs = append(errs, p.Message)
			}
		}
		got := strings.Join(errs, "\n")
		if c.want == "" && got != "" || c.want != "" && !strings.Contains(got, c.want) {
			t.Errorf("%s: problems %q, want %q", c.name, got, c.want)
		}
	}
}

func TestLaunchWithConnectionPointsAtProxyAndHoldsNoToken(t *testing.T) {
	reg, pkg := connectionApp(t, connectionMCP)
	if _, ps, err := reg.Install(pkg, InstallOptions{Dev: true, HostVersion: "0.17.0"}); err != nil {
		t.Fatalf("%v %v", err, ps)
	}
	l := launcherFor(t, reg, fakeSafe{})
	l.Connections = fakeConnections{"linear": "http"}
	if err := reg.GrantConnection("acme.tracker", "linear"); err != nil {
		t.Fatal(err)
	}
	got, err := l.Claude([]string{"acme.tracker"}, baseSettings(t))
	if err != nil {
		t.Fatal(err)
	}
	srv := readServers(t, got.PluginDirs[0])
	lin := srv["linear"]
	if !strings.HasPrefix(lin.URL, "http://127.0.0.1:4455"+ProxyPrefix+"acme.tracker/linear") || lin.Connection != "" || lin.Type != "http" {
		t.Fatalf("a connection server points at the proxy: %+v", lin)
	}
	if lin.Headers["X-Runtime-Token"] == "" || strings.HasPrefix(lin.Headers["X-Runtime-Token"], "person") {
		t.Fatalf("it carries the launch's token, not the person's: %+v", lin.Headers)
	}
	if srv["free"].URL != "https://free.example.com/mcp" {
		t.Fatalf("the other server is as written: %+v", srv["free"])
	}
}

func TestLaunchWaitingForConnectionStillStarts(t *testing.T) {
	reg, pkg := connectionApp(t, connectionMCP)
	if _, ps, err := reg.Install(pkg, InstallOptions{Dev: true, HostVersion: "0.17.0"}); err != nil {
		t.Fatalf("%v %v", err, ps)
	}
	if err := reg.GrantConnection("acme.tracker", "linear"); err != nil {
		t.Fatal(err)
	}
	for name, conns := range map[string]ConnectionSource{"no connection": fakeConnections{}, "no source": nil} {
		l := launcherFor(t, reg, fakeSafe{})
		l.Connections = conns
		got, err := l.Claude([]string{"acme.tracker"}, baseSettings(t))
		if err != nil {
			t.Fatalf("%s: the launch must not fail: %v", name, err)
		}
		srv := readServers(t, got.PluginDirs[0])
		if _, has := srv["linear"]; has {
			t.Errorf("%s: the server of a missing connection is left out: %+v", name, srv["linear"])
		}
		if srv["free"].URL == "" {
			t.Errorf("%s: the rest of the app works", name)
		}
	}
}

func TestPackageConnections(t *testing.T) {
	reg, pkgDir := connectionApp(t, `{"mcpServers":{"a":{"type":"http","connection":"linear"},"b":{"type":"http","connection":"linear"},"c":{"type":"http","connection":"asana"}}}`)
	_ = reg
	pkg, _ := LoadPackage(pkgDir)
	if got := strings.Join(pkg.Connections(), ","); got != "asana,linear" {
		t.Fatalf("%q", got)
	}
}

func TestConnectionNeedsThePersonsAgreementForThisApp(t *testing.T) {
	reg, pkg := connectionApp(t, connectionMCP)
	if _, ps, err := reg.Install(pkg, InstallOptions{Dev: true, HostVersion: "0.17.0"}); err != nil {
		t.Fatalf("%v %v", err, ps)
	}
	launch := func() map[string]MCPServer {
		l := launcherFor(t, reg, fakeSafe{})
		l.Connections = fakeConnections{"linear": "http"}
		got, err := l.Claude([]string{"acme.tracker"}, baseSettings(t))
		if err != nil {
			t.Fatal(err)
		}
		return readServers(t, got.PluginDirs[0])
	}
	if _, has := launch()["linear"]; has {
		t.Fatal("without the agreement the server is left out, signed in or not")
	}
	if err := reg.GrantConnection("acme.tracker", "asana"); err == nil {
		t.Fatal("an agreement is for a connection the package names")
	}
	if err := reg.GrantConnection("acme.tracker", "linear"); err != nil {
		t.Fatal(err)
	}
	if _, has := launch()["linear"]; !has || !reg.ConnectionGranted("acme.tracker", "linear") {
		t.Fatal("with the agreement the server goes")
	}
	if reg.ConnectionGranted("acme.other", "linear") {
		t.Fatal("an agreement is for one app")
	}
	// An update that names one more connection does not widen it.
	next := `{"mcpServers":{"linear":{"type":"http","connection":"linear"},"asana":{"type":"sse","connection":"asana"}}}`
	if err := os.WriteFile(filepath.Join(pkg, ".mcp.json"), []byte(next), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ps, err := reg.Install(pkg, InstallOptions{Dev: true, HostVersion: "0.17.0"}); err != nil {
		t.Fatalf("%v %v", err, ps)
	}
	if !reg.ConnectionGranted("acme.tracker", "linear") || reg.ConnectionGranted("acme.tracker", "asana") {
		t.Fatal("the new connection waits for its own agreement")
	}
	// Removing the app ends its agreements.
	if err := reg.Remove("acme.tracker"); err != nil {
		t.Fatal(err)
	}
	if reg.ConnectionGranted("acme.tracker", "linear") {
		t.Fatal("a removed app keeps no agreement")
	}
}

func TestGuardRefusesTheAgentsConnect(t *testing.T) {
	for _, line := range []string{"harnessly app connect acme.tracker linear", "harnessly app connect acme.tracker linear --off"} {
		if !selfCommand(line) {
			t.Errorf("%q must be refused to the agent", line)
		}
	}
}
