package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/api"
	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
	"github.com/ly-solutions/harnessly/internal/secrets"
	"github.com/ly-solutions/harnessly/internal/secrets/secretstest"
)

// hubMachine is the machine.json of an agent hubtest registered.
func hubMachine(s *hubtest.Stack, m *hubtest.Agent) *MachineFile {
	return &MachineFile{
		MachineID: m.MachineID, HubURL: s.URL, DialURL: "ws" + strings.TrimPrefix(s.URL, "http") + "/agent/v1",
		Ed25519: base64.StdEncoding.EncodeToString(m.Priv.Seed()), X25519: base64.StdEncoding.EncodeToString(m.Enc.Bytes()),
	}
}

// runtimeSafe is the safe the runtime of a machine builds in hub mode, over a loopback agent that dials the hub as mf.
func runtimeSafe(t *testing.T, mf *MachineFile) *secrets.Remote {
	t.Helper()
	return runtimeSafeOf(t, Config{Machine: mf})
}

// runtimeSafeOf is runtimeSafe over an agent with this configuration.
func runtimeSafeOf(t *testing.T, cfg Config) *secrets.Remote {
	t.Helper()
	cfg.LocalTokenHeader = "x-agent-local-token"
	a := &Agent{cfg: cfg, localToken: "local"}
	srv := httptest.NewServer(http.HandlerFunc(a.handler))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	data := t.TempDir()
	info, _ := json.Marshal(map[string]string{"port": u.Port(), "token": "local"})
	if err := os.WriteFile(filepath.Join(data, "agent.json"), info, 0o600); err != nil {
		t.Fatal(err)
	}
	r := secrets.NewRemote(LocalSafe{DataDir: data}, SafeOwnFolder, t.TempDir())
	r.RefPrefixes = []string{"*"}
	return r
}

func hubWithSafe(t *testing.T, kv secrets.KV, flags string) *hubtest.Stack {
	return hubtest.New(t, hubtest.Env("HUB_FEATURES_FORCE", flags), hubtest.WithOptions(func(o *api.Options) { o.SafeKV = kv }))
}

func TestHubSafeEndToEnd(t *testing.T) {
	kv := secretstest.New()
	kv.Data["shared/ci/deploy"] = map[string]string{"key": "ci-key"}
	kv.Data["other/x"] = map[string]string{"key": "no"}
	s := hubWithSafe(t, kv, "secrets.vault")
	s.Hub.Safe.RefPrefixes = []string{"shared"}

	alice := s.SignIn("alice@example.com")
	aid, _ := alice.NewSpace("Acme")
	am := alice.Machine(aid, nil)
	bob := s.SignIn("bob@example.com")
	bid, _ := bob.NewSpace("Beta")
	bm := bob.Machine(bid, nil)

	safe := runtimeSafe(t, hubMachine(s, am))
	if _, created, err := safe.Put("gh-token", "alice-value"); err != nil || !created {
		t.Fatalf("put: %v %v", created, err)
	}
	// The value lies in the owner's folder the hub chose, under the owner's id, not where the machine asked.
	var owner string
	for p := range kv.Data {
		if rest, ok := strings.CutPrefix(p, "harnessly/users/"); ok && strings.HasSuffix(rest, "/gh-token") {
			owner = strings.TrimSuffix(rest, "/gh-token")
		}
	}
	if owner == "" || kv.Data["harnessly/users/"+owner+"/gh-token"]["value"] != "alice-value" {
		t.Fatalf("stored at %v", kv.Data)
	}
	views, err := safe.List()
	if err != nil || len(views) != 1 || views[0].Name != "gh-token" {
		t.Fatalf("list: %+v %v", views, err)
	}
	values, err := safe.Reveal([]string{"gh-token"}, secrets.Use{})
	if err != nil || values["gh-token"] != "alice-value" {
		t.Fatalf("reveal: %v", err)
	}
	big := strings.Repeat("b", secrets.MaxValue)
	if _, _, err := safe.Put("big", big); err != nil {
		t.Fatalf("a value of the largest size: %v", err)
	}
	if values, err := safe.Reveal([]string{"big"}, secrets.Use{}); err != nil || values["big"] != big {
		t.Fatalf("reveal big: %v", err)
	}
	if v, err := safe.ReadRef("shared/ci/deploy", "key"); err != nil || v != "ci-key" {
		t.Fatalf("ref: %q %v", v, err)
	}
	for _, p := range []string{"other/x", "harnessly/users/" + owner + "/gh-token"} {
		if _, err := safe.ReadRef(p, "value"); secrets.ReasonOf(err) != secrets.ReasonDenied {
			t.Fatalf("ref %s: %v", p, err)
		}
	}

	// Bob's machine sees a folder of its own: Alice's secret is not there.
	bobs := runtimeSafe(t, hubMachine(s, bm))
	if views, err := bobs.List(); err != nil || len(views) != 0 {
		t.Fatalf("bob's list: %+v %v", views, err)
	}
	if _, err := bobs.Reveal([]string{"gh-token"}, secrets.Use{}); err == nil {
		t.Fatal("bob read alice's secret")
	}

	if err := safe.Remove("gh-token"); err != nil {
		t.Fatal(err)
	}
	if _, ok := kv.Data["harnessly/users/"+owner+"/gh-token"]; ok {
		t.Fatal("not removed")
	}

	// The journal says who did what to which secret, never a value.
	events, err := s.DB.S().Audit(context.Background(), store.AuditFilter{SpaceID: aid, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, e := range events {
		if strings.Contains(e.After, "alice-value") || strings.Contains(e.Before, "alice-value") {
			t.Fatalf("a value in the journal: %+v", e)
		}
		if e.TargetKind == "secret" {
			seen[e.Action+":"+e.TargetID] = true
		}
	}
	for _, want := range []string{"secret.write:gh-token", "secret.read:gh-token", "secret.ref:shared/ci/deploy", "secret.delete:gh-token"} {
		if !seen[want] {
			t.Errorf("journal lacks %s: %v", want, seen)
		}
	}
	if strings.Contains(s.Logs.String(), "alice-value") {
		t.Fatal("a value in the hub's log")
	}
}

func TestHubSafeRefusesWithoutFlagOrProof(t *testing.T) {
	kv := secretstest.New()
	s := hubWithSafe(t, kv, "")
	owner := s.SignIn("owner@example.com")
	id, _ := owner.NewSpace("Acme")
	m := owner.Machine(id, nil)
	mf := hubMachine(s, m)
	ctx := context.Background()

	// The flag is off: the safe is not there, and nothing is written.
	if _, err := RequestSafe(ctx, mf, nil, s.Clock.Now(), hubproto.SecretsRequest{Op: hubproto.SecretsWrite, Name: "n", Data: map[string]string{"value": "v"}}); secrets.ReasonOf(err) != secrets.ReasonUnavailable {
		t.Fatalf("flag off: %v", err)
	}
	if len(kv.Data) != 0 {
		t.Fatalf("written with the flag off: %v", kv.Data)
	}

	s2 := hubWithSafe(t, kv, "secrets.vault")
	o2 := s2.SignIn("owner@example.com")
	id2, _ := o2.NewSpace("Acme")
	m2 := o2.Machine(id2, nil)
	host, _ := hubproto.DialHost(s2.Cfg.AgentURL)
	post := func(auth string, body []byte) int {
		req, _ := http.NewRequest(http.MethodPost, s2.URL+hubproto.SecretsPath, strings.NewReader(string(body)))
		req.Header.Set("Authorization", auth)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	body := []byte(`{"op":"list"}`)
	dial, _ := hubproto.SignDial(m2.Priv, m2.MachineID, s2.Clock.Now(), host)
	if code := post(dial, body); code != http.StatusUnauthorized {
		t.Fatalf("a dial signature: %d", code)
	}
	auth, _ := hubproto.SignSecrets(m2.Priv, m2.MachineID, s2.Clock.Now(), host, body)
	if code := post(auth, []byte(`{"op":"read","name":"gh-token"}`)); code != http.StatusUnauthorized {
		t.Fatalf("a signature over another body: %d", code)
	}
	auth, _ = hubproto.SignSecrets(m2.Priv, m2.MachineID, s2.Clock.Now(), host, body)
	if code := post(auth, body); code != http.StatusOK {
		t.Fatalf("good: %d", code)
	}
	if code := post(auth, body); code != http.StatusUnauthorized {
		t.Fatalf("replayed: %d", code)
	}
	// Another machine's key does not speak for this one.
	auth, _ = hubproto.SignSecrets(m.Priv, m2.MachineID, s2.Clock.Now(), host, body)
	if code := post(auth, body); code != http.StatusUnauthorized {
		t.Fatalf("a foreign key: %d", code)
	}
}

// A closed cluster's machine whose hub only observes it keeps its safe at home, unless its operator opts in.
func TestHubSafeOnObserveLinkOnlyWhenOptedIn(t *testing.T) {
	kv := secretstest.New()
	s := hubWithSafe(t, kv, "secrets.vault")
	alice := s.SignIn("alice@example.com")
	aid, _ := alice.NewSpace("Acme")
	mf := hubMachine(s, alice.Machine(aid, nil))

	closed := runtimeSafeOf(t, Config{ObserveMachine: mf})
	if _, _, err := closed.Put("gh-token", "v"); secrets.ReasonOf(err) != secrets.ReasonUnavailable {
		t.Fatalf("an observe link without AGENT_HUB_SAFE=on: %v", err)
	}
	if len(kv.Data) != 0 {
		t.Fatalf("written without the opt-in: %v", kv.Data)
	}
	opted := runtimeSafeOf(t, Config{ObserveMachine: mf, ObserveSafe: true})
	if _, created, err := opted.Put("gh-token", "observed-value"); err != nil || !created {
		t.Fatalf("put over the observe link: %v %v", created, err)
	}
	if v, err := opted.Reveal([]string{"gh-token"}, secrets.Use{}); err != nil || v["gh-token"] != "observed-value" {
		t.Fatalf("reveal over the observe link: %q %v", v, err)
	}
}
