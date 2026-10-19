package main

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"harnessly/internal/gate"
	"harnessly/internal/jsvalue"
	"harnessly/internal/push"
	"harnessly/internal/tmux"
)

// The machines slice answers with the core off, and its push routes write through the sender the core sends with.
func TestMachinesSliceWithoutTheCore(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(dir, "claude"))
	t.Setenv("CODEX_HOME", filepath.Join(dir, "codex"))
	data := filepath.Join(dir, "data")
	_ = os.MkdirAll(data, 0o700)
	_ = os.WriteFile(filepath.Join(dir, "state-mode.json"), []byte(`{"goSlices":["machines"]}`), 0o600)
	old, err := net.Listen("tcp", "127.0.0.1:0") // the previous runtime is up: health is answered here
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	hooks := hookConfig{PushSubject: "mailto:ops@example.org", PushTagPrefix: "app:"}
	sender := newSharedPusher(data, hooks)
	if newPusher(data, sender, hooks) != sender {
		t.Fatal("the core must send with the shared sender")
	}
	sessionOK := func(r *http.Request) bool { return strings.Contains(r.Header.Get("Cookie"), "session-token=good") }
	d := deps{Home: dir, DataDir: data, Tmux: tmux.NewConfig(tmux.Config{Names: tmux.Names{Socket: "hs-test-machines", Prefix: "hs-"}, Home: dir}),
		SessionOK: sessionOK,
		APIRefusal: func(r *http.Request) (int, any, http.Header, bool) {
			if sessionOK(r) {
				return 0, nil, nil, false
			}
			return 401, map[string]string{"error": "Unauthorized"}, http.Header{"Content-Type": {"application/json"}}, true
		}}
	f, err := gate.New(gate.Config{Listen: "127.0.0.1:0", Upstream: "127.0.0.1:1", DataDir: dir, CountEvery: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	f.Add(f.AsRoutes(machineSliceRoutes(d, "127.0.0.1:0", old.Addr().String(), sender, hooks)...)...)
	do := func(method, path, body string) (int, string) {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Cookie", "session-token=good")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		f.ServeHTTP(rec, req)
		b, _ := io.ReadAll(rec.Result().Body)
		return rec.Code, string(b)
	}
	if code, body := do("GET", "/api/health", ""); code != 200 || !strings.Contains(body, `"app":"harnessly"`) {
		t.Fatalf("%d %s", code, body)
	}
	if code, body := do("GET", "/api/push/vapid-key", ""); code != 200 || !strings.HasPrefix(body, `{"publicKey":"B`) {
		t.Fatalf("%d %s", code, body)
	}
	if code, _ := do("POST", "/api/push/subscribe", `{"endpoint":"https://push.example.org/1","keys":{"p256dh":"P","auth":"A"},"deviceId":"d1"}`); code != 200 {
		t.Fatal(code)
	}
	if subs := sender.Store.Subscriptions(); len(subs) != 1 || subs[0].DeviceID != "d1" {
		t.Fatalf("%+v", subs)
	}
	code, body := do("GET", platformAPI+"/machines", "")
	v, _ := jsvalue.Parse([]byte(body))
	if code != 200 || v == nil {
		t.Fatalf("%d %s", code, body)
	}
	_ = push.SubscriptionsFile
}
