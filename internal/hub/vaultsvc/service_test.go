package vaultsvc_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hub/vaultsvc"
)

const goodKey = "v1:AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="

var (
	alice = vaultsvc.Person{ID: "u-alice", Email: "alice@example.com"}
	bob   = vaultsvc.Person{ID: "u-bob", Email: "bob@example.com"}
)

func rig(t *testing.T, keys string) (*vaultsvc.Service, *store.DB) {
	t.Helper()
	db, err := store.Open(context.Background(), "sqlite://"+filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	cfg := vaultsvc.Config{SharedMembers: vaultsvc.Emails("alice@example.com,bob@example.com"), SharedEditors: vaultsvc.Emails("alice@example.com")}
	if keys != "" {
		kr, err := vaultsvc.ParseKeys(keys)
		if err != nil {
			t.Fatal(err)
		}
		cfg.Keyring = kr
	}
	return vaultsvc.New(db, cfg, nil, nil), db
}

func call(s *vaultsvc.Service, p vaultsvc.Person, method, route string, body any) (int, map[string]any) {
	var rd *strings.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = strings.NewReader(string(b))
	} else {
		rd = strings.NewReader("")
	}
	r := httptest.NewRequest(method, "/api/platform/vault/"+route, rd)
	r.Header.Set("X-Hl-Vault", "1")
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	s.Serve(w, r, p, route)
	out := map[string]any{}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func TestParseKeys(t *testing.T) {
	for _, bad := range []string{"", "v1", "V1:" + goodKey[3:], "v1:AAAA", goodKey + "," + goodKey} {
		if _, err := vaultsvc.ParseKeys(bad); err == nil {
			t.Errorf("%q must be refused", bad)
		}
	}
	if kr, err := vaultsvc.ParseKeys("v2:" + goodKey[3:] + "," + goodKey); err != nil || kr.Current != "v2" || len(kr.Keys) != 2 {
		t.Fatalf("two keys: %v %v", kr, err)
	}
}

func TestSafeIsOffWithoutKey(t *testing.T) {
	s, _ := rig(t, "")
	code, out := call(s, alice, "GET", "status", nil)
	if code != 200 || out["enabled"] != false {
		t.Fatalf("status %d %v", code, out)
	}
	if code, _ := call(s, alice, "GET", "entries", nil); code != http.StatusServiceUnavailable {
		t.Fatalf("entries %d", code)
	}
}

func TestEntriesLifecycleAndIsolation(t *testing.T) {
	s, db := rig(t, goodKey)
	_, st := call(s, alice, "GET", "status", nil)
	if st["enabled"] != true || st["canShare"] != true {
		t.Fatalf("status %v", st)
	}
	code, out := call(s, alice, "POST", "entries", map[string]any{"origin": "https://example.com", "login": "al", "password": "hunter2", "label": "Site"})
	if code != 201 {
		t.Fatalf("create %d %v", code, out)
	}
	id := out["entry"].(map[string]any)["id"].(string)
	if strings.Contains(toJSON(out), "hunter2") {
		t.Fatal("password in answer")
	}
	// The row holds ciphertext only.
	var secret string
	if err := db.QueryRow(context.Background(), `SELECT secret FROM vault_entries WHERE id = ?`, id).Scan(&secret); err != nil {
		t.Fatal(err)
	}
	if raw, _ := base64.StdEncoding.DecodeString(secret); strings.Contains(string(raw), "hunter2") {
		t.Fatal("stored in clear")
	}
	if code, _ := call(s, alice, "POST", "entries", map[string]any{"origin": "https://example.com", "login": "al", "password": "x"}); code != 409 {
		t.Fatalf("duplicate %d", code)
	}
	if code, _ := call(s, alice, "POST", "entries", map[string]any{"origin": "http://example.com/path", "login": "al", "password": "x"}); code != 400 {
		t.Fatalf("bad origin %d", code)
	}
	// Another person sees nothing of it and cannot touch it.
	if _, l := call(s, bob, "GET", "entries", nil); len(l["entries"].([]any)) != 0 {
		t.Fatalf("bob sees %v", l)
	}
	if code, _ := call(s, bob, "DELETE", "entries/"+id, nil); code != 404 {
		t.Fatalf("bob delete %d", code)
	}
	if code, _ := call(s, bob, "PATCH", "entries/"+id, map[string]any{"label": "x"}); code != 404 {
		t.Fatalf("bob patch %d", code)
	}
	// A change of the login re-seals under the new binding, and the entry still opens.
	if code, o := call(s, alice, "PATCH", "entries/"+id, map[string]any{"login": "al2"}); code != 200 || o["entry"].(map[string]any)["login"] != "al2" {
		t.Fatalf("patch %d %v", code, o)
	}
	// Shared: bob sees, only alice (editor) changes.
	if code, _ := call(s, bob, "POST", "entries", map[string]any{"origin": "https://shared.example.com", "login": "s", "password": "p", "shared": true}); code != 403 {
		t.Fatalf("bob share %d", code)
	}
	_, sh := call(s, alice, "POST", "entries", map[string]any{"origin": "https://shared.example.com", "login": "s", "password": "p", "shared": true})
	shID := sh["entry"].(map[string]any)["id"].(string)
	if _, l := call(s, bob, "GET", "entries", nil); len(l["entries"].([]any)) != 1 {
		t.Fatalf("bob shared %v", l)
	}
	if code, _ := call(s, bob, "DELETE", "entries/"+shID, nil); code != 403 {
		t.Fatalf("bob delete shared %d", code)
	}
	if code, _ := call(s, alice, "DELETE", "entries/"+id, nil); code != 204 {
		t.Fatalf("delete %d", code)
	}
	if _, a := call(s, alice, "GET", "audit", nil); len(a["events"].([]any)) < 3 {
		t.Fatalf("audit %v", a)
	}
}

func TestForgedAndWrongType(t *testing.T) {
	s, _ := rig(t, goodKey)
	r := httptest.NewRequest("GET", "/api/platform/vault/entries", nil)
	w := httptest.NewRecorder()
	s.Serve(w, r, alice, "entries")
	if w.Code != 403 {
		t.Fatalf("no header: %d", w.Code)
	}
	r = httptest.NewRequest("GET", "/api/platform/vault/entries", nil)
	r.Header.Set("X-Hl-Vault", "1")
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	w = httptest.NewRecorder()
	s.Serve(w, r, alice, "entries")
	if w.Code != 403 {
		t.Fatalf("cross-site: %d", w.Code)
	}
	r = httptest.NewRequest("POST", "/api/platform/vault/entries", strings.NewReader("{}"))
	r.Header.Set("X-Hl-Vault", "1")
	w = httptest.NewRecorder()
	s.Serve(w, r, alice, "entries")
	if w.Code != 415 {
		t.Fatalf("type: %d", w.Code)
	}
}

func TestFillTokenAndRedeem(t *testing.T) {
	s, _ := rig(t, goodKey)
	_, out := call(s, alice, "POST", "entries", map[string]any{"origin": "https://example.com", "login": "al", "password": "hunter2"})
	id := out["entry"].(map[string]any)["id"].(string)
	slot, nonce := "https://slot-1.example.net", strings.Repeat("ab", 16)
	fill := map[string]any{"for": "person", "origin": "https://example.com", "slot": slot, "nonce": nonce}
	code, m := call(s, alice, "POST", "entries/"+id+"/fill", fill)
	if code != 200 {
		t.Fatalf("mint %d %v", code, m)
	}
	tok := m["token"].(string)
	req := vaultsvc.RedeemRequest{Token: tok, Nonce: nonce, SlotOrigin: slot, TargetOrigin: "https://example.com", Person: alice}
	if res := s.Redeem(context.Background(), req); !res.OK || res.Login != "al" || res.Password != "hunter2" {
		t.Fatalf("redeem %+v", res)
	}
	// One time only.
	if res := s.Redeem(context.Background(), req); res.OK || res.Status != 410 {
		t.Fatalf("second redeem %+v", res)
	}
	// A token is spent by a wrong attempt too, and a wrong site or document opens nothing.
	for name, mut := range map[string]func(*vaultsvc.RedeemRequest){
		"other person": func(r *vaultsvc.RedeemRequest) { r.Person = bob },
		"other slot":   func(r *vaultsvc.RedeemRequest) { r.SlotOrigin = "https://slot-2.example.net" },
		"other doc":    func(r *vaultsvc.RedeemRequest) { r.Nonce = strings.Repeat("cd", 16) },
		"other site":   func(r *vaultsvc.RedeemRequest) { r.TargetOrigin = "https://evil.example.org" },
		"port target":  func(r *vaultsvc.RedeemRequest) { r.TargetOrigin = "" },
	} {
		_, m := call(s, alice, "POST", "entries/"+id+"/fill", fill)
		r := req
		r.Token = m["token"].(string)
		mut(&r)
		if res := s.Redeem(context.Background(), r); res.OK || strings.Contains(res.Error, "hunter2") {
			t.Fatalf("%s: %+v", name, res)
		}
	}
	// Bob cannot even ask for a token for alice's entry.
	if code, _ := call(s, bob, "POST", "entries/"+id+"/fill", fill); code != 404 {
		t.Fatalf("bob mint %d", code)
	}
}

func TestAgentNeedsApprovalThenGrant(t *testing.T) {
	s, _ := rig(t, goodKey)
	_, out := call(s, alice, "POST", "entries", map[string]any{"origin": "https://example.com", "login": "al", "password": "p"})
	id := out["entry"].(map[string]any)["id"].(string)
	fill := map[string]any{"for": "agent", "agent": "m1/tab-1", "origin": "https://example.com", "slot": "https://slot-1.example.net", "nonce": strings.Repeat("ab", 16)}
	code, m := call(s, alice, "POST", "entries/"+id+"/fill", fill)
	if code != 409 || m["needsApproval"] != true {
		t.Fatalf("no grant: %d %v", code, m)
	}
	_, ap := call(s, alice, "POST", "entries/"+id+"/approve", map[string]any{"agent": "m1/tab-1", "origin": "https://example.com"})
	fill["approval"] = ap["approval"]
	if code, _ := call(s, alice, "POST", "entries/"+id+"/fill", fill); code != 200 {
		t.Fatalf("approved: %d", code)
	}
	// The grant stands: the next fill needs no approval, until it is revoked.
	delete(fill, "approval")
	if code, _ := call(s, alice, "POST", "entries/"+id+"/fill", fill); code != 200 {
		t.Fatalf("granted: %d", code)
	}
	if code, _ := call(s, alice, "DELETE", "entries/"+id+"/grants", nil); code != 204 {
		t.Fatalf("revoke %d", code)
	}
	if code, _ := call(s, alice, "POST", "entries/"+id+"/fill", fill); code != 409 {
		t.Fatalf("after revoke: %d", code)
	}
}

func toJSON(v any) string { b, _ := json.Marshal(v); return string(b) }
