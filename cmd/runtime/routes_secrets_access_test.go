package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/convo"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/secrets"
	"github.com/ly-solutions/harnessly/internal/typist"
	"github.com/ly-solutions/harnessly/internal/wire"
)

type safeAccessRig struct {
	t      *testing.T
	d      deps
	sf     *safe
	routes []gate.Route
	token  string
}

func newSafeAccessRig(t *testing.T) *safeAccessRig {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("RUNTIME_SECRETS_KEY_FILE", filepath.Join(t.TempDir(), "safe.key"))
	t.Setenv("RUNTIME_SECRETS_FILES_DIR", t.TempDir())
	token := "test-" + strings.Repeat("a7", 20)
	if err := os.WriteFile(filepath.Join(dir, "cli-token"), []byte(token), 0o600); err != nil {
		t.Fatal(err)
	}
	d := deps{DataDir: dir, Home: t.TempDir(), CLI: &gate.CLIToken{DataDir: dir, Headers: []string{"X-CLI-Token"}},
		SessionOK: func(r *http.Request) bool { return r.Header.Get("X-Test-Page") == "yes" }}
	sf := newSafe(d)
	t.Cleanup(sf.files.Close)
	rig := &safeAccessRig{t: t, d: d, sf: sf, token: token}
	// This test-only header stands for the caller identity verified by the socket resolver.
	identity := func(r *http.Request, session string) secrets.Use {
		actual := r.Header.Get("X-Test-Caller")
		if session != "" && session != "session-"+actual {
			return secrets.Use{}
		}
		switch actual {
		case "a", "b":
			return secrets.Use{Project: "project-" + actual, Conv: "chat-" + actual, Title: "Verified " + actual}
		case "unscoped":
			return secrets.Use{Conv: "chat-unscoped", Title: "Verified unscoped"}
		default:
			return secrets.Use{}
		}
	}
	rig.routes = sf.routes(d, func(session string) (string, string) {
		// A claimed session can supply legacy display text, never a verified project.
		return "claimed-" + session, "Unverified title"
	}, identity)
	return rig
}

func (rig *safeAccessRig) call(method, path string, body any, caller string, headers map[string]string) *httptest.ResponseRecorder {
	rig.t.Helper()
	var raw []byte
	if body != nil {
		var err error
		raw, err = json.Marshal(body)
		if err != nil {
			rig.t.Fatal(err)
		}
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(raw))
	if caller != "" {
		r.Header.Set("X-CLI-Token", rig.token)
		r.Header.Set("X-Test-Caller", caller)
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	for _, route := range rig.routes {
		if route.Match(r) {
			route.Handler.ServeHTTP(w, r)
			return w
		}
	}
	rig.t.Fatalf("no route for %s %s", method, path)
	return w
}

func (rig *safeAccessRig) put(name string, origins, projects []string) {
	rig.t.Helper()
	if _, _, err := rig.sf.store.Put(name, routeFakeValue); err != nil {
		rig.t.Fatal(err)
	}
	if _, err := rig.sf.store.Metadata(name, origins, projects); err != nil {
		rig.t.Fatal(err)
	}
}

func safeAccessDecode(t *testing.T, w *httptest.ResponseRecorder, out any) {
	t.Helper()
	if err := json.Unmarshal(w.Body.Bytes(), out); err != nil {
		t.Fatalf("invalid response JSON: %v", err)
	}
}

func safeAccessPublic(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if strings.Contains(w.Body.String(), routeFakeValue) {
		t.Error("response exposed the secret value")
	}
	for _, field := range []string{`"value":`, `"values":`, `"box":`, `"nonce":`} {
		if strings.Contains(w.Body.String(), field) {
			t.Errorf("public response exposes %s", field)
		}
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Error("safe response is cacheable")
	}
}

func TestSafeAccessRoutesListsUseVerifiedProject(t *testing.T) {
	rig := newSafeAccessRig(t)
	rig.put("key-a", []string{"https://example.com"}, []string{"project-a"})
	rig.put("key-b", nil, []string{"project-b"})
	rig.put("legacy-key", nil, nil)
	for _, tc := range []struct {
		caller string
		want   []string
	}{
		{"a", []string{"key-a", "legacy-key"}}, {"b", []string{"key-b", "legacy-key"}},
		{"unknown", []string{"legacy-key"}}, {"unscoped", []string{"legacy-key"}},
	} {
		t.Run(tc.caller, func(t *testing.T) {
			w := rig.call("GET", "/api/cli/secrets?session=session-a&project=project-a", nil, tc.caller, nil)
			if w.Code != http.StatusOK {
				t.Fatalf("list status %d", w.Code)
			}
			safeAccessPublic(t, w)
			var body struct {
				Secrets []map[string]json.RawMessage `json:"secrets"`
			}
			safeAccessDecode(t, w, &body)
			var names []string
			for _, item := range body.Secrets {
				var name string
				if err := json.Unmarshal(item["name"], &name); err != nil {
					t.Fatal(err)
				}
				names = append(names, name)
				for field := range item {
					if !slices.Contains([]string{"name", "createdAt", "updatedAt", "lastUsedAt"}, field) {
						t.Errorf("CLI metadata exposes %s", field)
					}
				}
			}
			if !slices.Equal(names, tc.want) {
				t.Errorf("visible names = %v, want %v", names, tc.want)
			}
		})
	}
	w := rig.call("GET", platformAPI+"/secrets", nil, "", map[string]string{"X-Test-Page": "yes"})
	if w.Code != http.StatusOK {
		t.Fatalf("page metadata status %d", w.Code)
	}
	safeAccessPublic(t, w)
	var body struct {
		Secrets []secrets.View `json:"secrets"`
	}
	safeAccessDecode(t, w, &body)
	if len(body.Secrets) != 3 || !slices.Equal(body.Secrets[0].Projects, []string{"project-a"}) || !slices.Equal(body.Secrets[0].Origins, []string{"https://example.com"}) {
		t.Errorf("page metadata missing access constraints: %+v", body.Secrets)
	}
}

func TestSafeAccessRoutesUseAndFileRejectForgedIdentity(t *testing.T) {
	for _, endpoint := range []string{"use", "file", "ask"} {
		t.Run(endpoint, func(t *testing.T) {
			for _, tc := range []struct{ name, caller, session string }{
				{"foreign-project", "b", "session-b"},
				{"missing-project", "unscoped", "session-unscoped"},
				{"unknown-caller", "unknown", "session-a"},
				{"mismatched-session", "b", "session-a"},
				{"fake-session", "a", "session-forged"},
			} {
				t.Run(tc.name, func(t *testing.T) {
					rig := newSafeAccessRig(t)
					rig.put("scoped-key", []string{"https://example.com"}, []string{"project-a"})
					before, err := os.ReadFile(filepath.Join(rig.d.DataDir, "secrets", "safe.json"))
					if err != nil {
						t.Fatal(err)
					}
					body := map[string]any{"name": "scoped-key", "names": []string{"scoped-key"}, "session": tc.session,
						"project": "project-a", "conv": "chat-a", "title": "Forged title", "wait": 0}
					w := rig.call("POST", "/api/cli/secrets/"+endpoint, body, tc.caller, nil)
					if endpoint == "ask" && tc.name == "foreign-project" {
						if w.Code != http.StatusOK {
							t.Fatalf("foreign verified project needs a grant card: %d", w.Code)
						}
						pending := rig.sf.asks.Pending()
						if len(pending) != 1 || !pending[0].Grant || pending[0].Project != "project-b" {
							t.Fatalf("foreign ask did not require verified project's grant: %+v", pending)
						}
					} else {
						if w.Code != http.StatusNotFound || strings.TrimSpace(w.Body.String()) != `{"error":"not-found"}` {
							t.Errorf("%s: forged identity response %d %s", endpoint, w.Code, w.Body.String())
						}
					}
					safeAccessPublic(t, w)
					after, err := os.ReadFile(filepath.Join(rig.d.DataDir, "secrets", "safe.json"))
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(before, after) {
						t.Error("denied access changed value or usage history")
					}
					if files, err := os.ReadDir(rig.sf.files.Dir); err != nil && !os.IsNotExist(err) || len(files) != 0 {
						t.Error("denied access created a secret file")
					}
				})
			}
		})
	}
}

func TestSafeAccessRoutesAllowedUseAndFile(t *testing.T) {
	rig := newSafeAccessRig(t)
	rig.put("scoped-key", nil, []string{"project-a"})
	for _, session := range []string{"session-a", ""} {
		w := rig.call("POST", "/api/cli/secrets/use", map[string]any{"names": []string{"scoped-key"}, "session": session, "project": "project-b"}, "a", nil)
		if w.Code != http.StatusOK {
			t.Fatalf("verified use status %d", w.Code)
		}
		var body struct {
			Values map[string]string `json:"values"`
		}
		safeAccessDecode(t, w, &body)
		if body.Values["scoped-key"] != routeFakeValue {
			t.Fatal("verified use returned a different value")
		}
	}
	views, err := rig.sf.store.List()
	if err != nil || len(views) != 1 || len(views[0].UsedBy) != 1 || views[0].UsedBy[0].Conv != "chat-a" || views[0].UsedBy[0].Title != "Verified a" {
		t.Fatalf("usage must come from resolver: %+v, %v", views, err)
	}
	w := rig.call("POST", "/api/cli/secrets/file", map[string]any{"name": "scoped-key", "session": "session-a"}, "a", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("verified file status %d", w.Code)
	}
	safeAccessPublic(t, w)
	var body struct {
		Path string `json:"path"`
	}
	safeAccessDecode(t, w, &body)
	if !strings.HasPrefix(body.Path, rig.sf.files.Dir+string(os.PathSeparator)) {
		t.Fatal("secret file escaped temporary test directory")
	}
	raw, err := os.ReadFile(body.Path)
	if err != nil || string(raw) != routeFakeValue {
		t.Fatalf("secret file contents differ: %v", err)
	}
	st, err := os.Stat(body.Path)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("secret file permissions: %v", err)
	}
}

func TestSafeAccessRoutesBatchAndAbsentAreIndistinguishable(t *testing.T) {
	rig := newSafeAccessRig(t)
	rig.put("key-a", nil, []string{"project-a"})
	rig.put("key-b", nil, []string{"project-b"})
	for _, endpoint := range []string{"use", "file"} {
		var previous string
		for _, name := range []string{"key-b", "absent-key"} {
			before, err := rig.sf.store.List()
			if err != nil {
				t.Fatal(err)
			}
			body := map[string]any{"name": name, "names": []string{"key-a", name}, "session": "session-a"}
			w := rig.call("POST", "/api/cli/secrets/"+endpoint, body, "a", nil)
			if w.Code != http.StatusNotFound {
				t.Errorf("%s %s: status %d", endpoint, name, w.Code)
			}
			safeAccessPublic(t, w)
			if previous != "" && w.Body.String() != previous {
				t.Error("foreign name is distinguishable from an absent name")
			}
			previous = w.Body.String()
			after, err := rig.sf.store.List()
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Errorf("denied batch recorded partial use: %v", err)
			}
		}
	}
}

func TestSafeAccessRoutesForeignAskRequiresGrantDespiteReplace(t *testing.T) {
	for _, replace := range []bool{false, true} {
		t.Run(map[bool]string{false: "normal", true: "replace"}[replace], func(t *testing.T) {
			rig := newSafeAccessRig(t)
			rig.put("scoped-key", []string{"https://example.com"}, []string{"project-a"})
			ask := map[string]any{"name": "scoped-key", "session": "session-b", "why": "sign in", "replace": replace, "origin": "HTTPS://OTHER.EXAMPLE.COM:443/", "wait": 0}
			w := rig.call("POST", "/api/cli/secrets/ask", ask, "b", nil)
			if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"status":"waiting"`) {
				t.Fatalf("ask did not wait: %d %s", w.Code, w.Body.String())
			}
			safeAccessPublic(t, w)
			pending := rig.sf.asks.Pending()
			if len(pending) != 1 {
				t.Fatalf("pending cards: %d", len(pending))
			}
			a := pending[0]
			if !a.Grant || a.Replace || a.Project != "project-b" || a.Conv != "chat-b" || a.Title != "Verified b" || a.Origin != "https://other.example.com" {
				t.Errorf("grant card must never request replacement, even with --replace: %+v", a)
			}
			page := map[string]string{"X-Test-Page": "yes"}
			card := platformAPI + "/secret-asks/" + a.ID
			before, err := os.ReadFile(filepath.Join(rig.d.DataDir, "secrets", "safe.json"))
			if err != nil {
				t.Fatal(err)
			}
			w = rig.call("POST", card+"/put", map[string]any{"value": routeFakeValue + "replacement"}, "", page)
			if w.Code != http.StatusBadRequest {
				t.Errorf("grant /put must reject: %d", w.Code)
			}
			after, err := os.ReadFile(filepath.Join(rig.d.DataDir, "secrets", "safe.json"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Error("grant /put changed the safe")
			}
			if _, ok := rig.sf.asks.Get(a.ID); !ok {
				t.Error("rejected /put consumed the grant card")
			}
			w = rig.call("POST", card+"/grant", map[string]any{"value": routeFakeValue + "replacement", "project": "project-forged", "origin": "https://evil.example.com"}, "", page)
			if w.Code != http.StatusOK {
				t.Fatalf("grant approval: %d %s", w.Code, w.Body.String())
			}
			safeAccessPublic(t, w)
			views, err := rig.sf.store.List()
			if err != nil || len(views) != 1 {
				t.Fatalf("list: %v", err)
			}
			v := views[0]
			if !slices.Equal(v.Projects, []string{"project-a", "project-b"}) || !slices.Equal(v.Origins, []string{"https://example.com", "https://other.example.com"}) {
				t.Errorf("grant must append card's scopes only: %+v", v)
			}
			if _, ok := rig.sf.asks.Get(a.ID); ok {
				t.Error("approved card remains answerable")
			}
			for _, project := range []string{"project-a", "project-b"} {
				got, err := rig.sf.store.Reveal([]string{"scoped-key"}, secrets.Use{Project: project})
				if err != nil || got["scoped-key"] != routeFakeValue {
					t.Errorf("grant changed value or denied %s: %v", project, err)
				}
			}
			w = rig.call("POST", card+"/grant", nil, "", page)
			if w.Code != http.StatusNotFound {
				t.Errorf("replayed grant: %d", w.Code)
			}
		})
	}
}

func TestSafeAccessRoutesScopedNormalAskRaceCannotOverwrite(t *testing.T) {
	rig := newSafeAccessRig(t)
	w := rig.call("POST", "/api/cli/secrets/ask", map[string]any{"name": "new-key", "session": "session-a", "why": "sign in", "origin": "https://example.com", "wait": 0}, "a", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("normal ask: %d", w.Code)
	}
	pending := rig.sf.asks.Pending()
	if len(pending) != 1 || pending[0].Grant || pending[0].Project != "project-a" {
		t.Fatalf("normal scoped ask: %+v", pending)
	}
	a := pending[0]
	// Simulate the other project's write while the page is displaying the normal card.
	rig.put("new-key", []string{"https://other.example.com"}, []string{"project-b"})
	before, err := os.ReadFile(filepath.Join(rig.d.DataDir, "secrets", "safe.json"))
	if err != nil {
		t.Fatal(err)
	}
	w = rig.call("POST", platformAPI+"/secret-asks/"+a.ID+"/put", map[string]any{"value": routeFakeValue + "replacement"}, "", map[string]string{"X-Test-Page": "yes"})
	if w.Code != http.StatusNotFound {
		t.Errorf("normal card overwrote newly foreign name: %d", w.Code)
	}
	after, err := os.ReadFile(filepath.Join(rig.d.DataDir, "secrets", "safe.json"))
	if err != nil || !bytes.Equal(before, after) {
		t.Errorf("raced card modified foreign value: %v", err)
	}
	if _, ok := rig.sf.asks.Get(a.ID); !ok {
		t.Error("failed put settled the normal card")
	}
}

func TestSafeAccessRoutesCardRejectsMovedOrDeletedConversation(t *testing.T) {
	for _, grant := range []bool{false, true} {
		for _, change := range []string{"moved", "deleted"} {
			t.Run(map[bool]string{false: "put", true: "grant"}[grant]+"/"+change, func(t *testing.T) {
				rig := newSafeAccessRig(t)
				if grant {
					rig.put("service-key", []string{"https://example.com"}, []string{"project-a"})
				}
				w := rig.call("POST", "/api/cli/secrets/ask", map[string]any{"name": "service-key", "session": "session-b", "why": "sign in", "origin": "https://other.example.com", "wait": 0}, "b", nil)
				if w.Code != http.StatusOK {
					t.Fatalf("open card: %d", w.Code)
				}
				pending := rig.sf.asks.Pending()
				if len(pending) != 1 || pending[0].Grant != grant {
					t.Fatalf("wrong pending card: %+v", pending)
				}
				a := pending[0]
				rig.sf.identity = func(id string) secrets.Use {
					if id != "chat-b" {
						t.Errorf("revalidated wrong conversation %q", id)
					}
					if change == "deleted" {
						return secrets.Use{}
					}
					return secrets.Use{Conv: "chat-b", Project: "project-c"}
				}
				before, err := rig.sf.store.List()
				if err != nil {
					t.Fatal(err)
				}
				action := "put"
				if grant {
					action = "grant"
				}
				w = rig.call("POST", platformAPI+"/secret-asks/"+a.ID+"/"+action, map[string]any{"value": routeFakeValue + "replacement"}, "", map[string]string{"X-Test-Page": "yes"})
				if w.Code != http.StatusNotFound {
					t.Errorf("stale %s card accepted after conversation %s: %d", action, change, w.Code)
				}
				safeAccessPublic(t, w)
				after, err := rig.sf.store.List()
				if err != nil || !reflect.DeepEqual(before, after) {
					t.Errorf("stale card changed the safe: %v", err)
				}
				if _, ok := rig.sf.asks.Get(a.ID); ok {
					t.Error("stale card remains answerable")
				}
			})
		}
	}
}

func TestSafeAccessRoutesWaitingAskRechecksReadyAccess(t *testing.T) {
	for _, change := range []string{"unchanged", "moved-project", "removed-secret", "revoked-project", "revoked-origin"} {
		t.Run(change, func(t *testing.T) {
			rig := newSafeAccessRig(t)
			var changed atomic.Bool
			resolver := func(_ *http.Request, _ string) secrets.Use {
				project := "project-a"
				if changed.Load() && change == "moved-project" {
					project = "project-b"
				}
				return secrets.Use{Conv: "chat-a", Project: project, Title: "Verified a"}
			}
			rig.routes = rig.sf.routes(rig.d, func(string) (string, string) { return "", "" }, resolver)
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				done <- rig.call("POST", "/api/cli/secrets/ask", map[string]any{"name": "new-key", "session": "session-a", "origin": "https://example.com", "why": "sign in", "wait": 2}, "a", nil)
			}()
			var pending []secrets.Ask
			deadline := time.Now().Add(time.Second)
			for time.Now().Before(deadline) {
				pending = rig.sf.asks.Pending()
				if len(pending) == 1 {
					break
				}
				time.Sleep(time.Millisecond)
			}
			if len(pending) != 1 {
				t.Fatal("request did not open a waiting card")
			}
			if change != "removed-secret" {
				project, origin := "project-a", "https://example.com"
				if change == "revoked-project" {
					project = "project-b"
				}
				if change == "revoked-origin" {
					origin = "https://other.example.com"
				}
				rig.put("new-key", []string{origin}, []string{project})
			}
			changed.Store(true)
			rig.sf.asks.Ready(pending[0].ID)
			select {
			case w := <-done:
				if w.Code != http.StatusOK {
					t.Fatalf("waiting ask status: %d", w.Code)
				}
				safeAccessPublic(t, w)
				var body struct {
					Status string `json:"status"`
				}
				safeAccessDecode(t, w, &body)
				want := secrets.AskDeclined
				if change == "unchanged" {
					want = secrets.AskReady
				}
				if body.Status != want {
					t.Errorf("ask after %s = %q, want %q", change, body.Status, want)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("waiting ask did not settle")
			}
		})
	}
}

func TestSafeAccessRoutesPageRequiresSessionAndSameOrigin(t *testing.T) {
	for _, prefix := range []string{"", "/agents"} {
		for _, tc := range []struct {
			method, path string
			body         any
		}{
			{"GET", platformAPI + "/secrets", nil},
			{"PUT", platformAPI + "/secrets/scoped-key", map[string]any{"value": routeFakeValue + "replacement"}},
			{"PATCH", platformAPI + "/secrets/scoped-key", map[string]any{"projects": []string{"project-forged"}}},
			{"DELETE", platformAPI + "/secrets/scoped-key", nil},
			{"GET", platformAPI + "/secret-asks", nil},
			{"POST", platformAPI + "/secret-asks/CARD/put", map[string]any{"value": routeFakeValue + "replacement"}},
			{"POST", platformAPI + "/secret-asks/CARD/grant", nil},
			{"POST", platformAPI + "/secret-asks/CARD/decline", nil},
		} {
			t.Run(prefix+tc.method+tc.path, func(t *testing.T) {
				rig := newSafeAccessRig(t)
				rig.put("scoped-key", nil, []string{"project-a"})
				a, _, err := rig.sf.asks.OpenScoped(secrets.Ask{Name: "scoped-key", Project: "project-b", Conv: "chat-b", Grant: true})
				if err != nil {
					t.Fatal(err)
				}
				agentToken, err := rig.d.CLI.IssueAgent("session-a", time.Hour)
				if err != nil {
					t.Fatal(err)
				}
				path := prefix + strings.ReplaceAll(tc.path, "CARD", a.ID)
				for _, auth := range []struct {
					name    string
					headers map[string]string
					status  int
				}{
					{"no-session", nil, http.StatusUnauthorized},
					{"cli-token", map[string]string{"X-CLI-Token": rig.token}, http.StatusUnauthorized},
					{"agent-token", map[string]string{"X-CLI-Token": agentToken}, http.StatusUnauthorized},
					{"cross-site", map[string]string{"X-Test-Page": "yes", "Sec-Fetch-Site": "cross-site"}, http.StatusForbidden},
					{"same-site", map[string]string{"X-Test-Page": "yes", "Sec-Fetch-Site": "same-site"}, http.StatusForbidden},
					{"foreign-origin", map[string]string{"X-Test-Page": "yes", "Origin": "https://evil.example.com"}, http.StatusForbidden},
				} {
					w := rig.call(tc.method, path, tc.body, "", auth.headers)
					if w.Code != auth.status {
						t.Errorf("%s: status %d, want %d", auth.name, w.Code, auth.status)
					}
					if strings.Contains(w.Body.String(), routeFakeValue) {
						t.Error("unauthorized page response exposed value")
					}
					if _, ok := rig.sf.asks.Get(a.ID); !ok {
						t.Error("unauthorized page settled a card")
					}
					v, err := rig.sf.store.Lookup("scoped-key", "project-a")
					if err != nil || !slices.Equal(v.Projects, []string{"project-a"}) {
						t.Errorf("unauthorized page changed metadata: %v", err)
					}
				}
			})
		}
	}
}

type safeAccessPublisher struct{}

func (*safeAccessPublisher) Publish(wire.Channel, wire.Event) int64 { return 0 }

type safeAccessMachine struct{}

func (safeAccessMachine) Pane(string) typist.Pane  { return nil }
func (safeAccessMachine) Probe(string) convo.Probe { return convo.Probe{} }

func TestSafeAccessRealRoutesDoNotTrustSessionWithoutSocketCaller(t *testing.T) {
	rig := newSafeAccessRig(t)
	rt := convo.New(convo.Deps{MachineID: "local", Publisher: &safeAccessPublisher{}, Machine: safeAccessMachine{}})
	t.Cleanup(rt.Stop)
	cwd := t.TempDir()
	if n := rt.Import([]convo.ImportTab{{ID: "tab-safe_123", Session: "session-a", Title: "Verified record", Provider: wire.ProviderClaude, Cwd: cwd}}, nil); n != 1 {
		t.Fatalf("imported %d conversations", n)
	}
	if n := rt.ImportProjects([]convo.LegacyProject{{ID: "project-a", Name: "A"}}, map[string]string{"tab-safe_123": "project-a"}); n != 1 {
		t.Fatalf("assigned %d projects", n)
	}
	c := &core{}
	c.v2.Store(&v2Live{rt: rt})
	use := secretIdentity(c, "tab-safe_123")
	// The actor publishes its read snapshot just after acknowledging ImportProjects.
	deadline := time.Now().Add(time.Second)
	for use.Project == "" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
		use = secretIdentity(c, "tab-safe_123")
	}
	if use.Project != "project-a" || use.Conv != "tab-safe_123" || use.Title != "Verified record" {
		t.Fatalf("record identity: %+v", use)
	}
	if got := secretIdentity(c, "absent"); got != (secrets.Use{}) {
		t.Errorf("unknown record has identity: %+v", got)
	}
	// Keep the real route factory on our temporary safe; no core, terminal, or service is started.
	safesMu.Lock()
	safesFor[rig.d.CLI] = rig.sf
	safesMu.Unlock()
	t.Cleanup(func() { safesMu.Lock(); delete(safesFor, rig.d.CLI); safesMu.Unlock() })
	rig.routes = secretRoutes(rig.d, c)
	rig.put("scoped-key", nil, []string{"project-a"})
	for _, endpoint := range []string{"use", "file", "ask"} {
		w := rig.call("POST", "/api/cli/secrets/"+endpoint, map[string]any{"session": "session-a", "project": "project-a", "name": "scoped-key", "names": []string{"scoped-key"}, "wait": 0}, "a", nil)
		if w.Code != http.StatusNotFound {
			t.Errorf("real %s trusted claimed session without socket caller: %d", endpoint, w.Code)
		}
		safeAccessPublic(t, w)
	}
	w := rig.call("GET", "/api/cli/secrets?session=session-a", nil, "a", nil)
	if w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != `{"secrets":[]}` {
		t.Errorf("real list trusted claimed session: %d %s", w.Code, w.Body.String())
	}
}

func TestSafeAccessRoutesOriginOnlyGrant(t *testing.T) {
	for _, tc := range []struct {
		name, caller string
		projects     []string
	}{
		{"scoped", "a", []string{"project-a"}},
		{"unscoped-project", "a", nil},
		{"projectless", "unscoped", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rig := newSafeAccessRig(t)
			rig.put("service-key", []string{"https://example.com"}, tc.projects)
			w := rig.call("POST", "/api/cli/secrets/ask", map[string]any{"name": "service-key", "origin": "https://other.example.com", "wait": 0}, tc.caller, nil)
			if w.Code != http.StatusOK {
				t.Fatalf("ask: %d %s", w.Code, w.Body.String())
			}
			pending := rig.sf.asks.Pending()
			if len(pending) != 1 || !pending[0].OriginGrant || pending[0].Grant || pending[0].Replace {
				t.Fatalf("origin card: %+v", pending)
			}
			a := pending[0]
			page := map[string]string{"X-Test-Page": "yes"}
			card := platformAPI + "/secret-asks/" + a.ID
			if w := rig.call("POST", card+"/put", map[string]any{"value": routeFakeValue + "replacement"}, "", page); w.Code != http.StatusBadRequest {
				t.Fatalf("origin grant accepted /put: %d", w.Code)
			}
			w = rig.call("POST", card+"/grant", map[string]any{"value": routeFakeValue + "replacement", "project": "forged", "origin": "https://evil.example.com"}, "", page)
			if w.Code != http.StatusOK {
				t.Fatalf("grant: %d %s", w.Code, w.Body.String())
			}
			safeAccessPublic(t, w)
			v, err := rig.sf.store.Lookup(a.Name, a.Project)
			if err != nil || !slices.Equal(v.Projects, tc.projects) || !slices.Equal(v.Origins, []string{"https://example.com", a.Origin}) {
				t.Fatalf("grant changed wrong scopes: %+v, %v", v, err)
			}
			got, err := rig.sf.store.Reveal([]string{a.Name}, secrets.Use{Project: a.Project, Origin: a.Origin})
			if err != nil || got[a.Name] != routeFakeValue {
				t.Fatalf("origin grant replaced value: %v", err)
			}
			w = rig.call("POST", "/api/cli/secrets/ask", map[string]any{"name": a.Name, "origin": a.Origin, "wait": 0}, tc.caller, nil)
			if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"status":"ready"`) {
				t.Fatalf("authorized retry: %d %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestSafeAccessRoutesAccessibleReplaceStillReplacesValue(t *testing.T) {
	rig := newSafeAccessRig(t)
	rig.put("service-key", []string{"https://example.com"}, nil)
	w := rig.call("POST", "/api/cli/secrets/ask", map[string]any{"name": "service-key", "origin": "https://other.example.com", "replace": true, "wait": 0}, "unscoped", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("ask: %d", w.Code)
	}
	pending := rig.sf.asks.Pending()
	if len(pending) != 1 || pending[0].NeedsGrant() || !pending[0].Replace {
		t.Fatalf("replacement became permission: %+v", pending)
	}
	a := pending[0]
	card := platformAPI + "/secret-asks/" + a.ID
	page := map[string]string{"X-Test-Page": "yes"}
	if w := rig.call("POST", card+"/grant", nil, "", page); w.Code != http.StatusBadRequest {
		t.Fatalf("replacement accepted /grant: %d", w.Code)
	}
	w = rig.call("POST", card+"/put", map[string]any{"value": routeFakeValue + "replacement"}, "", page)
	if w.Code != http.StatusOK {
		t.Fatalf("replace: %d %s", w.Code, w.Body.String())
	}
	got, err := rig.sf.store.Reveal([]string{a.Name}, secrets.Use{Origin: a.Origin})
	if err != nil || got[a.Name] != routeFakeValue+"replacement" {
		t.Fatalf("value was not replaced: %v", err)
	}
}
