package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

func TestReadHubEnv(t *testing.T) {
	dir := t.TempDir()
	body := `{"rev":3,"hash":"h","items":[
	 {"itemId":"a","kind":"skill","name":"Review","layer":"org","pinned":true,"source":{"secretRefs":["X"]}},
	 {"itemId":"b","kind":"mcp","name":"Tracker","layer":"team","pinned":false},
	 {"itemId":"c","kind":"repo","name":"app","layer":"org"},
	 {"itemId":"d","kind":"mcp","name":"Wiki","layer":"tenant"}],
	 "applied":[{"itemId":"a","status":"ok"},{"itemId":"b","status":"error","message":"нет секрета"}]}`
	if err := os.WriteFile(filepath.Join(dir, "environment.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	got := readHubEnv([]string{filepath.Join(dir, "none"), dir})
	if len(got) != 3 {
		t.Fatalf("repo must be left out: %+v", got)
	}
	want := []hubEnvItem{
		{ItemID: "a", Kind: "skill", Name: "Review", Layer: "org", Pinned: true, Status: "ok"},
		{ItemID: "b", Kind: "mcp", Name: "Tracker", Layer: "team", Status: "error", Message: "нет секрета"},
		{ItemID: "d", Kind: "mcp", Name: "Wiki", Layer: "tenant", Status: "pending"},
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d: %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestReadHubEnvNoFile(t *testing.T) {
	if got := readHubEnv([]string{t.TempDir()}); got == nil || len(got) != 0 {
		t.Fatalf("want an empty list, got %#v", got)
	}
}

func TestHubEnvOwnGoesThroughTheAgentAndRemembersWhatWasSwitchedOff(t *testing.T) {
	t.Setenv("RUNTIME_HUB_ENV_UI", "on")
	t.Setenv("RUNTIME_HUB_ENV_OWN", "on")
	dir := t.TempDir()
	body := `{"items":[{"itemId":"a","kind":"skill","name":"Review","layer":"org"},{"itemId":"u","kind":"mcp","name":"Mine","layer":"user"}],"applied":[]}`
	if err := os.WriteFile(filepath.Join(dir, "environment.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	var asked []hubproto.EnvOwnRequest
	reply := hubproto.EnvOwnResult{Status: 200}
	call := func(p hubproto.EnvOwnRequest) (hubproto.EnvOwnResult, error) {
		asked = append(asked, p)
		return reply, nil
	}
	d := deps{DataDir: dir, SessionOK: func(r *http.Request) bool { return r.Header.Get("X-Session") == "1" }}
	routes := hubEnvRoutesWith(d, call)
	do := func(method, path, body string, session bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if session {
			r.Header.Set("X-Session", "1")
		}
		w := httptest.NewRecorder()
		for _, rt := range routes {
			if rt.Match(r) {
				rt.Handler.ServeHTTP(w, r)
				return w
			}
		}
		t.Fatalf("no route for %s %s", method, path)
		return nil
	}
	base := platformAPI + "/hub-env"
	list := func() hubEnvAnswer {
		var out hubEnvAnswer
		if err := json.Unmarshal(do("GET", base, "", true).Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	if w := do("POST", base+"/own", `{"kind":"skill","name":"x","source":{"body":"x"}}`, false); w.Code != 401 {
		t.Fatalf("without a session: %d", w.Code)
	}
	if got := list(); !got.CanOwn || len(got.Items) != 2 || got.Items[0].Own || !got.Items[1].Own {
		t.Fatalf("list = %+v", got)
	}
	reply = hubproto.EnvOwnResult{Status: 201, ItemID: "n"}
	if w := do("POST", base+"/own", `{"kind":"skill","name":"Notes","source":{"body":"x"}}`, true); w.Code != 201 || !strings.Contains(w.Body.String(), `"n"`) {
		t.Fatalf("add: %d %s", w.Code, w.Body)
	}
	if w := do("POST", base+"/own", `{"kind":"repo","name":"app","source":{}}`, true); w.Code != 400 {
		t.Fatalf("a repo of one's own: %d", w.Code)
	}
	// The hub's refusal passes through as it is.
	reply = hubproto.EnvOwnResult{Status: 409, Code: "given", Message: "Такое уже есть"}
	if w := do("POST", base+"/own", `{"kind":"skill","name":"Review","source":{"body":"x"}}`, true); w.Code != 409 || !strings.Contains(w.Body.String(), "given") {
		t.Fatalf("refusal: %d %s", w.Code, w.Body)
	}
	// Switched off, the item leaves the machine's list but stays on the page to be switched back on.
	reply = hubproto.EnvOwnResult{Status: 200}
	if w := do("PUT", base+"/a", `{"off":true}`, true); w.Code != 200 {
		t.Fatalf("switch: %d %s", w.Code, w.Body)
	}
	if err := os.WriteFile(filepath.Join(dir, "environment.json"), []byte(`{"items":[{"itemId":"u","kind":"mcp","name":"Mine","layer":"user"}],"applied":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got := list()
	if len(got.Items) != 2 || got.Items[1].ItemID != "a" || !got.Items[1].Off {
		t.Fatalf("after switch-off = %+v", got.Items)
	}
	if w := do("DELETE", base+"/own/u", "", true); w.Code != 204 {
		t.Fatalf("remove: %d", w.Code)
	}
	want := []string{hubproto.EnvOwnAdd, hubproto.EnvOwnAdd, hubproto.EnvOwnSwitch, hubproto.EnvOwnRemove}
	if len(asked) != len(want) {
		t.Fatalf("asked %+v", asked)
	}
	for i, a := range want {
		if asked[i].Action != a {
			t.Errorf("call %d = %s, want %s", i, asked[i].Action, a)
		}
	}
	if !asked[2].Off || asked[2].ItemID != "a" || asked[3].ItemID != "u" {
		t.Errorf("calls = %+v", asked)
	}

	// Without the flag the page cannot change anything.
	t.Setenv("RUNTIME_HUB_ENV_OWN", "")
	if w := do("PUT", base+"/a", `{"off":false}`, true); w.Code != 404 {
		t.Fatalf("without the flag: %d", w.Code)
	}
	if list().CanOwn {
		t.Fatal("CanOwn without the flag")
	}
}
