package tbotjoin

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hub/store/storetest"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

func edKey(t *testing.T) string {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	k, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(k))) + " tbot"
}

func TestCheck(t *testing.T) {
	key := edKey(t)
	m := store.Machine{ID: "m1", Hostname: "fleet-dev-2"}
	r, err := Check(m, hubproto.TbotJoin{Bot: "agent-anna", Token: "agent-anna-dev-2", Machine: "fleet-dev-2", PublicKey: key})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(r.Fingerprint, "SHA256:") || strings.HasSuffix(r.PublicKey, " tbot") {
		t.Fatalf("fingerprint %q, key %q: want SHA256 and the key without its comment", r.Fingerprint, r.PublicKey)
	}
	if _, err := Check(store.Machine{Hostname: "dev-2.example.org"}, hubproto.TbotJoin{Bot: "agent-anna", Token: "agent-anna-dev-2", PublicKey: key}); err != nil {
		t.Fatalf("a host that is the machine part with a domain: %v", err)
	}
	rsaKey, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	rk, _ := ssh.NewPublicKey(&rsaKey.PublicKey)
	bad := []hubproto.TbotJoin{
		{Bot: "agent-anna", Token: "agent-anna-runner-1", PublicKey: key},                               // another machine's token
		{Bot: "agent-anna", Token: "agent-boris-dev-2", PublicKey: key},                                 // another bot's token
		{Bot: "Agent-anna", Token: "Agent-anna-dev-2", PublicKey: key},                                  // not a name
		{Bot: "agent-anna", Token: "agent-anna-dev-2", PublicKey: "ssh-ed25519 nope"},                   // not a key
		{Bot: "agent-anna", Token: "agent-anna-dev-2", PublicKey: string(ssh.MarshalAuthorizedKey(rk))}, // not Ed25519
		{Bot: "agent-anna", Token: "agent-anna-dev-2", PublicKey: key + "\n" + key},                     // two keys
	}
	for i, in := range bad {
		if _, err := Check(m, in); err == nil {
			t.Errorf("%d: %+v passed", i, in)
		}
	}
}

type fakeCluster struct {
	mu      sync.Mutex
	owners  map[string][]string
	tokens  map[string]string // name → public key
	foreign map[string]bool
	err     error
}

func (c *fakeCluster) BotOwners(_ context.Context, bot string) ([]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return nil, c.err
	}
	o, ok := c.owners[bot]
	if !ok {
		return nil, ErrNoBot
	}
	return o, nil
}

func (c *fakeCluster) PutToken(_ context.Context, name, _, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.foreign[name] {
		return ErrNotOurs
	}
	c.tokens[name] = key
	return nil
}

func (c *fakeCluster) DeleteToken(_ context.Context, name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.foreign[name] {
		return ErrNotOurs
	}
	delete(c.tokens, name)
	return nil
}

type told struct {
	machine string
	state   hubproto.TbotJoinState
}

type fakeReg struct{ ch chan told }

func (r fakeReg) Do(_ context.Context, machineID, op string, params any, _ time.Duration) (json.RawMessage, error) {
	if op != hubproto.OpTbotJoined {
		return nil, httpx.ErrNotFound
	}
	r.ch <- told{machineID, params.(hubproto.TbotJoinState)}
	return json.RawMessage(`{}`), nil
}

type fixture struct {
	svc     *Service
	cluster *fakeCluster
	reg     fakeReg
	mux     *http.ServeMux
	anna    store.User
	boris   store.User
	m       store.Machine
	on      map[string]bool
	bells   []string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db := storetest.Open(t)
	ctx := context.Background()
	f := &fixture{cluster: &fakeCluster{owners: map[string][]string{"agent-anna": {"Anna@Example.org"}}, tokens: map[string]string{}, foreign: map[string]bool{}},
		reg: fakeReg{ch: make(chan told, 8)}, mux: http.NewServeMux(), on: map[string]bool{"anna@example.org": true, "boris@example.org": true}}
	f.anna = store.User{ID: store.NewID(), Email: "anna@example.org", Name: "Anna", CreatedAt: time.Now()}
	f.boris = store.User{ID: store.NewID(), Email: "boris@example.org", Name: "Boris", CreatedAt: time.Now()}
	for _, u := range []*store.User{&f.anna, &f.boris} {
		if err := db.S().CreateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	f.m = store.Machine{ID: "m1", Name: "dev two", Hostname: "fleet-dev-2"}
	f.svc = New(Deps{DB: db, Cluster: f.cluster, On: true, Reg: f.reg, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Gate: GateFunc(func(_ context.Context, feature, person, machine string) bool {
			return feature == Switch && f.on[person] && machine == "m1"
		}),
		MayOpen: func(_ context.Context, userID, machineID string) error {
			if machineID != "m1" {
				return httpx.ErrNotFound
			}
			return nil
		},
		Require: func(h func(http.ResponseWriter, *http.Request, *auth.Principal) error) httpx.Handler {
			return func(w http.ResponseWriter, r *http.Request) error {
				u := f.anna
				if r.Header.Get("X-Who") == "boris" {
					u = f.boris
				}
				return h(w, r, &auth.Principal{User: u})
			}
		},
		Bell: func(_ context.Context, _, email, key, _, _, _ string) { f.bells = append(f.bells, email+" "+key) },
	})
	f.svc.Mount(f.mux)
	return f
}

func (f *fixture) ask(t *testing.T, key string) hubproto.TbotJoinState {
	t.Helper()
	raw, _ := json.Marshal(hubproto.TbotJoin{Bot: "agent-anna", Token: "agent-anna-dev-2", Machine: "fleet-dev-2", PublicKey: key})
	return f.svc.Requested(context.Background(), f.m, raw)
}

func (f *fixture) call(t *testing.T, method, path, who string) (int, string) {
	t.Helper()
	r := httptest.NewRequest(method, path, nil)
	if who != "" {
		r.Header.Set("X-Who", who)
	}
	w := httptest.NewRecorder()
	f.mux.ServeHTTP(w, r)
	return w.Code, w.Body.String()
}

func (f *fixture) list(t *testing.T, who string) []View {
	t.Helper()
	code, body := f.call(t, "GET", "/api/me/tbot-joins", who)
	if code != 200 {
		t.Fatalf("list: %d %s", code, body)
	}
	var out struct {
		Requests []View `json:"requests"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatal(err)
	}
	return out.Requests
}

func (f *fixture) heard(t *testing.T) told {
	t.Helper()
	select {
	case got := <-f.reg.ch:
		return got
	case <-time.After(5 * time.Second):
		t.Fatal("the machine was not told")
	}
	return told{}
}

func TestApproveAndRevoke(t *testing.T) {
	f := newFixture(t)
	key := edKey(t)
	if s := f.ask(t, key); s.Status != hubproto.TbotJoinPending {
		t.Fatalf("first ask: %+v", s)
	}
	if s := f.ask(t, key); s.Status != hubproto.TbotJoinPending {
		t.Fatalf("second ask: %+v", s)
	}
	if len(f.bells) != 1 || !strings.HasPrefix(f.bells[0], "anna@example.org ") {
		t.Fatalf("bells %v: one, to the bot's owner", f.bells)
	}
	if got := f.list(t, "boris"); len(got) != 0 {
		t.Fatalf("boris sees %v", got)
	}
	got := f.list(t, "")
	if len(got) != 1 || got[0].State != store.TbotJoinPending || got[0].Token != "agent-anna-dev-2" {
		t.Fatalf("anna sees %+v", got)
	}
	id := got[0].ID
	if code, _ := f.call(t, "POST", "/api/me/tbot-joins/"+id+"/approve", "boris"); code != 404 {
		t.Fatalf("boris approves: %d", code)
	}
	if code, body := f.call(t, "POST", "/api/me/tbot-joins/"+id+"/approve", ""); code != 200 {
		t.Fatalf("approve: %d %s", code, body)
	}
	if k := f.cluster.tokens["agent-anna-dev-2"]; k == "" || !strings.HasPrefix(key, k) {
		t.Fatalf("token key %q, want %q", k, key)
	}
	if h := f.heard(t); h.machine != "m1" || h.state.Status != hubproto.TbotJoinApproved {
		t.Fatalf("told %+v", h)
	}
	if s := f.ask(t, key); s.Status != hubproto.TbotJoinApproved {
		t.Fatalf("ask after approval: %+v", s)
	}
	if code, _ := f.call(t, "POST", "/api/me/tbot-joins/"+id+"/approve", ""); code != 409 {
		t.Fatalf("approve twice: %d", code)
	}
	if code, body := f.call(t, "POST", "/api/me/tbot-joins/"+id+"/revoke", ""); code != 200 {
		t.Fatalf("revoke: %d %s", code, body)
	}
	if _, ok := f.cluster.tokens["agent-anna-dev-2"]; ok {
		t.Fatal("the token is still there after revoke")
	}
	if h := f.heard(t); h.state.Status != hubproto.TbotJoinRevoked {
		t.Fatalf("told %+v", h)
	}
	if s := f.ask(t, key); s.Status != hubproto.TbotJoinRevoked {
		t.Fatalf("the revoked key asks again: %+v", s)
	}
	if got := f.list(t, ""); len(got) != 0 {
		t.Fatalf("a revoked request is still listed: %+v", got)
	}
	// A new key is a new request.
	if s := f.ask(t, edKey(t)); s.Status != hubproto.TbotJoinPending {
		t.Fatalf("a new key: %+v", s)
	}
}

func TestNewKeyReplacesTheApprovedOne(t *testing.T) {
	f := newFixture(t)
	f.ask(t, edKey(t))
	first := f.list(t, "")[0].ID
	if code, _ := f.call(t, "POST", "/api/me/tbot-joins/"+first+"/approve", ""); code != 200 {
		t.Fatal(code)
	}
	f.heard(t)
	f.ask(t, edKey(t))
	var second string
	for _, v := range f.list(t, "") {
		if v.State == store.TbotJoinPending {
			second = v.ID
		}
	}
	if code, _ := f.call(t, "POST", "/api/me/tbot-joins/"+second+"/approve", ""); code != 200 {
		t.Fatal(code)
	}
	states := map[string]bool{}
	for range 2 {
		states[f.heard(t).state.Status] = true
	}
	if !states[hubproto.TbotJoinRevoked] || !states[hubproto.TbotJoinApproved] {
		t.Fatalf("told %v: the old key revoked and the new approved", states)
	}
	if got := f.list(t, ""); len(got) != 1 || got[0].ID != second {
		t.Fatalf("listed %+v", got)
	}
}

func TestRefusals(t *testing.T) {
	f := newFixture(t)
	key := edKey(t)
	raw := func(bot, token string) json.RawMessage {
		b, _ := json.Marshal(hubproto.TbotJoin{Bot: bot, Token: token, PublicKey: key})
		return b
	}
	ctx := context.Background()
	if s := f.svc.Requested(ctx, f.m, raw("agent-boris", "agent-boris-dev-2")); s.Code != "no_bot" {
		t.Fatalf("unknown bot: %+v", s)
	}
	f.cluster.owners["agent-carl"] = []string{"carl@example.org"}
	if s := f.svc.Requested(ctx, f.m, raw("agent-carl", "agent-carl-dev-2")); s.Code != "no_owner" {
		t.Fatalf("an owner with no account: %+v", s)
	}
	f.on["anna@example.org"] = false
	if s := f.svc.Requested(ctx, f.m, raw("agent-anna", "agent-anna-dev-2")); s.Code != "off" {
		t.Fatalf("switch off: %+v", s)
	}
	f.on["anna@example.org"] = true
	other := f.m
	other.ID = "m2"
	f.on["anna@example.org"] = true
	if s := f.svc.Requested(ctx, other, raw("agent-anna", "agent-anna-dev-2")); s.Status != hubproto.TbotJoinRefused {
		t.Fatalf("a machine the owner may not open: %+v", s)
	}
	f.cluster.foreign["agent-anna-dev-2"] = true
	f.ask(t, key)
	id := f.list(t, "")[0].ID
	if code, body := f.call(t, "POST", "/api/me/tbot-joins/"+id+"/approve", ""); code != 409 || !strings.Contains(body, "token_exists") {
		t.Fatalf("a token made by somebody else: %d %s", code, body)
	}
	if code, _ := f.call(t, "POST", "/api/me/tbot-joins/"+id+"/decline", ""); code != 200 {
		t.Fatalf("decline: %d", code)
	}
	if s := f.ask(t, key); s.Status != hubproto.TbotJoinDeclined {
		t.Fatalf("a declined key asks again: %+v", s)
	}
	f.svc.d.On = false
	if s := f.ask(t, key); s.Code != "off" {
		t.Fatalf("hub off: %+v", s)
	}
	if code, _ := f.call(t, "GET", "/api/me/tbot-joins", ""); code != 404 {
		t.Fatalf("list with the hub off: %d", code)
	}
}

func TestKubeTokens(t *testing.T) {
	var mu sync.Mutex
	objects := map[string]map[string]any{
		"agent-anna-old": {"metadata": map[string]any{"name": "agent-anna-old", "labels": map[string]any{ManagedByLabel: "argocd"}}},
	}
	var posted map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer sa-token" {
			w.WriteHeader(401)
			return
		}
		const base = "/apis/resources.teleport.dev/v2/namespaces/tp/teleportprovisiontokens"
		switch {
		case r.URL.Path == "/apis/resources.teleport.dev/v1/namespaces/tp/teleportbotsv1/agent-anna":
			_, _ = io.WriteString(w, `{"spec":{"traits":[{"name":"team","values":["x"]},{"name":"owner","values":["anna@example.org"]}]}}`)
		case r.Method == "POST" && r.URL.Path == base:
			_ = json.NewDecoder(r.Body).Decode(&posted)
			name := posted["metadata"].(map[string]any)["name"].(string)
			objects[name] = posted
			w.WriteHeader(201)
		case strings.HasPrefix(r.URL.Path, base+"/"):
			name := strings.TrimPrefix(r.URL.Path, base+"/")
			o, ok := objects[name]
			if !ok {
				w.WriteHeader(404)
				_, _ = io.WriteString(w, `{"message":"not found"}`)
				return
			}
			if r.Method == "DELETE" {
				delete(objects, name)
				return
			}
			_ = json.NewEncoder(w).Encode(o)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	dir := t.TempDir()
	tokenFile := dir + "/token"
	if err := writeFile(tokenFile, "sa-token\n"); err != nil {
		t.Fatal(err)
	}
	k := &Kube{URL: srv.URL, Namespace: "tp", TokenFile: tokenFile, Client: srv.Client()}
	ctx := context.Background()
	owners, err := k.BotOwners(ctx, "agent-anna")
	if err != nil || len(owners) != 1 || owners[0] != "anna@example.org" {
		t.Fatalf("owners %v %v", owners, err)
	}
	if _, err := k.BotOwners(ctx, "agent-boris"); err != ErrNoBot {
		t.Fatalf("unknown bot: %v", err)
	}
	if err := k.PutToken(ctx, "agent-anna-old", "agent-anna", "ssh-ed25519 AAAA"); err != ErrNotOurs {
		t.Fatalf("a token of git: %v", err)
	}
	if err := k.DeleteToken(ctx, "agent-anna-old"); err != ErrNotOurs {
		t.Fatalf("delete a token of git: %v", err)
	}
	if err := k.PutToken(ctx, "agent-anna-dev-2", "agent-anna", "ssh-ed25519 AAAA"); err != nil {
		t.Fatal(err)
	}
	spec := posted["spec"].(map[string]any)
	meta := posted["metadata"].(map[string]any)
	if meta["labels"].(map[string]any)[ManagedByLabel] != ManagedBy || meta["namespace"] != "tp" {
		t.Fatalf("metadata %v", meta)
	}
	if spec["join_method"] != "bound_keypair" || spec["bot_name"] != "agent-anna" || len(spec["roles"].([]any)) != 1 || spec["roles"].([]any)[0] != "Bot" {
		t.Fatalf("spec %v", spec)
	}
	bk := spec["bound_keypair"].(map[string]any)
	if bk["onboarding"].(map[string]any)["initial_public_key"] != "ssh-ed25519 AAAA" || bk["recovery"].(map[string]any)["mode"] != "standard" {
		t.Fatalf("bound_keypair %v", bk)
	}
	// Again: the hub's own token is replaced.
	if err := k.PutToken(ctx, "agent-anna-dev-2", "agent-anna", "ssh-ed25519 BBBB"); err != nil {
		t.Fatal(err)
	}
	if err := k.DeleteToken(ctx, "agent-anna-dev-2"); err != nil {
		t.Fatal(err)
	}
	if err := k.DeleteToken(ctx, "agent-anna-dev-2"); err != nil {
		t.Fatalf("delete what is gone: %v", err)
	}
}

func writeFile(path, s string) error { return os.WriteFile(path, []byte(s), 0o600) }
