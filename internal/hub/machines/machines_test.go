package machines_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

type fixture struct {
	s       *hubtest.Stack
	owner   *hubtest.Person
	spaceID string
}

func setup(t *testing.T, opts ...hubtest.Option) *fixture {
	t.Helper()
	s := hubtest.New(t, opts...)
	owner := s.SignIn("owner@example.com")
	id, _ := owner.NewSpace("Команда")
	return &fixture{s: s, owner: owner, spaceID: id}
}

func (f *fixture) codes(body map[string]any) hubtest.Resp {
	return f.owner.Do("POST", "/api/spaces/"+f.spaceID+"/machines/codes", body)
}

var codeRe = regexp.MustCompile(`^hx_[a-z2-7]{24}$`)

func TestCreateCode(t *testing.T) {
	f := setup(t)
	r := f.codes(map[string]any{"profile": "dir"})
	if r.Code != http.StatusCreated {
		t.Fatal(r)
	}
	var out struct {
		Code      string
		ExpiresAt string
		Profile   string
		Command   string
	}
	r.JSON(&out)
	if !codeRe.MatchString(out.Code) {
		t.Errorf("code = %q", out.Code)
	}
	if want := "curl -fsSL " + f.s.URL + "/install | sudo sh -s -- --token " + out.Code + " --profile dir"; out.Command != want {
		t.Errorf("command = %q\nwant      %q", out.Command, want)
	}
	if want := f.s.Clock.Now().Add(30 * time.Minute).UTC().Format(time.RFC3339); out.ExpiresAt != want || out.Profile != "dir" {
		t.Errorf("expiresAt = %q profile = %q, want %q", out.ExpiresAt, out.Profile, want)
	}

	// Only the hash is kept.
	var n int
	if err := f.s.DB.QueryRow(context.Background(), `SELECT COUNT(*) FROM registration_codes WHERE code_hash = ?`, out.Code).Scan(&n); err != nil || n != 0 {
		t.Errorf("the clear code is in the database: %d %v", n, err)
	}
	if _, err := f.s.DB.S().RegCodeByHash(context.Background(), store.HashToken(out.Code)); err != nil {
		t.Errorf("the hashed code is not there: %v", err)
	}
}

func TestCreateCodeProfiles(t *testing.T) {
	f := setup(t)
	cases := []struct {
		name string
		body map[string]any
		code int
		err  string
	}{
		{"full needs the confirmation", map[string]any{"profile": "full"}, 400, "invalid_request"},
		{"full confirmed", map[string]any{"profile": "full", "confirmFull": true}, 201, ""},
		{"container needs an image", map[string]any{"profile": "container"}, 422, "provider_off"},
		{"unknown", map[string]any{"profile": "vm"}, 400, "invalid_request"},
		{"missing", map[string]any{}, 400, "invalid_request"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := f.codes(c.body)
			if r.Code != c.code || r.Err() != c.err {
				t.Errorf("%s", r)
			}
		})
	}
	if r := f.codes(map[string]any{"profile": "full"}); r.Detail("field") != "confirmFull" {
		t.Errorf("field = %v", r.Detail("field"))
	}

	g := setup(t, hubtest.Env("HUB_CONTAINER_IMAGE", "ghcr.io/example/machine:1"))
	if r := g.codes(map[string]any{"profile": "container"}); r.Code != 201 {
		t.Errorf("container with an image: %s", r)
	}
}

func TestCreateCodeIsForOwners(t *testing.T) {
	f := setup(t)
	member := f.s.SignIn("member@example.com")
	f.owner.Join(f.spaceID, member)
	stranger := f.s.SignIn("stranger@example.com")
	path := "/api/spaces/" + f.spaceID + "/machines/codes"
	body := map[string]any{"profile": "dir"}
	if r := member.Do("POST", path, body); r.Code != 403 {
		t.Errorf("member: %s", r)
	}
	if r := stranger.Do("POST", path, body); r.Code != 403 {
		t.Errorf("stranger: %s", r)
	}
	if r := f.s.Browser().Do("POST", path, body); r.Code != 401 {
		t.Errorf("anonymous: %s", r)
	}
}

func TestUnusedCodesAreCapped(t *testing.T) {
	f := setup(t)
	for i := 0; i < 20; i++ {
		if r := f.codes(map[string]any{"profile": "dir"}); r.Code != 201 {
			t.Fatalf("code %d: %s", i, r)
		}
	}
	r := f.codes(map[string]any{"profile": "dir"})
	if r.Code != http.StatusConflict || r.Detail("reason") != "code_limit" {
		t.Errorf("21st code: %s", r)
	}
	f.s.Clock.Advance(31 * time.Minute)
	if r := f.codes(map[string]any{"profile": "dir"}); r.Code != 201 {
		t.Errorf("after the others expired: %s", r)
	}
}

func TestRegister(t *testing.T) {
	f := setup(t)
	stream := f.owner.Events(f.spaceID)
	code := f.owner.NewCode(f.spaceID, "dir")
	a := f.s.NewAgent(nil)
	r := a.Register(code, hubproto.ProfileDir, hubproto.ViaCommand)
	if r.Code != http.StatusCreated {
		t.Fatal(r)
	}
	var res hubproto.RegisterResponse
	r.JSON(&res)
	if len(res.MachineID) != 20 || res.RuntimeBasePath != "/m/"+res.MachineID || res.HubURL != f.s.URL {
		t.Errorf("response = %+v", res)
	}
	if want := "ws" + strings.TrimPrefix(f.s.URL, "http") + "/agent/v1"; res.DialURL != want {
		t.Errorf("dialUrl = %q, want %q", res.DialURL, want)
	}
	if res.SpaceName == nil || *res.SpaceName != "Команда" {
		t.Errorf("spaceName = %v", res.SpaceName)
	}

	// A machine that never connected is pending, and the space hears about it.
	var got struct{ Machine map[string]any }
	f.owner.Do("GET", "/api/machines/"+res.MachineID, nil).JSON(&got)
	m := got.Machine
	if m["status"] != "pending" || m["profile"] != "dir" || m["via"] != "command" || m["spaceId"] != f.spaceID || m["connectedAt"] != nil || m["lastSeenAt"] != nil {
		t.Errorf("machine = %v", m)
	}
	if want := base64.StdEncoding.EncodeToString(a.Enc.PublicKey().Bytes()); m["encKey"] != want {
		t.Errorf("encKey = %v", m["encKey"])
	}
	ev := stream.Await("the machine event", func(e hubtest.Event) bool { return e.Name == "machine" })
	if !strings.Contains(string(ev.Data), res.MachineID) {
		t.Errorf("event = %s", ev.Data)
	}

	// The code works once.
	b := f.s.NewAgent(nil)
	r2 := b.Register(code, hubproto.ProfileDir, hubproto.ViaCommand)
	if r2.Code != http.StatusGone || r2.Err() != "expired" {
		t.Errorf("second use: %s", r2)
	}
}

func TestRegisterTakesTheCodesProfile(t *testing.T) {
	f := setup(t)
	r := f.owner.Do("POST", "/api/spaces/"+f.spaceID+"/machines/codes", map[string]any{"profile": "full", "confirmFull": true})
	var out struct{ Code string }
	r.JSON(&out)
	a := f.s.NewAgent(nil)
	// The agent says dir; the owner chose full in the install command, and that is what counts.
	if r := a.Register(out.Code, hubproto.ProfileDir, hubproto.ViaCommand); r.Code != 201 {
		t.Fatal(r)
	}
	m, err := f.s.DB.S().MachineByID(context.Background(), a.MachineID)
	if err != nil || m.Profile != "full" {
		t.Errorf("machine = %+v %v", m, err)
	}
}

func TestRegisterCodeExpires(t *testing.T) {
	f := setup(t)
	code := f.owner.NewCode(f.spaceID, "dir")
	f.s.Clock.Advance(30*time.Minute + time.Second)
	a := f.s.NewAgent(nil)
	if r := a.Register(code, hubproto.ProfileDir, hubproto.ViaCommand); r.Code != 410 {
		t.Errorf("an expired code: %s", r)
	}
	for _, bad := range []string{"", "hx_", "hx_aaaaaaaaaaaaaaaaaaaaaaaa", "not a code"} {
		if r := a.Register(bad, hubproto.ProfileDir, hubproto.ViaCommand); r.Code != 410 {
			t.Errorf("code %q: %s", bad, r)
		}
	}
}

func TestRegisterValidation(t *testing.T) {
	f := setup(t)
	good := func(code string, a *hubtest.Agent) hubproto.RegisterRequest {
		return a.RegisterRequest(code, hubproto.ProfileDir, hubproto.ViaCommand)
	}
	cases := []struct {
		name  string
		field string
		edit  func(*hubproto.RegisterRequest)
	}{
		{"short signing key", "publicKey", func(r *hubproto.RegisterRequest) { r.PublicKey = base64.StdEncoding.EncodeToString([]byte("short")) }},
		{"signing key not base64", "publicKey", func(r *hubproto.RegisterRequest) { r.PublicKey = "%%%" }},
		{"short encryption key", "encKey", func(r *hubproto.RegisterRequest) { r.EncKey = base64.StdEncoding.EncodeToString([]byte("short")) }},
		{"os", "os", func(r *hubproto.RegisterRequest) { r.OS = "windows" }},
		{"arch", "arch", func(r *hubproto.RegisterRequest) { r.Arch = "mips" }},
		{"via", "via", func(r *hubproto.RegisterRequest) { r.Via = "carrier-pigeon" }},
		{"hostname with a control character", "hostname", func(r *hubproto.RegisterRequest) { r.Hostname = "dev\nbox" }},
		{"hostname too long", "hostname", func(r *hubproto.RegisterRequest) { r.Hostname = strings.Repeat("a", 256) }},
		{"unix user too long", "unixUser", func(r *hubproto.RegisterRequest) { r.UnixUser = strings.Repeat("a", 65) }},
		{"version", "agentVersion", func(r *hubproto.RegisterRequest) { r.AgentVersion = strings.Repeat("1", 65) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code := f.owner.NewCode(f.spaceID, "dir")
			a := f.s.NewAgent(nil)
			req := good(code, a)
			c.edit(&req)
			r := f.s.Browser().Do("POST", "/agent/register", req)
			if r.Code != 400 || r.Detail("field") != c.field {
				t.Fatalf("%s", r)
			}
			// A refused request leaves the code usable.
			if r := a.Register(code, hubproto.ProfileDir, hubproto.ViaCommand); r.Code != 201 {
				t.Errorf("the code was spent by a bad request: %s", r)
			}
		})
	}
	if r := f.s.Browser().Do("POST", "/agent/register", `{"code":`); r.Code != 400 {
		t.Errorf("broken JSON: %s", r)
	}
}

func TestOneKeyOneMachine(t *testing.T) {
	f := setup(t)
	a := f.s.NewAgent(nil)
	if r := a.Register(f.owner.NewCode(f.spaceID, "dir"), hubproto.ProfileDir, hubproto.ViaCommand); r.Code != 201 {
		t.Fatal(r)
	}
	second := f.owner.NewCode(f.spaceID, "dir")
	if r := a.Register(second, hubproto.ProfileDir, hubproto.ViaCommand); r.Code != http.StatusConflict {
		t.Errorf("the same key twice: %s", r)
	}
}

func TestLocalViaOnASpaceCodeIsACommand(t *testing.T) {
	f := setup(t)
	a := f.s.NewAgent(nil)
	if r := a.Register(f.owner.NewCode(f.spaceID, "dir"), hubproto.ProfileDir, hubproto.ViaLocal); r.Code != 201 {
		t.Fatal(r)
	}
	m, _ := f.s.DB.S().MachineByID(context.Background(), a.MachineID)
	if m.Via != "command" {
		t.Errorf("via = %q: only the hub's own local code makes a local machine", m.Via)
	}
}

func TestRegistrationsAreLimitedPerAddress(t *testing.T) {
	f := setup(t)
	a := f.s.NewAgent(nil)
	var last hubtest.Resp
	for i := 0; i < 40; i++ {
		last = a.Register(fmt.Sprintf("hx_%024d", i), hubproto.ProfileDir, hubproto.ViaCommand)
		if last.Code == http.StatusTooManyRequests {
			if i != 30 {
				t.Errorf("limited at try %d, want after 30", i)
			}
			if last.Header.Get("Retry-After") == "" || last.Err() != "rate_limited" {
				t.Errorf("answer = %s, headers %v", last, last.Header)
			}
			return
		}
	}
	t.Fatalf("never limited: %s", last)
}

func (f *fixture) view(t *testing.T, id string) map[string]any {
	t.Helper()
	var got struct{ Machine map[string]any }
	if r := f.owner.Do("GET", "/api/machines/"+id, nil); r.Code != 200 {
		t.Fatal(r)
	} else {
		r.JSON(&got)
	}
	return got.Machine
}

func TestConnectedMachineAsTheAPISendsIt(t *testing.T) {
	f := setup(t)
	stream := f.owner.Events(f.spaceID)
	a := f.owner.Machine(f.spaceID, nil)
	m := f.view(t, a.MachineID)

	if m["status"] != "online" || m["connectedAt"] == nil || m["lastSeenAt"] == nil {
		t.Errorf("machine = %v", m)
	}
	if m["hostname"] != "dev-box" || m["unixUser"] != "dev" || m["agentVersion"] != "1.0.0" || m["os"] != "linux" || m["arch"] != "amd64" {
		t.Errorf("hello was not applied: %v", m)
	}
	if env, ok := m["env"].([]any); !ok || len(env) != 0 {
		t.Errorf("env = %v, want []", m["env"])
	}
	policy, _ := m["policy"].(map[string]any)
	if policy["terminal"] != true || policy["source"] != "default" {
		t.Errorf("policy = %v", m["policy"])
	}
	pre, _ := m["preflight"].(map[string]any)
	if pre["osName"] != "Ubuntu 24.04" {
		t.Errorf("preflight = %v", m["preflight"])
	}
	if _, isString := pre["at"].(string); !isString {
		t.Errorf("preflight.at = %v: times are RFC 3339 strings", pre["at"])
	}
	proxy, _ := m["proxy"].(map[string]any)
	if len(proxy) != 3 || proxy["all"] != nil {
		t.Errorf("proxy = %v", m["proxy"])
	}

	agents, _ := m["agents"].([]any)
	if len(agents) != 3 {
		t.Fatalf("agents = %v", m["agents"])
	}
	byName := map[string]map[string]any{}
	for _, raw := range agents {
		ag := raw.(map[string]any)
		byName[ag["provider"].(string)] = ag
	}
	if byName["claude"]["installed"] != true || byName["claude"]["login"] != "logged_in" || byName["claude"]["authMode"] != "subscription" {
		t.Errorf("claude = %v", byName["claude"])
	}
	if byName["codex"]["installed"] != false || byName["codex"]["login"] != "logged_out" {
		t.Errorf("codex = %v", byName["codex"])
	}
	if byName["gemini"]["soon"] != true || byName["gemini"]["installed"] != false {
		t.Errorf("gemini = %v", byName["gemini"])
	}
	if _, ok := byName["claude"]["soon"]; ok {
		t.Error("only gemini says soon")
	}

	// The hub asked for what it needs, and subscribed to the events it wants and no others.
	var ops []string
	for _, c := range a.Calls("") {
		ops = append(ops, c.Op)
	}
	if got := strings.Join(ops, ","); !strings.HasPrefix(got, "hello,link.subscribe,preflight.run,agents.status,proxy.get") {
		t.Errorf("connect routine = %s", got)
	}
	sub := a.Calls("link.subscribe")[0]
	var events []string
	_ = json.Unmarshal(sub.Params["events"], &events)
	if strings.Join(events, ",") != "job,login,agents,preflight,policy" {
		t.Errorf("subscribed events = %v", events)
	}

	// Going away is a state change too.
	a.Close()
	stream.Await("offline", func(e hubtest.Event) bool {
		return e.Name == "machine" && strings.Contains(string(e.Data), `"status":"offline"`)
	})
	if m := f.view(t, a.MachineID); m["status"] != "offline" || m["connectedAt"] != nil || m["lastSeenAt"] == nil {
		t.Errorf("offline machine = %v", m)
	}
}

func TestAgentWithAnotherMachineIDIsDropped(t *testing.T) {
	f := setup(t)
	a := f.s.NewAgent(nil)
	if r := a.Register(f.owner.NewCode(f.spaceID, "dir"), hubproto.ProfileDir, hubproto.ViaCommand); r.Code != 201 {
		t.Fatal(r)
	}
	a.Hello.MachineID = "aaaaaaaaaaaaaaaaaaaa"
	if code, body := a.TryConnect(); code != 101 {
		t.Fatalf("dial: %d %s", code, body)
	}
	a.WaitClosed()
	if m := f.view(t, a.MachineID); m["status"] == "online" {
		t.Errorf("a machine that lied about its id is online: %v", m)
	}
}

func TestAgentWithoutHelloIsDropped(t *testing.T) {
	f := setup(t)
	a := f.s.NewAgent(nil)
	a.Handle("hello", func(hubtest.Call) (any, error) { return nil, hubtest.Fail("failed", "no") })
	if r := a.Register(f.owner.NewCode(f.spaceID, "dir"), hubproto.ProfileDir, hubproto.ViaCommand); r.Code != 201 {
		t.Fatal(r)
	}
	if code, body := a.TryConnect(); code != 101 {
		t.Fatalf("dial: %d %s", code, body)
	}
	a.WaitClosed()
}

func TestEventsFromTheAgent(t *testing.T) {
	f := setup(t)
	member := f.s.SignIn("member@example.com")
	f.owner.Join(f.spaceID, member)
	a := f.owner.Machine(f.spaceID, nil)
	ownerStream := f.owner.Events(f.spaceID)
	memberStream := member.Events(f.spaceID)

	t.Run("preflight", func(t *testing.T) {
		report := hubtest.Preflight(f.s.Clock.Now())
		report.OSName = "Debian 12"
		a.Emit(map[string]any{"event": "preflight", "report": report})
		f.s.Eventually("the new report", func() bool {
			pre, _ := f.view(t, a.MachineID)["preflight"].(map[string]any)
			return pre["osName"] == "Debian 12"
		})
	})

	t.Run("policy", func(t *testing.T) {
		a.Emit(map[string]any{"event": "policy", "policy": hubproto.Policy{Terminal: false, Keys: true, DenyRoutes: []string{"/api/fs/write"}, Source: "/etc/harnessly/policy.toml"}})
		f.s.Eventually("the new policy", func() bool {
			pol, _ := f.view(t, a.MachineID)["policy"].(map[string]any)
			return pol["terminal"] == false && pol["source"] == "/etc/harnessly/policy.toml"
		})
	})

	t.Run("agents", func(t *testing.T) {
		v := "0.9.1"
		mode := hubproto.AuthAPIKey
		over := "ANTHROPIC_API_KEY"
		a.Emit(map[string]any{"event": "agents", "agents": []hubproto.AgentStatus{
			{Provider: "codex", Installed: true, Version: &v, Login: hubproto.LoggedIn, AuthMode: &mode, OverriddenBy: &over,
				Access: &hubproto.ProviderCheck{Provider: "codex", Verdict: "ok", At: f.s.Clock.Now().UnixMilli(), Via: "direct", Host: "api.openai.com"}},
			{Provider: "gemini", Installed: true, Login: hubproto.LoggedIn},
			{Provider: "somethingelse", Installed: true},
		}})
		f.s.Eventually("codex to be installed", func() bool {
			for _, raw := range f.view(t, a.MachineID)["agents"].([]any) {
				ag := raw.(map[string]any)
				if ag["provider"] == "codex" {
					access, _ := ag["access"].(map[string]any)
					_, atIsString := access["at"].(string)
					return ag["installed"] == true && ag["authMode"] == "api_key" && ag["overriddenBy"] == "ANTHROPIC_API_KEY" && atIsString
				}
			}
			return false
		})
		agents := f.view(t, a.MachineID)["agents"].([]any)
		if len(agents) != 3 {
			t.Fatalf("agents = %v: still claude, codex and gemini only", agents)
		}
		// Claude, which this event did not mention, keeps its last status; gemini stays a card that says soon.
		first, last := agents[0].(map[string]any), agents[2].(map[string]any)
		if first["provider"] != "claude" || first["installed"] != true || last["provider"] != "gemini" || last["installed"] != false || last["soon"] != true {
			t.Errorf("agents = %v", agents)
		}
		rows, err := f.s.DB.S().AgentConnections(context.Background(), a.MachineID)
		if err != nil {
			t.Fatal(err)
		}
		var providers []string
		for _, r := range rows {
			providers = append(providers, r.Provider+":"+r.LoginState+":"+r.ProviderVerdict)
		}
		if got := strings.Join(providers, " "); got != "claude:logged_in: codex:logged_in:ok" {
			t.Errorf("agent_connections = %q", got)
		}
	})

	t.Run("job reaches owners and not members", func(t *testing.T) {
		job := hubproto.Job{ID: strings.Repeat("a", 32), Kind: hubproto.JobAgentInstall, State: hubproto.JobRunning, Steps: []hubproto.JobStep{{ID: "system", State: hubproto.StepOK}}}
		a.Emit(map[string]any{"event": "job", "job": job})
		ev := ownerStream.Await("the job", func(e hubtest.Event) bool { return e.Name == "job" })
		var d struct {
			MachineID string
			Job       hubproto.Job
		}
		if err := json.Unmarshal(ev.Data, &d); err != nil || d.MachineID != a.MachineID || d.Job.ID != job.ID || d.Job.Steps[0].State != "ok" {
			t.Errorf("event = %s (%v)", ev.Data, err)
		}
		// A member's stream was open the whole time: whatever else it got, it got no job.
		f.s.Eventually("the machine events", func() bool { return len(memberStream.Named("machine")) > 0 })
		if got := memberStream.Named("job"); len(got) != 0 {
			t.Errorf("a member received %d job events", len(got))
		}
	})

	t.Run("login with a finished status updates the agent", func(t *testing.T) {
		url := "https://claude.ai/oauth/x"
		exp := f.s.Clock.Now().Add(10 * time.Minute).UnixMilli()
		login := hubproto.Login{ID: strings.Repeat("b", 32), Provider: "claude", Method: "link", State: hubproto.LoginURL, URL: &url, NeedsCode: true, ExpiresAt: &exp}
		a.Emit(map[string]any{"event": "login", "login": login})
		ev := ownerStream.Await("the login", func(e hubtest.Event) bool { return e.Name == "login" })
		var d struct {
			MachineID string
			Login     map[string]any
		}
		_ = json.Unmarshal(ev.Data, &d)
		if d.MachineID != a.MachineID || d.Login["url"] != url || d.Login["needsCode"] != true {
			t.Errorf("event = %s", ev.Data)
		}
		if _, ok := d.Login["expiresAt"].(string); !ok {
			t.Errorf("expiresAt = %v: times are RFC 3339 strings", d.Login["expiresAt"])
		}
		if got := memberStream.Named("login"); len(got) != 0 {
			t.Errorf("a member received %d login events", len(got))
		}
	})

	t.Run("a login link that is not the provider's page does not reach the owners", func(t *testing.T) {
		evil := "https://evil.example.net/oauth/authorize"
		console := "https://evil.example.net/m/" + a.MachineID + "/console/0123456789abcdef"
		login := hubproto.Login{ID: strings.Repeat("c", 32), Provider: "claude", Method: "link", State: hubproto.LoginURL, URL: &evil, ConsoleURL: &console, NeedsCode: true}
		a.Emit(map[string]any{"event": "login", "login": login})
		ev := ownerStream.Await("the cleaned login", func(e hubtest.Event) bool {
			return e.Name == "login" && strings.Contains(string(e.Data), strings.Repeat("c", 32))
		})
		var d struct{ Login map[string]any }
		_ = json.Unmarshal(ev.Data, &d)
		if d.Login["url"] != nil || d.Login["consoleUrl"] != nil || d.Login["state"] != "failed" || strings.Contains(string(ev.Data), "evil.example.net") {
			t.Errorf("event = %s", ev.Data)
		}
	})

	t.Run("nonsense is ignored", func(t *testing.T) {
		a.Emit(map[string]any{"event": "job", "job": map[string]any{"id": ""}})
		a.Emit(map[string]any{"event": "preflight", "report": "not a report"})
		a.Emit(map[string]any{"event": "transcripts.changed", "x": 1})
		if m := f.view(t, a.MachineID); m["status"] != "online" {
			t.Errorf("machine = %v", m)
		}
	})
}

func TestRename(t *testing.T) {
	f := setup(t)
	a := f.owner.Machine(f.spaceID, nil)
	member := f.s.SignIn("member@example.com")
	f.owner.Join(f.spaceID, member)
	path := "/api/machines/" + a.MachineID

	cases := []struct {
		name string
		body map[string]any
		code int
		want string
	}{
		{"renames", map[string]any{"name": "  Сервер   сборки "}, 200, "Сервер сборки"},
		{"empty", map[string]any{"name": "  "}, 400, ""},
		{"too long", map[string]any{"name": strings.Repeat("я", 61)}, 400, ""},
		{"control character", map[string]any{"name": "a\x07b"}, 400, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := f.owner.Do("PATCH", path, c.body)
			if r.Code != c.code {
				t.Fatal(r)
			}
			if c.want != "" {
				var out struct{ Machine struct{ Name string } }
				r.JSON(&out)
				if out.Machine.Name != c.want {
					t.Errorf("name = %q", out.Machine.Name)
				}
			}
		})
	}
	if r := member.Do("PATCH", path, map[string]any{"name": "mine"}); r.Code != 403 {
		t.Errorf("member: %s", r)
	}
	if got := f.view(t, a.MachineID)["name"]; got != "Сервер сборки" {
		t.Errorf("name = %v", got)
	}
}

func TestMembersSeeMachinesStrangersDoNot(t *testing.T) {
	f := setup(t)
	a := f.owner.Machine(f.spaceID, nil)
	member := f.s.SignIn("member@example.com")
	f.owner.Join(f.spaceID, member)
	stranger := f.s.SignIn("stranger@example.com")

	if r := member.Do("GET", "/api/machines/"+a.MachineID, nil); r.Code != 200 {
		t.Errorf("member get: %s", r)
	}
	if r := member.Do("GET", "/api/spaces/"+f.spaceID+"/machines", nil); r.Code != 200 || !strings.Contains(string(r.Body), a.MachineID) {
		t.Errorf("member list: %s", r)
	}
	// A machine that is not yours and a machine that does not exist look the same.
	for _, id := range []string{a.MachineID, "aaaaaaaaaaaaaaaaaaaa"} {
		if r := stranger.Do("GET", "/api/machines/"+id, nil); r.Code != 403 {
			t.Errorf("stranger get %s: %s", id, r)
		}
	}
	if r := stranger.Do("GET", "/api/spaces/"+f.spaceID+"/machines", nil); r.Code != 403 {
		t.Errorf("stranger list: %s", r)
	}
	if r := member.Do("DELETE", "/api/machines/"+a.MachineID, nil); r.Code != 403 {
		t.Errorf("member delete: %s", r)
	}
	if r := member.Do("POST", "/api/machines/"+a.MachineID+"/preflight", map[string]any{}); r.Code != 403 {
		t.Errorf("member preflight: %s", r)
	}
}

func TestRevoke(t *testing.T) {
	f := setup(t)
	stream := f.owner.Events(f.spaceID)
	a := f.owner.Machine(f.spaceID, nil)
	other := f.owner.Machine(f.spaceID, nil)

	if r := f.owner.Do("DELETE", "/api/machines/"+a.MachineID, nil); r.Code != 204 {
		t.Fatal(r)
	}
	if code := a.WaitClosed(); code != 4010 {
		t.Errorf("close code = %d, want 4010", code)
	}
	ev := stream.Await("machineRemoved", func(e hubtest.Event) bool { return e.Name == "machineRemoved" })
	if !strings.Contains(string(ev.Data), a.MachineID) {
		t.Errorf("event = %s", ev.Data)
	}
	if r := f.owner.Do("GET", "/api/machines/"+a.MachineID, nil); r.Code != 404 {
		t.Errorf("revoked machine: %s", r)
	}
	if r := f.owner.Do("DELETE", "/api/machines/"+a.MachineID, nil); r.Code != 404 {
		t.Errorf("second delete: %s", r)
	}
	var list struct{ Machines []struct{ ID string } }
	f.owner.Do("GET", "/api/spaces/"+f.spaceID+"/machines", nil).JSON(&list)
	if len(list.Machines) != 1 || list.Machines[0].ID != other.MachineID {
		t.Errorf("list = %+v", list)
	}
	// Its key does not dial again.
	if code, _ := a.TryConnect(); code != http.StatusForbidden {
		t.Errorf("redial of a revoked machine: %d, want 403", code)
	}
}

// A machine removes itself with the signature of its own key: no cookie, no person.
func TestMachineUnlinksItself(t *testing.T) {
	f := setup(t)
	stream := f.owner.Events(f.spaceID)
	a := f.owner.Machine(f.spaceID, nil)
	other := f.owner.Machine(f.spaceID, nil)

	// A dial's signature is no unlink, and a signature of another machine's key is nothing.
	host, _ := hubproto.DialHost(f.s.Cfg.PublicURL)
	dialAuth, _ := hubproto.SignDial(a.Priv, a.MachineID, f.s.Clock.Now(), host)
	if code := a.UnlinkWith(dialAuth); code != http.StatusUnauthorized {
		t.Errorf("a dial signature unlinked: %d", code)
	}
	strangerAuth, _ := hubproto.SignUnlink(other.Priv, a.MachineID, f.s.Clock.Now(), host)
	if code := a.UnlinkWith(strangerAuth); code != http.StatusUnauthorized {
		t.Errorf("another key unlinked: %d", code)
	}
	if r := f.owner.Do("GET", "/api/machines/"+a.MachineID, nil); r.Code != 200 {
		t.Fatalf("machine gone after refused requests: %s", r)
	}

	if code := a.Unlink(); code != http.StatusNoContent {
		t.Fatalf("unlink: %d", code)
	}
	if code := a.WaitClosed(); code != 4010 {
		t.Errorf("close code = %d, want 4010", code)
	}
	ev := stream.Await("machineRemoved", func(e hubtest.Event) bool { return e.Name == "machineRemoved" })
	if !strings.Contains(string(ev.Data), a.MachineID) {
		t.Errorf("event = %s", ev.Data)
	}
	if r := f.owner.Do("GET", "/api/machines/"+a.MachineID, nil); r.Code != 404 {
		t.Errorf("unlinked machine: %s", r)
	}
	// Again: the machine is gone, which the agent reads as done.
	if code := a.Unlink(); code != http.StatusForbidden {
		t.Errorf("second unlink: %d, want 403", code)
	}
	if r := f.owner.Do("GET", "/api/machines/"+other.MachineID, nil); r.Code != 200 {
		t.Errorf("the other machine: %s", r)
	}
}

func TestInstallCommandNeedsSudoOnlyForDir(t *testing.T) {
	f := setup(t)
	for profile, shell := range map[string]string{"dir": "sudo sh", "full": "sh"} {
		body := map[string]any{"profile": profile, "confirmFull": true}
		r := f.owner.Do("POST", "/api/spaces/"+f.spaceID+"/machines/codes", body)
		if r.Code != 201 {
			t.Fatal(r)
		}
		var out struct{ Command string }
		r.JSON(&out)
		if want := "/install | " + shell + " -s -- --token "; !strings.Contains(out.Command, want) {
			t.Errorf("%s: %q lacks %q", profile, out.Command, want)
		}
	}
}

func TestOpenInPathMode(t *testing.T) {
	f := setup(t)
	a := f.owner.Machine(f.spaceID, nil)
	id := a.MachineID

	var out struct{ URL string }
	f.owner.Do("GET", "/api/machines/"+id+"/open", nil).JSON(&out)
	if out.URL != "/m/"+id+"/" {
		t.Errorf("url = %q", out.URL)
	}
	q := url.QueryEscape("/m/" + id + "/?tab=abc")
	f.owner.Do("GET", "/api/machines/"+id+"/open?path="+q, nil).JSON(&out)
	if out.URL != "/m/"+id+"/?tab=abc" {
		t.Errorf("url with a path = %q", out.URL)
	}
	r := f.owner.Do("GET", "/api/machines/"+id+"/open?redirect=1", nil)
	if r.Code != 302 || r.Header.Get("Location") != "/m/"+id+"/" || r.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("redirect: %s %v", r, r.Header)
	}

	for _, bad := range []string{"/m/aaaaaaaaaaaaaaaaaaaa/", "/", "//evil.example/m/" + id, "https://evil.example/m/" + id, "/m/" + id + "//evil.example", "/m/" + id + "x", "/m/" + id + "/#frag", `/m/` + id + `/\evil`} {
		r := f.owner.Do("GET", "/api/machines/"+id+"/open?path="+url.QueryEscape(bad), nil)
		if r.Code != 400 {
			t.Errorf("path %q: %s", bad, r)
		}
	}
	stranger := f.s.SignIn("stranger@example.com")
	if r := stranger.Do("GET", "/api/machines/"+id+"/open", nil); r.Code != 403 {
		t.Errorf("stranger: %s", r)
	}
}

func TestOpenInSubdomainMode(t *testing.T) {
	f := setup(t, hubtest.Env("HUB_RUNTIME_DOMAIN", "m.example.test"))
	a := f.owner.Machine(f.spaceID, nil)
	id := a.MachineID
	pub, _ := url.Parse(f.s.URL)

	var out struct{ URL string }
	f.owner.Do("GET", "/api/machines/"+id+"/open", nil).JSON(&out)
	u, err := url.Parse(out.URL)
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "http" || u.Host != id+".m.example.test:"+pub.Port() || u.Path != "/m/"+id+"/" || u.Query().Get("hl_ticket") == "" {
		t.Errorf("url = %q", out.URL)
	}
	// Each opening gets its own ticket.
	var again struct{ URL string }
	f.owner.Do("GET", "/api/machines/"+id+"/open", nil).JSON(&again)
	if again.URL == out.URL {
		t.Error("two openings share a ticket")
	}
	r := f.owner.Do("GET", "/api/machines/"+id+"/open?redirect=1&path="+url.QueryEscape("/m/"+id+"/?tab=1"), nil)
	loc, _ := url.Parse(r.Header.Get("Location"))
	if r.Code != 302 || loc.Host != u.Host || loc.Query().Get("tab") != "1" || loc.Query().Get("hl_ticket") == "" {
		t.Errorf("redirect: %s %v", r, r.Header)
	}
}

func TestRerunPreflight(t *testing.T) {
	f := setup(t)
	a := f.owner.Machine(f.spaceID, nil)
	a.Handle("preflight.run", func(hubtest.Call) (any, error) {
		r := hubtest.Preflight(f.s.Clock.Now())
		r.OSName = "Ubuntu 26.04"
		return r, nil
	})
	r := f.owner.Do("POST", "/api/machines/"+a.MachineID+"/preflight", map[string]any{})
	if r.Code != 200 || !strings.Contains(string(r.Body), "Ubuntu 26.04") {
		t.Fatal(r)
	}
	if pre, _ := f.view(t, a.MachineID)["preflight"].(map[string]any); pre["osName"] != "Ubuntu 26.04" {
		t.Errorf("not remembered: %v", pre)
	}

	a.Close()
	f.s.Eventually("the link to go", func() bool { return f.view(t, a.MachineID)["status"] == "offline" })
	r = f.owner.Do("POST", "/api/machines/"+a.MachineID+"/preflight", map[string]any{})
	if r.Code != http.StatusBadGateway || r.Err() != "machine_offline" {
		t.Errorf("offline: %s", r)
	}
}

// ---- the machine next to the hub ----

func localSetup(t *testing.T, opts ...hubtest.Option) (*fixture, string) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "run", "local-code")
	f := setup(t, append(opts, hubtest.Env("HUB_LOCAL_CODE_FILE", file))...)
	if err := f.s.Hub.Machines.EnsureLocalCode(context.Background()); err != nil {
		t.Fatal(err)
	}
	return f, file
}

func readCode(t *testing.T, file string) string {
	t.Helper()
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(b))
}

func (f *fixture) localAvailable() bool {
	var cfg struct {
		Connect struct{ Local struct{ Available bool } }
	}
	f.s.Browser().Do("GET", "/api/config", nil).JSON(&cfg)
	return cfg.Connect.Local.Available
}

func TestLocalMachine(t *testing.T) {
	f, file := localSetup(t)
	code := readCode(t, file)
	if !codeRe.MatchString(code) {
		t.Fatalf("local code = %q", code)
	}
	if f.localAvailable() {
		t.Error("«Эта машина» is offered before a machine registered")
	}
	// Calling again changes nothing while the code is fresh.
	if err := f.s.Hub.Machines.EnsureLocalCode(context.Background()); err != nil || readCode(t, file) != code {
		t.Errorf("the code was replaced without a reason: %v", err)
	}

	a := f.s.NewAgent(nil)
	r := a.Register(code, hubproto.ProfileContainer, hubproto.ViaLocal)
	if r.Code != 201 {
		t.Fatal(r)
	}
	var res hubproto.RegisterResponse
	r.JSON(&res)
	if res.SpaceName != nil {
		t.Errorf("spaceName = %q: a local machine belongs to no space yet", *res.SpaceName)
	}
	m, _ := f.s.DB.S().MachineByID(context.Background(), a.MachineID)
	if m.SpaceID != "" || m.Via != "local" || m.Profile != "container" {
		t.Errorf("machine = %+v", m)
	}
	// The used code is replaced by a new one, so that the machine can register again after its volume was wiped.
	f.s.Eventually("a new local code", func() bool { return readCode(t, file) != code })
	if r := f.s.NewAgent(nil).Register(code, hubproto.ProfileContainer, hubproto.ViaLocal); r.Code != 410 {
		t.Errorf("the used local code: %s", r)
	}
	if !f.localAvailable() {
		t.Error("«Эта машина» is not offered although a local machine registered")
	}
	// It is not in any list yet.
	var list struct{ Machines []any }
	f.owner.Do("GET", "/api/spaces/"+f.spaceID+"/machines", nil).JSON(&list)
	if len(list.Machines) != 0 {
		t.Errorf("an unclaimed machine is listed: %v", list.Machines)
	}

	// The instance admin (the first account) claims it.
	stream := f.owner.Events(f.spaceID)
	r = f.owner.Do("POST", "/api/spaces/"+f.spaceID+"/machines/local", map[string]any{})
	if r.Code != 200 {
		t.Fatal(r)
	}
	var claimed struct {
		Machine struct{ ID, SpaceID, Via string }
	}
	r.JSON(&claimed)
	if claimed.Machine.ID != a.MachineID || claimed.Machine.SpaceID != f.spaceID || claimed.Machine.Via != "local" {
		t.Errorf("claimed = %+v", claimed)
	}
	stream.Await("the machine event", func(e hubtest.Event) bool { return e.Name == "machine" })
	if r := f.owner.Do("POST", "/api/spaces/"+f.spaceID+"/machines/local", map[string]any{}); r.Code != 404 {
		t.Errorf("a second claim: %s", r)
	}
	if f.localAvailable() {
		t.Error("«Эта машина» is still offered after it was claimed")
	}
}

func TestOnlyTheInstanceAdminClaimsTheLocalMachine(t *testing.T) {
	f, file := localSetup(t)
	a := f.s.NewAgent(nil)
	if r := a.Register(readCode(t, file), hubproto.ProfileContainer, hubproto.ViaLocal); r.Code != 201 {
		t.Fatal(r)
	}
	second := f.s.SignIn("second@example.com")
	id, _ := second.NewSpace("Другая команда")
	if r := second.Do("POST", "/api/spaces/"+id+"/machines/local", map[string]any{}); r.Code != 403 {
		t.Errorf("a space owner who is no admin: %s", r)
	}
	if r := f.owner.Do("POST", "/api/spaces/"+id+"/machines/local", map[string]any{}); r.Code != 403 {
		t.Errorf("an admin who does not own that space: %s", r)
	}
	m, _ := f.s.DB.S().MachineByID(context.Background(), a.MachineID)
	if m.SpaceID != "" {
		t.Error("the machine was claimed anyway")
	}
}

func TestLocalMachineRegisteringAgainReplacesTheWaitingOne(t *testing.T) {
	f, file := localSetup(t)
	used := readCode(t, file)
	first := f.s.NewAgent(nil)
	if r := first.Register(used, hubproto.ProfileContainer, hubproto.ViaLocal); r.Code != 201 {
		t.Fatal(r)
	}
	first.Connect()

	// The volume was wiped: a new machine registers with the code the hub wrote after the first one.
	var fresh string
	f.s.Eventually("a fresh code in the file", func() bool {
		fresh = readCode(t, file)
		return fresh != used
	})
	second := f.s.NewAgent(nil)
	if r := second.Register(fresh, hubproto.ProfileContainer, hubproto.ViaLocal); r.Code != 201 {
		t.Fatal(r)
	}
	if code := first.WaitClosed(); code != 4010 {
		t.Errorf("the replaced machine's link closed with %d, want 4010", code)
	}
	m, err := f.s.DB.S().UnclaimedLocalMachine(context.Background())
	if err != nil || m.ID != second.MachineID {
		t.Errorf("unclaimed local = %+v %v, want the second machine", m, err)
	}
}

func TestStaleLocalCodeFileIsReplaced(t *testing.T) {
	f, file := localSetup(t)
	old := readCode(t, file)
	if err := os.WriteFile(file, []byte("hx_0000000000000000000000aa\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := f.s.Hub.Machines.EnsureLocalCode(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := readCode(t, file); got == "hx_0000000000000000000000aa" || got == old {
		t.Errorf("file = %q: a code the hub does not know must be replaced by a valid one", got)
	}
	// And a code near its end is renewed.
	cur := readCode(t, file)
	f.s.Clock.Advance(25 * time.Minute)
	if err := f.s.Hub.Machines.EnsureLocalCode(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := readCode(t, file); got == cur {
		t.Error("a local code with five minutes left was kept")
	}
	if r := f.s.NewAgent(nil).Register(cur, hubproto.ProfileContainer, hubproto.ViaLocal); r.Code != 410 {
		t.Errorf("the replaced code still works: %s", r)
	}
}

// The code of the local machine is a credential for the machine's side: nobody else on the host reads it.
func TestLocalCodeFileIsPrivate(t *testing.T) {
	_, file := localSetup(t)
	fi, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("code file mode %v, want 0600", fi.Mode().Perm())
	}
	di, err := os.Stat(filepath.Dir(file))
	if err != nil {
		t.Fatal(err)
	}
	if di.Mode().Perm() != 0o700 {
		t.Errorf("folder made for the code file has mode %v, want 0700", di.Mode().Perm())
	}
	left, _ := filepath.Glob(filepath.Join(filepath.Dir(file), ".code-*"))
	if len(left) != 0 {
		t.Errorf("temporary files left behind: %v", left)
	}
}

// The machine's container reads the file as a member of a group the hub names: 0640, the group of the file is that
// one, and the folder made for it is not open to everyone.
func TestLocalCodeFileForAGroup(t *testing.T) {
	gid := os.Getgid()
	_, file := localSetup(t, hubtest.Env("HUB_LOCAL_CODE_GROUP", strconv.Itoa(gid)))
	fi, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o640 {
		t.Errorf("code file mode %v, want 0640", fi.Mode().Perm())
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); !ok || int(st.Gid) != gid {
		t.Errorf("code file group %+v, want %d", fi.Sys(), gid)
	}
	di, err := os.Stat(filepath.Dir(file))
	if err != nil {
		t.Fatal(err)
	}
	if di.Mode().Perm()&0o007 != 0 {
		t.Errorf("folder mode %v is open to everyone", di.Mode().Perm())
	}
}

func TestNoLocalMachineWithoutTheSetting(t *testing.T) {
	f := setup(t)
	if err := f.s.Hub.Machines.EnsureLocalCode(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.localAvailable() {
		t.Error("local machine offered without HUB_LOCAL_CODE_FILE")
	}
	if r := f.owner.Do("POST", "/api/spaces/"+f.spaceID+"/machines/local", map[string]any{}); r.Code != 404 {
		t.Errorf("claim: %s", r)
	}
}

func TestCloudHasNoLocalMachine(t *testing.T) {
	file := filepath.Join(t.TempDir(), "code")
	f := setup(t, hubtest.Env("HUB_EDITION", "cloud"), hubtest.Env("HUB_RUNTIME_DOMAIN", "m.example.test"), hubtest.Env("HUB_LOCAL_CODE_FILE", file))
	if err := f.s.Hub.Machines.EnsureLocalCode(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file); err == nil {
		t.Error("the cloud hub wrote a local code")
	}
	if f.localAvailable() {
		t.Error("cloud offers a local machine")
	}
}
