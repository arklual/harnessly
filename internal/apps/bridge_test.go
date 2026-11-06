package apps

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestAuthorize(t *testing.T) {
	m := &Manifest{Targets: []string{TargetPane}, Permissions: []string{"chat.prompt", "storage"}}
	cases := []struct {
		method, target string
		params         map[string]any
		code           string
	}{
		{"ui/initialize", "pane", nil, ""},
		{"tools/call", "pane", nil, ""},
		{"ui/message", "pane", nil, ""},
		{"harnessly/chat.read", "pane", nil, CodePermissionDenied},
		{"harnessly/chat.card", "pane", nil, CodePermissionDenied},
		{"harnessly/pane.open", "pane", nil, CodePermissionDenied},
		{"harnessly/notify", "pane", nil, CodePermissionDenied},
		{"harnessly/agents.list", "pane", nil, CodePermissionDenied},
		{"harnessly/pane.open", "pane", map[string]any{"target": "chat:tab-1"}, CodePermissionDenied},
		{"harnessly/storage.get", "pane", map[string]any{"scope": "install"}, ""},
		{"harnessly/storage.set", "pane", map[string]any{"scope": "user"}, ""},
		{"harnessly/storage.set", "pane", map[string]any{"scope": "space"}, CodePermissionDenied},
		{"harnessly/storage.get", "pane", map[string]any{"scope": "space"}, ""},
		{"ui/message", "chat", nil, CodeNotInTarget},
	}
	for _, c := range cases {
		err := m.Authorize(c.method, c.target, c.params)
		var be *BridgeError
		got := ""
		if errors.As(err, &be) {
			got = be.Code
		} else if err != nil {
			t.Fatalf("%s: %v", c.method, err)
		}
		if got != c.code {
			t.Errorf("%s %v: %q, want %q", c.method, c.params, got, c.code)
		}
	}
	// agents.read gives the list of conversations; opening one needs it as well as pane.open.
	agents := &Manifest{Targets: []string{TargetPane}, Permissions: []string{"agents.read"}}
	if err := agents.Authorize("harnessly/agents.list", "pane", nil); err != nil {
		t.Errorf("agents.list with agents.read: %v", err)
	}
	open := &Manifest{Targets: []string{TargetPane}, Permissions: []string{"pane.open"}}
	if err := open.Authorize("harnessly/pane.open", "pane", map[string]any{"target": "app:a.b"}); err != nil {
		t.Errorf("pane.open of an app: %v", err)
	}
	var denial *BridgeError
	if err := open.Authorize("harnessly/pane.open", "pane", map[string]any{"target": "chat:tab-1"}); !errors.As(err, &denial) || denial.Code != CodePermissionDenied {
		t.Errorf("pane.open of a chat without agents.read: %v", err)
	}
	both := &Manifest{Targets: []string{TargetPane}, Permissions: []string{"pane.open", "agents.read"}}
	if err := both.Authorize("harnessly/pane.open", "pane", map[string]any{"target": "chat:tab-1"}); err != nil {
		t.Errorf("pane.open of a chat with agents.read: %v", err)
	}
	// A call with no target is refused, not let through.
	for _, method := range []string{"ui/initialize", "harnessly/storage.get", "tools/call"} {
		var be *BridgeError
		if err := m.Authorize(method, "", nil); !errors.As(err, &be) || be.Code != CodeInvalidParams {
			t.Errorf("%s with no target: %v", method, err)
		}
	}
	if err := m.Authorize("harnessly/fs.read", "pane", nil); !errors.Is(err, ErrUnknownMethod) {
		t.Errorf("an unknown method: %v", err)
	}
	// files.read:<mask> gives the right to the files methods; which paths is FileScope's to judge.
	files := &Manifest{Targets: []string{TargetPane}, Permissions: []string{"files.read:docs/**"}}
	if err := files.Authorize("harnessly/files.read", "pane", nil); err != nil {
		t.Errorf("files.read with a mask: %v", err)
	}
	var be *BridgeError
	if err := files.Authorize("harnessly/files.write", "pane", nil); !errors.As(err, &be) || be.Code != CodePermissionDenied {
		t.Errorf("files.write on a files.read mask: %v", err)
	}
	if m.Allows("files.read") {
		t.Error("an app without a files mask holds files.read")
	}
	m.Permissions = append(m.Permissions, "storage.space.write")
	if err := m.Authorize("harnessly/storage.delete", "pane", map[string]any{"scope": "space"}); err != nil {
		t.Errorf("with the right: %v", err)
	}
}

// Every method of the table that needs a right names one the manifest knows, so a typo cannot make a method open.
func TestMethodPermissionsAreRealRights(t *testing.T) {
	for method, perm := range methodPermission {
		if perm != "" && !permissions[perm] && !paramPermissions[perm] {
			t.Errorf("%s needs %q, which is no permission", method, perm)
		}
	}
}

func TestCanCallTool(t *testing.T) {
	p := &Package{MCP: map[string]MCPServer{"bank": {URL: "https://x.test/mcp"}}}
	if s, tool, err := p.CanCallTool("bank.create"); err != nil || s != "bank" || tool != "create" {
		t.Errorf("%q %q %v", s, tool, err)
	}
	for _, name := range []string{"other.create", "bank", ".x", "bank.", "harnessly-app.data"} {
		if _, _, err := p.CanCallTool(name); err == nil {
			t.Errorf("%q: want refused", name)
		}
	}
}

func TestPublicAddress(t *testing.T) {
	for addr, public := range map[string]bool{
		"93.184.216.34": true, "8.8.8.8": true, "2606:4700:4700::1111": true, "1.1.1.1": true,
		"127.0.0.1": false, "10.0.0.5": false, "172.16.0.1": false, "192.168.1.1": false, "169.254.169.254": false,
		"0.0.0.0": false, "0.1.2.3": false, "100.64.0.1": false, "100.127.255.255": false, "192.0.0.8": false,
		"198.18.0.1": false, "198.19.255.255": false, "240.0.0.1": false, "255.255.255.255": false, "224.0.0.1": false,
		"192.0.2.1": false, "198.51.100.7": false, "203.0.113.9": false,
		"::1": false, "::": false, "fe80::1": false, "fc00::1": false, "fd12::1": false, "ff02::1": false,
		"::ffff:127.0.0.1": false, "::ffff:10.0.0.1": false, "::ffff:93.184.216.34": true,
		"64:ff9b::7f00:1": false, "64:ff9b::808:808": false, "64:ff9b:1::1": false,
		"2002:7f00:1::": false, "2002:808:808::": false, "2001:0:4136:e378:8000:63bf:3fff:fdd2": false,
		"2001:db8::1": false, "::7f00:1": false,
	} {
		ip := netip.MustParseAddr(addr)
		if got := publicAddress(ip); got != public {
			t.Errorf("%s: public=%v, want %v", addr, got, public)
		}
	}
}

// The check is made on the address the connection is going to, which is what a name resolved to at dial time.
func TestPublicOnlyIsTheDialersControl(t *testing.T) {
	for addr, refused := range map[string]bool{
		"127.0.0.1:443": true, "[::1]:443": true, "100.64.0.1:443": true, "93.184.216.34:443": false, "not-an-address": true,
	} {
		if err := dialControl(false)("tcp", addr, nil); (err != nil) != refused {
			t.Errorf("%s: %v", addr, err)
		}
	}
}

// A name that resolves to a local address is dialed to that address and refused there.
func TestCallToolDoesNotDialLocalNames(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("reached the local server") }))
	defer srv.Close()
	u := strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)
	_, err := CallTool(context.Background(), MCPServer{URL: u}, "t", nil)
	var be *BridgeError
	if !errors.As(err, &be) || be.Message != ToolRefused || strings.Contains(be.Message, "127.0.0.1") {
		t.Errorf("%+v", err)
	}
}

// What a view reads of a failed call is a short word; the detail stays for the log.
func TestToolFailuresAreShortWords(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	<-ctx.Done()
	for _, c := range []struct {
		err  error
		ctx  context.Context
		want string
	}{
		{errors.New("dial tcp 10.1.2.3:443: connect: connection refused"), context.Background(), ToolUnreachable},
		{context.DeadlineExceeded, context.Background(), ToolTimeout},
		{errors.New("x"), ctx, ToolTimeout},
		{&BridgeError{Code: CodeNetworkDenied, Message: "refused"}, context.Background(), ToolRefused},
	} {
		got := transportFailure(c.ctx, c.err)
		if got.Message != c.want || got.Detail == "" {
			t.Errorf("%v: %+v, want %s", c.err, got, c.want)
		}
	}
}

func TestCallToolRefusesPlainHTTP(t *testing.T) {
	_, err := CallTool(context.Background(), MCPServer{URL: "http://x.test/mcp"}, "t", nil)
	var be *BridgeError
	if !errors.As(err, &be) || be.Code != CodeHostNotAllowed {
		t.Errorf("%v", err)
	}
	// A loopback https server is no public address: the call must not reach it.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("reached the loopback") }))
	defer srv.Close()
	if _, err := CallTool(context.Background(), MCPServer{URL: srv.URL}, "t", nil); err == nil {
		t.Error("want the loopback refused")
	}
}

// The remote's own error text does not reach the page, and neither does a redirect lead the call on.
func TestRemoteFailuresAreWords(t *testing.T) {
	var elsewhere bool
	mux := http.NewServeMux()
	mux.HandleFunc("/rpc-error", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"message":"db password is hunter2"}}`))
	})
	mux.HandleFunc("/down", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "stack trace at 10.0.0.9", 500) })
	mux.HandleFunc("/redirect", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/elsewhere", http.StatusFound) })
	mux.HandleFunc("/elsewhere", func(http.ResponseWriter, *http.Request) { elsewhere = true })
	srv := httptest.NewServer(mux)
	defer srv.Close()
	for path, want := range map[string]string{"/rpc-error": ToolError, "/down": ToolUnreachable, "/redirect": ToolUnreachable} {
		c := &mcpConn{url: srv.URL + path, http: &http.Client{CheckRedirect: noRedirects}}
		_, err := c.rpc(context.Background(), 1, "initialize", map[string]any{})
		var be *BridgeError
		if !errors.As(err, &be) || be.Message != want || be.Detail == "" || strings.Contains(be.Error(), "hunter2") || strings.Contains(be.Error(), "10.0.0.9") {
			t.Errorf("%s: %+v", path, err)
		}
	}
	if elsewhere {
		t.Error("a redirect was followed")
	}
}

func TestReadAnswerEventStream(t *testing.T) {
	rec := httptest.NewRecorder()
	rec.Header().Set("Content-Type", "text/event-stream")
	_, _ = rec.WriteString("event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\"}\n\ndata: {\"jsonrpc\":\"2.0\",\"id\":2,\"result\":{\"ok\":true}}\n\n")
	raw, err := readAnswer(rec.Result(), 2)
	if err != nil || !strings.Contains(string(raw), `"ok":true`) {
		t.Errorf("%s %v", raw, err)
	}
	if _, err := readAnswer(rec.Result(), 7); err == nil {
		t.Error("want no answer for another id")
	}
}

func TestStorage(t *testing.T) {
	s := &Storage{Root: t.TempDir()}
	val := func(v string) json.RawMessage { return json.RawMessage(v) }

	if _, ok, err := s.Get("acme.demo", ScopeInstall, "", "k"); err != nil || ok {
		t.Fatalf("empty: %v %v", ok, err)
	}
	if err := s.Set("acme.demo", ScopeInstall, "", "k", val(`{"a":1}`)); err != nil {
		t.Fatal(err)
	}
	if got, ok, _ := s.Get("acme.demo", ScopeInstall, "", "k"); !ok || string(got) != `{"a":1}` {
		t.Errorf("%s %v", got, ok)
	}
	// Chats and apps are apart.
	_ = s.Set("acme.demo", ScopeChat, "c1", "k", val(`1`))
	_ = s.Set("acme.demo", ScopeChat, "c2", "k", val(`2`))
	if got, _, _ := s.Get("acme.demo", ScopeChat, "c2", "k"); string(got) != "2" {
		t.Errorf("chat c2: %s", got)
	}
	if _, ok, _ := s.Get("acme.other", ScopeInstall, "", "k"); ok {
		t.Error("another app sees the key")
	}
	if keys, _ := s.Keys("acme.demo", ScopeInstall, ""); len(keys) != 1 || keys[0] != "k" {
		t.Errorf("keys %v", keys)
	}
	if err := s.Delete("acme.demo", ScopeInstall, "", "k"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.Get("acme.demo", ScopeInstall, "", "k"); ok {
		t.Error("deleted key is there")
	}

	for name, call := range map[string]func() error{
		"owner escape": func() error { return s.Set("acme.demo", ScopeChat, "../x", "k", val(`1`)) },
		"no owner":     func() error { return s.Set("acme.demo", ScopeDoc, "", "k", val(`1`)) },
		"bad scope":    func() error { return s.Set("acme.demo", Scope("root"), "", "k", val(`1`)) },
		"bad app":      func() error { return s.Set("../x", ScopeInstall, "", "k", val(`1`)) },
		"empty key":    func() error { return s.Set("acme.demo", ScopeInstall, "", "", val(`1`)) },
		"not json":     func() error { return s.Set("acme.demo", ScopeInstall, "", "k", val(`{`)) },
	} {
		if err := call(); !errors.Is(err, ErrStorageArgs) {
			t.Errorf("%s: %v", name, err)
		}
	}

	big := val(`"` + strings.Repeat("x", scopeLimit[ScopeDoc]) + `"`)
	if err := s.Set("acme.demo", ScopeDoc, "d1", "k", big); !errors.Is(err, ErrQuota) {
		t.Errorf("over the limit: %v", err)
	}
}

// Many owners cannot add up past what the scope costs in all.
func TestStorageScopeTotal(t *testing.T) {
	s := &Storage{Root: t.TempDir()}
	chunk := json.RawMessage(`"` + strings.Repeat("x", scopeLimit[ScopeChat]-100) + `"`)
	var stored int64
	var refused bool
	for i := 0; i < 200 && !refused; i++ {
		err := s.Set("acme.demo", ScopeChat, "chat"+strconv.Itoa(i), "k", chunk)
		if errors.Is(err, ErrQuota) {
			refused = true
		} else if err != nil {
			t.Fatal(err)
		} else {
			stored += int64(len(chunk))
		}
	}
	if !refused || stored > scopeTotal[ScopeChat] {
		t.Errorf("refused=%v stored=%d, total allowed %d", refused, stored, scopeTotal[ScopeChat])
	}
	// Another app has its own total, and a rewrite of an existing area counts only the difference.
	if err := s.Set("acme.other", ScopeChat, "chat0", "k", chunk); err != nil {
		t.Errorf("another app: %v", err)
	}
	if err := s.Set("acme.demo", ScopeChat, "chat0", "k", json.RawMessage(`1`)); err != nil {
		t.Errorf("shrinking: %v", err)
	}
}
