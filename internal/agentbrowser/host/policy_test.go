package host

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/cdp"
)

func testGate(names map[string][]string, dev ...int) *gate {
	g := newGate(true)
	g.local = map[string]bool{"203.0.113.7": true}
	g.private = map[string]bool{"gitlab.office": true}
	g.lookup = func(host string) ([]string, error) {
		if a, ok := names[host]; ok {
			return a, nil
		}
		return nil, errors.New("no such host")
	}
	g.ports = func() (map[int]bool, map[int]bool) {
		out := map[int]bool{}
		for _, p := range dev {
			out[p] = true
		}
		return out, map[int]bool{5173: true, 8022: true}
	}
	return g
}

func TestGateHoldsTheAgentBrowserToPublicAddressesAndDevServers(t *testing.T) {
	g := testGate(map[string][]string{
		"example.org":   {"93.184.215.14"},
		"rebind.test":   {"127.0.0.1"},
		"intranet.test": {"10.1.2.3"},
		"metadata.test": {"169.254.169.254"},
		"mixed.test":    {"93.184.215.14", "192.168.1.10"},
		"gitlab.office": {"10.0.0.5"},
		"self.test":     {"203.0.113.7"},
	}, 5173)
	for _, tt := range []struct {
		host, port, addr string
	}{
		{"example.org", "443", "93.184.215.14"},
		{"localhost", "5173", "localhost"},
		{"app.localhost", "5173", "localhost"},
		{"127.0.0.1", "5173", "127.0.0.1"},
		{"::1", "5173", "::1"},
		{"gitlab.office", "443", "10.0.0.5"},
	} {
		if addr, why := g.resolve(tt.host, tt.port); addr != tt.addr || why != "" {
			t.Errorf("%s:%s: %q %q, want %q", tt.host, tt.port, addr, why, tt.addr)
		}
	}
	for _, tt := range []struct{ host, port string }{
		// The machine's ports other than its dev servers: the runtime's own, a database.
		{"localhost", "8022"}, {"127.0.0.1", "5432"}, {"::1", "8022"}, {"127.0.0.1", "x"},
		// Private networks, link-local, the machine's own public address.
		{"10.0.0.1", "80"}, {"172.16.0.1", "80"}, {"192.168.0.1", "80"}, {"100.64.0.1", "80"},
		{"169.254.169.254", "80"}, {"fe80::1", "80"}, {"fd00::1", "80"}, {"203.0.113.7", "443"},
		{"0.0.0.0", "5173"}, {"::ffff:127.0.0.1", "5173"},
		// Names that lead there: rebinding to the loopback opens no dev server either.
		{"rebind.test", "5173"}, {"intranet.test", "80"}, {"metadata.test", "80"}, {"mixed.test", "443"},
		{"self.test", "443"}, {"missing.test", "443"},
	} {
		if addr, why := g.resolve(tt.host, tt.port); addr != "" || why == "" {
			t.Errorf("%s:%s reached %q", tt.host, tt.port, addr)
		}
	}
}

func TestGateChecksTheNameOnceAndConnectsWhereItChecked(t *testing.T) {
	answers := [][]string{{"93.184.215.14"}, {"127.0.0.1"}}
	g := testGate(nil)
	g.lookup = func(string) ([]string, error) {
		a := answers[0]
		answers = answers[1:]
		return a, nil
	}
	first, _ := g.resolve("flip.test", "443")
	second, why := g.resolve("flip.test", "443")
	if first != "93.184.215.14" || second != first || why != "" {
		t.Fatalf("verdict not kept: %q then %q (%s)", first, second, why)
	}
}

func TestAllowedNavigation(t *testing.T) {
	for _, ok := range []string{"https://example.org/", "http://localhost:5173/x", "about:blank"} {
		if !AllowedNavigation(ok) {
			t.Errorf("%s refused", ok)
		}
	}
	for _, bad := range []string{"file:///etc/passwd", "chrome://settings", "javascript:alert(1)",
		"data:text/html,<b>x</b>", "view-source:https://example.org", "about:config", "https://u:p@example.org",
		"chrome-extension://x/y", "blob:https://example.org/x", "ftp://example.org", "http://", ""} {
		if AllowedNavigation(bad) {
			t.Errorf("%s allowed", bad)
		}
	}
}

func TestRelayAnswersHelloAndRefusesDangerousPages(t *testing.T) {
	chromeSide, hostSide := net.Pipe()
	defer chromeSide.Close()
	stream := cdp.NewStream(hostSide)
	defer stream.Close()
	r := &relay{chrome: stream, build: "1.2.3"}
	go func() { _ = r.read(t.Context()) }()
	client, clientHost := net.Pipe()
	defer client.Close()
	r.replace(clientHost)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	peer := cdp.NewStream(client)
	ask := func(raw string) map[string]json.RawMessage {
		t.Helper()
		if err := peer.Write(ctx, []byte(raw)); err != nil {
			t.Fatal(err)
		}
		b, err := peer.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var msg map[string]json.RawMessage
		if err = json.Unmarshal(b, &msg); err != nil {
			t.Fatal(err)
		}
		return msg
	}
	if got := ask(`{"id":7,"method":"Host.hello"}`); string(got["id"]) != "7" || string(got["result"]) != `{"build":"1.2.3"}` {
		t.Fatalf("hello: %v", got)
	}
	for _, raw := range []string{
		`{"id":8,"method":"Page.navigate","sessionId":"s","params":{"url":"file:///etc/passwd"}}`,
		`{"id":9,"method":"Target.createTarget","params":{"url":"chrome://settings"}}`,
		`{"id":10,"method":"Page.navigate","sessionId":"s","params":{"url":"javascript:alert(1)"}}`,
		`{"id":11,"method":"Page.navigate","sessionId":"s","params":{"url":"data:text/html,x"}}`,
		// One parser reads URL, the other url.
		`{"id":13,"method":"Page.navigate","sessionId":"s","params":{"url":"file:///etc/passwd","URL":"https://example.org/"}}`,
		`{"id":14,"method":"Page.navigate","sessionId":"s","params":{"Url":"file:///etc/passwd"}}`,
		// Around the screening: a non-flat session, the page's own protocol, the egress, the download rule.
		`{"id":15,"method":"Target.sendMessageToTarget","params":{"message":"{\"id\":1,\"method\":\"Page.navigate\",\"params\":{\"url\":\"file:///\"}}"}}`,
		`{"id":16,"method":"Target.exposeDevToolsProtocol","params":{"targetId":"t"}}`,
		`{"id":17,"method":"Target.createBrowserContext","params":{"proxyServer":"direct://"}}`,
		`{"id":18,"method":"Browser.setDownloadBehavior","params":{"behavior":"allow","downloadPath":"/home"}}`,
	} {
		if got := ask(raw); got["error"] == nil || got["result"] != nil {
			t.Fatalf("%s passed: %v", raw, got)
		}
	}
	// Nothing of that reached Chrome: the first thing it reads is the next allowed command.
	if err := peer.Write(ctx, []byte(`{"id":12,"method":"Page.navigate","sessionId":"s","params":{"url":"file:///etc/passwd","url":"https://example.org/"}}`)); err != nil {
		t.Fatal(err)
	}
	b, err := cdp.NewStream(chromeSide).Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// A key given twice reaches Chrome once, with the value that was checked: Go's map keeps the last one.
	if !strings.Contains(string(b), `"method":"Page.navigate"`) || strings.Count(string(b), `"url"`) != 1 ||
		strings.Contains(string(b), "file:") {
		t.Fatalf("chrome read %s", b)
	}
}

func TestParseLsofKeepsTheUsersOwnListenersOnly(t *testing.T) {
	t.Setenv("RUNTIME_RESERVED_PORTS", "9999")
	out := "p100\ncnode\nn*:5173\nn127.0.0.1:5174\np200\ncharnessly-runtime\nn127.0.0.1:8022\n" +
		"p300\ncpython3\nn[::1]:9999\nn[::1]:8000\np400\ncmyruntime\nn*:7000\n"
	got := parseLsof(out, 400, "myruntime")
	want := map[int]bool{5173: true, 5174: true, 8000: true}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for p := range want {
		if !got[p] {
			t.Fatalf("got %v", got)
		}
	}
}

func TestGateSaysWhyALoopbackPortIsRefused(t *testing.T) {
	g := testGate(nil, 5173)
	if _, why := g.resolve("localhost", "4400"); !strings.Contains(why, "net::ERR_CONNECTION_REFUSED") {
		t.Fatalf("closed port: %q", why)
	}
	if _, why := g.resolve("127.0.0.1", "8022"); !strings.Contains(why, "not a dev server") {
		t.Fatalf("the runtime's port: %q", why)
	}
	if why := Refusal("javascript:alert(1)"); why == "" {
		t.Fatal("javascript: passed")
	}
	if why := Refusal("about:blank"); why != "" {
		t.Fatalf("about:blank: %q", why)
	}
}
