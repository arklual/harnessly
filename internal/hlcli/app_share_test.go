package hlcli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ly-solutions/harnessly/internal/apps"
)

type shareCalls struct {
	mu    sync.Mutex
	calls []call
}

func (s *shareCalls) last() call {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls[len(s.calls)-1]
}

func shareRuntime(t *testing.T, answers map[string]string) (*shareCalls, map[string]string) {
	t.Helper()
	s := &shareCalls{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		s.mu.Lock()
		s.calls = append(s.calls, call{r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Get("x-cli-token"), body})
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		ans, ok := answers[r.URL.Path]
		if !ok {
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"error":"Not found"}`))
			return
		}
		if code, rest, found := strings.Cut(ans, " "); found && len(code) == 3 && code[0] >= '2' && code[0] <= '5' {
			n := int(code[0]-'0')*100 + int(code[1]-'0')*10 + int(code[2]-'0')
			w.WriteHeader(n)
			ans = rest
		}
		_, _ = w.Write([]byte(ans))
	}))
	t.Cleanup(srv.Close)
	return s, map[string]string{"HARNESSLY_URL": srv.URL, "HARNESSLY_CLI_TOKEN": "tok"}
}

func appInCwd(t *testing.T, id string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "pkg")
	if err := apps.Scaffold(dir, id, "", "chat"); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestAppPublishPromoteRollback(t *testing.T) {
	dir := appInCwd(t, "acme.demo")
	calls, env := shareRuntime(t, map[string]string{
		"/api/cli/apps/publish":  `{"id":"acme.demo","version":"0.1.0-beta.1","channel":"beta","problems":[]}`,
		"/api/cli/apps/promote":  `{"id":"acme.demo","version":"0.1.0","channel":"stable"}`,
		"/api/cli/apps/rollback": `{"id":"acme.demo","version":"0.0.9","channel":"stable","moved":[]}`,
	})
	run := func(args ...string) result {
		t.Helper()
		return runWith(t, t.TempDir(), env, "", args...)
	}
	if r := run("app", "publish", dir, "--channel", "beta"); r.code != 0 || !strings.Contains(r.out, "0.1.0-beta.1") {
		t.Fatalf("publish: %+v", r)
	}
	if c := calls.last(); c.Body["dir"] != dir || c.Body["channel"] != "beta" || c.Body["team"] != false {
		t.Fatalf("publish body: %+v", c)
	}
	if r := run("app", "publish", dir, "--channel=stable", "--team"); r.code != 0 {
		t.Fatalf("publish team: %+v", r)
	}
	if c := calls.last(); c.Body["channel"] != "stable" || c.Body["team"] != true {
		t.Fatalf("publish body: %+v", c)
	}
	if r := run("app", "publish", dir, "--channel", "nightly"); r.code == 0 {
		t.Fatal("a bad channel should fail")
	}
	// The id comes from the manifest of the folder, or --app.
	if r := run("app", "promote", "0.1.0", "--channel", "stable", dir); r.code != 0 || !strings.Contains(r.out, "acme.demo 0.1.0 is on stable") {
		t.Fatalf("promote: %+v", r)
	}
	if c := calls.last(); c.Body["id"] != "acme.demo" || c.Body["version"] != "0.1.0" {
		t.Fatalf("promote body: %+v", c)
	}
	if r := run("app", "promote", "0.1.0", "--channel", "stable", "--app", "x.y", "--threshold", "0.5"); r.code != 0 {
		t.Fatalf("promote: %+v", r)
	}
	if c := calls.last(); c.Body["id"] != "x.y" || c.Body["threshold"] != 0.5 {
		t.Fatalf("promote body: %+v", c)
	}
	if r := run("app", "rollback", "--channel", "stable", "--app", "acme.demo"); r.code != 0 || !strings.Contains(r.out, "back at 0.0.9") {
		t.Fatalf("rollback: %+v", r)
	}
	if r := run("app", "rollback", "--app", "acme.demo"); r.code == 0 {
		t.Fatal("rollback without a channel is a usage error")
	}
}

func TestAppRefusalsReadInTheRuntimesWords(t *testing.T) {
	_, env := shareRuntime(t, map[string]string{
		"/api/cli/apps/team/install": `409 {"error":"no-team","message":"apps: this machine is not in a team"}`,
		"/api/cli/apps/promote":      `422 {"error":"eval-gate","message":"apps: the evals do not pass: 50% passed, stable needs 80%"}`,
	})
	r := runWith(t, t.TempDir(), env, "", "app", "install", "--team", "acme.demo")
	if r.code == 0 || !strings.Contains(r.errs+r.out, "not in a team") {
		t.Fatalf("%+v", r)
	}
	r = runWith(t, t.TempDir(), env, "", "app", "promote", "1.0.0", "--channel", "stable", "--app", "acme.demo")
	if r.code == 0 || !strings.Contains(r.errs+r.out, "50% passed") {
		t.Fatalf("%+v", r)
	}
}

func TestAppInstallFromAChannelAndForTheTeam(t *testing.T) {
	calls, env := shareRuntime(t, map[string]string{
		"/api/cli/apps/follow":       `{"app":{"id":"acme.demo"},"problems":[]}`,
		"/api/cli/apps/team/install": `{"id":"acme.demo","version":"1.0.0","channel":"stable"}`,
		"/api/cli/apps/install":      `{"app":{"id":"acme.demo","version":"0.1.0"},"problems":[]}`,
	})
	run := func(args ...string) result { return runWith(t, t.TempDir(), env, "", args...) }
	if r := run("app", "install", "acme.demo", "--channel", "beta"); r.code != 0 {
		t.Fatalf("%+v", r)
	}
	if c := calls.last(); c.Path != "/api/cli/apps/follow" || c.Body["channel"] != "beta" || c.Body["id"] != "acme.demo" {
		t.Fatalf("%+v", c)
	}
	if r := run("app", "install", "--team", "acme.demo"); r.code != 0 || !strings.Contains(r.out, "installed for the team on stable") {
		t.Fatalf("%+v", r)
	}
	if c := calls.last(); c.Path != "/api/cli/apps/team/install" || c.Body["channel"] != "stable" {
		t.Fatalf("a team install defaults to stable: %+v", c)
	}
	if r := run("app", "install", "acme.demo", "--space", "sp1"); r.code != 0 {
		t.Fatalf("%+v", r)
	}
	if c := calls.last(); c.Path != "/api/cli/apps/team/install" {
		t.Fatalf("--space is the team: %+v", c)
	}
	if r := run("app", "install", "--channel", "stable"); r.code == 0 {
		t.Fatal("a channel without an app is a usage error")
	}
	// A plain install still takes a folder.
	if r := run("app", "install", t.TempDir()); r.code != 0 {
		t.Fatalf("%+v", r)
	}
	if c := calls.last(); c.Path != "/api/cli/apps/install" {
		t.Fatalf("%+v", c)
	}
}

func TestAppUpdateAndShare(t *testing.T) {
	calls, env := shareRuntime(t, map[string]string{
		"/api/cli/apps/sync":    `{"moved":[{"id":"acme.demo","from":"1.0.0","to":"2.0.0","applied":false,"reason":"major"}]}`,
		"/api/cli/apps/share":   `{"path":"/apps/guest/abc/","link":{"id":"lnk1","expiresAt":1800000000000,"write":false}}`,
		"/api/cli/apps/unshare": `{"revoked":"lnk1"}`,
		"/api/cli/apps/shares":  `{"links":[{"id":"lnk1","app":"acme.demo","view":"main","expiresAt":1800000000000}]}`,
	})
	run := func(args ...string) result { return runWith(t, t.TempDir(), env, "", args...) }
	if r := run("app", "update"); r.code != 0 || !strings.Contains(r.out, "waits for you (major)") {
		t.Fatalf("%+v", r)
	}
	if r := run("app", "update", "--yes"); r.code != 0 {
		t.Fatalf("%+v", r)
	}
	if c := calls.last(); c.Body["confirm"].([]any)[0] != "*" {
		t.Fatalf("%+v", c)
	}
	if r := run("app", "share", "acme.demo", "--ttl", "7d", "--write"); r.code != 0 || !strings.Contains(r.out, "/apps/guest/abc/") {
		t.Fatalf("%+v", r)
	}
	if c := calls.last(); c.Body["ttlSeconds"] != float64(7*24*3600) || c.Body["write"] != true {
		t.Fatalf("%+v", c)
	}
	if r := run("app", "share", "acme.demo", "--ttl", "soon"); r.code == 0 {
		t.Fatal("a bad ttl is a usage error")
	}
	if r := run("app", "shares"); r.code != 0 || !strings.Contains(r.out, "lnk1") {
		t.Fatalf("%+v", r)
	}
	if r := run("app", "unshare", "lnk1"); r.code != 0 {
		t.Fatalf("%+v", r)
	}
}

// A fake `claude` on the PATH stands for the real agent: it prints a recorded session. The scenarios run end to end
// through the command: the mocks are prepared, the run is judged, the report is written and the exit code follows the
// pass rate. The real agent is not run here.
func TestAppEvalEndToEndWithAFakeClaude(t *testing.T) {
	bin := t.TempDir()
	session, err := filepath.Abs("../apps/testdata/eval/session.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\ncat '" + session + "'\n"
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	dir := appInCwd(t, "acme.cashback")
	write := func(name, body string) {
		t.Helper()
		_ = os.MkdirAll(filepath.Join(dir, "evals"), 0o755)
		if err := os.WriteFile(filepath.Join(dir, "evals", name+".json"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("good", `{"prompt":"p","runs":2,"expect":{"tool_used":["bank-cashback.get_categories"],"tool_not_used":["bank-cashback.create"],"final_text":["согласовать"]}}`)
	env := map[string]string{}
	r := runWith(t, t.TempDir(), env, "", "app", "eval", dir, "--no-baseline", "--no-judge")
	if r.code != 0 || !strings.Contains(r.out, "pass\tgood\t2/2") || !strings.Contains(r.out, "100%") {
		t.Fatalf("a passing eval: %+v", r)
	}
	raw, err := os.ReadFile(filepath.Join(dir, apps.EvalReportPath))
	if err != nil {
		t.Fatalf("the report is left in the package: %v", err)
	}
	var rep apps.EvalReport
	if json.Unmarshal(raw, &rep) != nil || rep.PassRate != 1 || !rep.Ok || rep.Agent != "claude" {
		t.Fatalf("report: %s", raw)
	}

	// A scenario the recorded session does not meet fails the command.
	write("bad", `{"prompt":"p","runs":1,"expect":{"tool_used":["bank-cashback.create"]}}`)
	r = runWith(t, t.TempDir(), env, "", "app", "eval", dir, "--no-baseline", "--no-judge", "--no-report")
	if r.code != 1 || !strings.Contains(r.out, "FAIL\tbad\t0/1") || !strings.Contains(r.out, "did not call bank-cashback.create") {
		t.Fatalf("a failing eval: %+v", r)
	}
	// --case picks scenarios; the threshold decides the exit code.
	r = runWith(t, t.TempDir(), env, "", "app", "eval", dir, "--case", "good", "--no-baseline", "--no-judge", "--no-report", "--threshold", "1")
	if r.code != 0 {
		t.Fatalf("--case: %+v", r)
	}
	r = runWith(t, t.TempDir(), env, "", "app", "eval", dir, "--no-baseline", "--no-judge", "--no-report", "--json")
	var js apps.EvalReport
	if r.code != 1 || json.Unmarshal([]byte(r.out), &js) != nil || js.Runs != 3 {
		t.Fatalf("--json: %+v", r)
	}
	if r := runWith(t, t.TempDir(), env, "", "app", "eval", dir, "--agent", "codex"); r.code == 0 || !strings.Contains(r.errs+r.out, "codex") {
		t.Fatalf("codex: %+v", r)
	}
	if r := runWith(t, t.TempDir(), env, "", "app", "eval", appInCwd(t, "acme.empty")); r.code == 0 || !strings.Contains(r.errs+r.out, "no scenarios") {
		t.Fatalf("no scenarios: %+v", r)
	}
}

func TestAppMockMCPCommand(t *testing.T) {
	fixture := filepath.Join(t.TempDir(), "f.json")
	_ = os.WriteFile(fixture, []byte(`{"tools":[{"name":"ping","result":"pong"}]}`), 0o644)
	var out strings.Builder
	code := Run([]string{"app", "mock-mcp", fixture}, Env{
		Getenv: func(string) string { return "" }, Stdin: strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ping"}}` + "\n"), Stdout: &out,
	})
	if code != 0 || !strings.Contains(out.String(), "pong") {
		t.Fatalf("%d %s", code, out.String())
	}
}
