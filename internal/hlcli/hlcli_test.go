package hlcli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fakeRuntime answers the /api/cli routes the command uses and records what it was asked.
type fakeRuntime struct {
	srv   *httptest.Server
	mu    sync.Mutex
	calls []call
	token string
}

type call struct {
	Method, Path, Query, Token string
	Body                       map[string]any
}

func newFake(t *testing.T) *fakeRuntime {
	t.Helper()
	f := &fakeRuntime{token: "tok-123"}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeRuntime) serve(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	raw, _ := io.ReadAll(r.Body)
	_ = json.Unmarshal(raw, &body)
	got := r.Header.Get("x-cli-token")
	f.mu.Lock()
	f.calls = append(f.calls, call{r.Method, r.URL.Path, r.URL.RawQuery, got, body})
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if got != f.token {
		w.WriteHeader(403)
		_, _ = w.Write([]byte(`{"error":"Forbidden"}`))
		return
	}
	switch {
	case r.URL.Path == "/api/cli/notify/bots":
		_, _ = w.Write([]byte(`{"bots":[{"id":"nb_1","name":"Алерты","username":"ping_bot","chat":"Анна","bound":true,` +
			`"instruction":"когда закончил\nили нужен ответ"},{"id":"nb_2","name":"Free","username":"free_bot","chat":"","bound":false,"instruction":""}],` +
			`"shared":{"enabled":true,"bot":"harnessly_bot","linked":true,"chat":"Anna","instruction":"когда нужен ответ"}}`))
	case r.URL.Path == "/api/cli/notify/send" && body["bot"] == "":
		_, _ = w.Write([]byte(`{"bot":"","chat":"Anna","messages":1}`))
	case r.URL.Path == "/api/cli/notify/send" && body["bot"] == "nobody":
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"error":"not-found"}`))
	case r.URL.Path == "/api/cli/notify/send":
		_, _ = w.Write([]byte(`{"bot":"Алерты","chat":"Анна","messages":2}`))
	case r.URL.Path == "/api/cli/peers":
		_, _ = w.Write([]byte(`{"chats":[{"ref":"dev-department/tab-rsk","title":"Модель по умолчанию","machine":"dev-department",` +
			`"machineName":"dev-department","owner":"Иван","state":"busy"}],"errors":["no-relay"]}`))
	case r.URL.Path == "/api/cli/peers/send" && body["to"] == "dev-department/tab-blocked":
		_, _ = w.Write([]byte(`{"id":"pm_1","state":"refused","reason":"blocked"}`))
	case r.URL.Path == "/api/cli/peers/send":
		_, _ = w.Write([]byte(`{"id":"pm_1","state":"delivered","to":{"ref":"dev-department/tab-rsk","title":"Модель по умолчанию",` +
			`"machineName":"dev-department","owner":"Иван","state":"busy"}}`))
	case r.URL.Path == "/api/cli/workspaces":
		_, _ = w.Write([]byte(`{"workspaces":[{"id":"ws-1","name":"Work","directories":["/home/a/work"]}]}`))
	case r.URL.Path == "/api/cli/tabs" && r.Method == "GET":
		_, _ = w.Write([]byte(`{"tabs":[{"tabId":"t-1","workspaceId":"ws-1","name":"main","sessionName":"hs-me","panelType":"codex-cli","agentProviderId":"codex"},` +
			`{"tabId":"t-2","workspaceId":"ws-1","name":"sh","sessionName":"hs-sh","panelType":"terminal","agentProviderId":null}]}`))
	case r.URL.Path == "/api/cli/tabs" && r.Method == "POST":
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"tabId":"t-9","workspaceId":"ws-1","panelType":"codex-cli","agentProviderId":"codex","launched":true}`))
	case strings.HasSuffix(r.URL.Path, "/result"):
		_, _ = w.Write([]byte(`{"content":"hello\n"}`))
	case strings.HasSuffix(r.URL.Path, "/status"):
		_, _ = w.Write([]byte(`{"alive":true,"command":"codex","cliState":"idle","agentProviderId":"codex"}`))
	case strings.HasSuffix(r.URL.Path, "/send"):
		_, _ = w.Write([]byte(`{"status":"sent"}`))
	case strings.HasPrefix(r.URL.Path, "/api/cli/tabs/") && r.Method == "DELETE":
		_, _ = w.Write([]byte(`{"ok":true}`))
	case r.URL.Path == "/api/cli/panel":
		cmd, _ := body["command"].(map[string]any)
		if cmd["kind"] == "open" && cmd["tab"] == "browser" && cmd["target"] == "https://evil.example/" {
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":"open browser: opens a port of this machine"}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"applied":true,"message":"opened for the person"}`))
	case r.URL.Path == "/api/cli/deliver":
		_, _ = w.Write([]byte(`{"ok":true,"message":"Delivered notes.md\nhttps://delivered.harnessly.invalid/file"}`))
	case r.URL.Path == "/api/cli/api-guide":
		w.Header().Set("Content-Type", "text/markdown")
		_, _ = w.Write([]byte("# API\n"))
	default:
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"error":"Not found"}`))
	}
}

func (f *fakeRuntime) last() call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[len(f.calls)-1]
}

type result struct {
	code      int
	out, errs string
}

func (f *fakeRuntime) run(t *testing.T, session string, args ...string) result {
	t.Helper()
	home := t.TempDir()
	env := map[string]string{"HARNESSLY_URL": f.srv.URL, "HARNESSLY_CLI_TOKEN": f.token}
	return runWith(t, home, env, session, args...)
}

func runWith(t *testing.T, home string, env map[string]string, session string, args ...string) result {
	t.Helper()
	var out, errs bytes.Buffer
	code := Run(args, Env{
		Getenv:  func(k string) string { return env[k] },
		Home:    home,
		Session: func() string { return session },
		Cwd:     "/home/a/work",
		Stdin:   strings.NewReader("piped text"),
		Stdout:  &out,
		Stderr:  &errs,
	})
	return result{code, out.String(), errs.String()}
}

func TestOpenSendsTheDetectedTabFromTheOwnSession(t *testing.T) {
	f := newFake(t)
	cases := []struct {
		arg, tab, target string
	}{
		{"https://docs.example.com/a/plan-x1", "artifacts", "https://docs.example.com/a/plan-x1"},
		{"https://boards.example.com/b/arch?k=abc", "artifacts", "https://boards.example.com/b/arch?k=abc"},
		{"4400", "browser", "4400"},
		{"localhost:3000/app", "browser", "localhost:3000/app"},
		{"https://gitlab.example.com/g/p/-/merge_requests/12", "gitlab", "https://gitlab.example.com/g/p/-/merge_requests/12"},
		{"https://github.com/ly-solutions/harnessly/pull/12", "gitlab", "https://github.com/ly-solutions/harnessly/pull/12"},
		{"https://github.com/ly-solutions/harnessly/commit/0123abc", "gitlab", "https://github.com/ly-solutions/harnessly/commit/0123abc"},
		{"https://github.com/ly-solutions/harnessly/issues/7", "gitlab", "https://github.com/ly-solutions/harnessly/issues/7"},
		{"https://github.com/ly-solutions/harnessly", "browser", "https://github.com/ly-solutions/harnessly"},
		{"PROJ-123", "task", "PROJ-123"},
		{"/home/a/work/report.md", "files", "/home/a/work/report.md"},
		{"notes/todo.md", "files", "/home/a/work/notes/todo.md"},
	}
	for _, c := range cases {
		r := f.run(t, "hs-me", "open", c.arg)
		if r.code != 0 {
			t.Fatalf("%s: exit %d: %s", c.arg, r.code, r.errs)
		}
		got := f.last()
		cmd := got.Body["command"].(map[string]any)
		if got.Path != "/api/cli/panel" || got.Method != "POST" || got.Body["session"] != "hs-me" || cmd["kind"] != "open" || cmd["tab"] != c.tab || cmd["target"] != c.target {
			t.Errorf("%s: sent %+v", c.arg, got)
		}
		if got.Token != "tok-123" {
			t.Errorf("token not sent")
		}
		if !strings.Contains(r.out, "opened for the person") {
			t.Errorf("%s: output %q", c.arg, r.out)
		}
	}
}

func TestOpenByKindAndErrors(t *testing.T) {
	f := newFake(t)
	if r := f.run(t, "hs-me", "open", "changes", "src"); r.code != 0 {
		t.Fatal(r.errs)
	}
	cmd := f.last().Body["command"].(map[string]any)
	if cmd["tab"] != "changes" || cmd["target"] != "/home/a/work/src" {
		t.Errorf("by kind: %v", cmd)
	}
	if r := f.run(t, "hs-me", "open", "files"); r.code != 0 {
		t.Fatal(r.errs)
	}
	if _, has := f.last().Body["command"].(map[string]any)["target"]; has {
		t.Errorf("a kind without a target sends none")
	}
	r := f.run(t, "hs-me", "open", "https://evil.example/")
	if r.code != 1 || !strings.Contains(r.errs, "opens a port of this machine") {
		t.Errorf("a refusal is shown with its reason: %+v", r)
	}
	if r := f.run(t, "hs-me", "open"); r.code != 2 {
		t.Errorf("no target is a usage error: %+v", r)
	}
}

func TestOutsideAPaneSaysSo(t *testing.T) {
	f := newFake(t)
	r := f.run(t, "", "open", "4400")
	if r.code != 1 || !strings.Contains(r.errs, "not inside a harnessly pane") {
		t.Errorf("got %+v", r)
	}
}

func TestPanelAndStatus(t *testing.T) {
	f := newFake(t)
	f.run(t, "hs-me", "panel", "show")
	if cmd := f.last().Body["command"].(map[string]any); cmd["kind"] != "show" {
		t.Errorf("panel show: %v", cmd)
	}
	f.run(t, "hs-me", "panel", "hide")
	if cmd := f.last().Body["command"].(map[string]any); cmd["kind"] != "hide" {
		t.Errorf("panel hide: %v", cmd)
	}
	f.run(t, "hs-me", "status")
	if cmd := f.last().Body["command"].(map[string]any); cmd["kind"] != "status" {
		t.Errorf("status: %v", cmd)
	}
	if r := f.run(t, "hs-me", "panel", "flip"); r.code != 2 {
		t.Errorf("panel flip: %+v", r)
	}
}

func TestTabCommandsUseTheOwnWorkspace(t *testing.T) {
	f := newFake(t)
	r := f.run(t, "hs-me", "tab", "list")
	if r.code != 0 || !strings.Contains(r.out, "t-1\tcodex-cli\tcodex\tmain\t(this tab)") || !strings.Contains(r.out, "t-2\tterminal\t-\tsh") {
		t.Fatalf("list: %+v", r)
	}
	r = f.run(t, "hs-me", "tab", "create", "-t", "codex", "-n", "review", "--prompt", "Review it", "--model", "m1")
	if r.code != 0 || !strings.HasPrefix(r.out, "t-9\n") {
		t.Fatalf("create: %+v", r)
	}
	got := f.last()
	if got.Method != "POST" || got.Body["workspaceId"] != "ws-1" || got.Body["panelType"] != "codex-cli" || got.Body["prompt"] != "Review it" || got.Body["model"] != "m1" || got.Body["name"] != "review" {
		t.Errorf("create sent %+v", got)
	}
	if _, has := got.Body["effort"]; has {
		t.Errorf("an unset option is not sent")
	}
	f.run(t, "hs-me", "tab", "send", "t-2", "ls", "-la")
	if got := f.last(); got.Path != "/api/cli/tabs/t-2/send" || got.Query != "workspaceId=ws-1" || got.Body["content"] != "ls -la" {
		t.Errorf("send %+v", got)
	}
	f.run(t, "hs-me", "tab", "send", "t-2", "-")
	if got := f.last(); got.Body["content"] != "piped text" {
		t.Errorf("send from stdin %+v", got)
	}
	r = f.run(t, "hs-me", "tab", "result", "t-2")
	if r.out != "hello\n" {
		t.Errorf("result %q", r.out)
	}
	r = f.run(t, "hs-me", "tab", "status", "t-1")
	if !strings.Contains(r.out, "alive: true") || !strings.Contains(r.out, "state: idle") {
		t.Errorf("status %q", r.out)
	}
	f.run(t, "hs-me", "tab", "close", "t-2")
	if got := f.last(); got.Method != "DELETE" || got.Path != "/api/cli/tabs/t-2" || got.Query != "workspaceId=ws-1" {
		t.Errorf("close %+v", got)
	}
}

func TestWorkspaceFlagAndSingleWorkspaceFallback(t *testing.T) {
	f := newFake(t)
	f.run(t, "", "tab", "list", "-w", "ws-7")
	if got := f.last(); got.Query != "workspaceId=ws-7" {
		t.Errorf("-w: %+v", got)
	}
	// Outside a pane with one workspace, that one is used.
	f.run(t, "", "tab", "result", "t-1")
	if got := f.last(); got.Query != "workspaceId=ws-1" {
		t.Errorf("single workspace: %+v", got)
	}
}

func TestJSONOutputAndWorkspaces(t *testing.T) {
	f := newFake(t)
	r := f.run(t, "", "workspaces")
	if r.out != "ws-1\tWork\t/home/a/work\n" {
		t.Errorf("workspaces: %q", r.out)
	}
	r = f.run(t, "", "workspaces", "--json")
	var v map[string]any
	if err := json.Unmarshal([]byte(r.out), &v); err != nil || v["workspaces"] == nil {
		t.Errorf("--json: %q", r.out)
	}
	r = f.run(t, "hs-me", "tab", "create", "--json", "-t", "codex-cli", "--prompt", "x")
	if err := json.Unmarshal([]byte(r.out), &v); err != nil || v["tabId"] != "t-9" {
		t.Errorf("create --json: %q", r.out)
	}
}

func TestAPIGuide(t *testing.T) {
	f := newFake(t)
	if r := f.run(t, "", "api-guide"); r.out != "# API\n" {
		t.Errorf("api-guide: %q", r.out)
	}
}

func TestRejectedTokenAndMissingRuntime(t *testing.T) {
	f := newFake(t)
	home := t.TempDir()
	r := runWith(t, home, map[string]string{"HARNESSLY_URL": f.srv.URL, "HARNESSLY_CLI_TOKEN": "wrong"}, "hs-me", "workspaces")
	if r.code != 1 || !strings.Contains(r.errs, "rejected the CLI token") {
		t.Errorf("token: %+v", r)
	}
	r = runWith(t, home, nil, "hs-me", "workspaces")
	if r.code != 1 || !strings.Contains(r.errs, "not inside a harnessly workspace") {
		t.Errorf("no port file: %+v", r)
	}
	r = runWith(t, home, map[string]string{"HARNESSLY_URL": "http://127.0.0.1:1"}, "hs-me", "workspaces")
	if r.code != 1 || !strings.Contains(r.errs, "no CLI token") {
		t.Errorf("no token: %+v", r)
	}
	r = runWith(t, home, map[string]string{"HARNESSLY_URL": "http://127.0.0.1:1", "HARNESSLY_CLI_TOKEN": "x"}, "hs-me", "workspaces")
	if r.code != 1 || !strings.Contains(r.errs, "does not answer") {
		t.Errorf("refused: %+v", r)
	}
}

func TestFindsTheRuntimeFromTheDataFolder(t *testing.T) {
	f := newFake(t)
	home := t.TempDir()
	dir := filepath.Join(home, ".harnessly")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	port := f.srv.Listener.Addr().String()[strings.LastIndex(f.srv.Listener.Addr().String(), ":")+1:]
	_ = os.WriteFile(filepath.Join(dir, "port"), []byte(port+"\n"), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "cli-token"), []byte(f.token+"\n"), 0o600)
	r := runWith(t, home, nil, "hs-me", "workspaces")
	if r.code != 0 || !strings.Contains(r.out, "ws-1") {
		t.Errorf("data folder: %+v", r)
	}
	// HARNESSLY_DATA_DIR moves the folder.
	other := t.TempDir()
	_ = os.WriteFile(filepath.Join(other, "port"), []byte(port), 0o600)
	_ = os.WriteFile(filepath.Join(other, "cli-token"), []byte(f.token), 0o600)
	r = runWith(t, t.TempDir(), map[string]string{"HARNESSLY_DATA_DIR": other}, "hs-me", "workspaces")
	if r.code != 0 {
		t.Errorf("HARNESSLY_DATA_DIR: %+v", r)
	}
}

func TestHelp(t *testing.T) {
	r := runWith(t, t.TempDir(), nil, "", "--help")
	if r.code != 0 || !strings.Contains(r.out, "harnessly open") || !strings.Contains(r.out, "tab create") || !strings.Contains(r.out, "api-guide") {
		t.Errorf("help: %+v", r)
	}
	if r := runWith(t, t.TempDir(), nil, "", "open", "--help"); r.code != 0 || !strings.Contains(r.out, "right pane") {
		t.Errorf("open --help: %+v", r)
	}
	if r := runWith(t, t.TempDir(), nil, "", "bogus"); r.code != 2 || !strings.Contains(r.errs, "unknown command") {
		t.Errorf("bogus: %+v", r)
	}
	if r := runWith(t, t.TempDir(), nil, ""); r.code != 0 || r.out != helpText {
		t.Errorf("no args prints the help")
	}
}

func TestOpenDesign(t *testing.T) {
	f := newFake(t)
	cases := []struct {
		args   []string
		target string
	}{
		{[]string{"design", "landing"}, "landing"},
		{[]string{"design", "./designs/landing/project/Main.dc.html"}, "/home/a/work/designs/landing/project/Main.dc.html"},
		{[]string{"design", "Лендинг", "весна"}, "Лендинг весна"},
	}
	for _, c := range cases {
		r := f.run(t, "hs-me", append([]string{"open"}, c.args...)...)
		if r.code != 0 {
			t.Fatalf("%v: exit %d: %s", c.args, r.code, r.errs)
		}
		cmd := f.last().Body["command"].(map[string]any)
		if cmd["tab"] != "design" || cmd["target"] != c.target {
			t.Errorf("%v: sent %+v", c.args, cmd)
		}
	}
}

func TestPeersListAndSend(t *testing.T) {
	f := newFake(t)
	r := f.run(t, "hs-me", "peers")
	if r.code != 0 || !strings.Contains(r.out, "dev-department/tab-rsk\tbusy\tИван · dev-department\tМодель по умолчанию") ||
		!strings.Contains(r.out, "! no-relay") {
		t.Fatalf("list: %+v", r)
	}
	r = f.run(t, "hs-me", "peers", "send", "dev-department/tab-rsk", "готово,", "выкатил")
	if r.code != 0 || !strings.Contains(r.out, "delivered → Иван · dev-department · Модель по умолчанию (dev-department/tab-rsk)") {
		t.Fatalf("send: %+v", r)
	}
	if c := f.last(); c.Body["session"] != "hs-me" || c.Body["text"] != "готово, выкатил" {
		t.Fatalf("the runtime is told the sender's session and the text: %+v", c.Body)
	}
	if r := f.run(t, "hs-me", "peers", "send", "dev-department/tab-rsk", "-"); r.code != 0 || f.last().Body["text"] != "piped text" {
		t.Fatalf("stdin: %+v", r)
	}
	r = f.run(t, "hs-me", "peers", "send", "dev-department/tab-blocked", "x")
	if r.code != 1 || !strings.Contains(r.errs, "refused: blocked — the other side blocked this machine or its owner") {
		t.Fatalf("a refusal says why and exits 1: %+v", r)
	}
	if r := f.run(t, "hs-me", "peers", "send", "only-ref"); r.code != 2 {
		t.Fatalf("usage: %+v", r)
	}
}

func TestNotifyListAndSend(t *testing.T) {
	f := newFake(t)
	r := f.run(t, "hs-me", "notify")
	if r.code != 0 || !strings.HasPrefix(r.out, "Telegram\t@harnessly_bot → Anna\t(send without --bot)\n\tкогда нужен ответ\n") ||
		!strings.Contains(r.out, "nb_1\tАлерты\t@ping_bot → Анна\n\tкогда закончил\n\tили нужен ответ\n") ||
		!strings.Contains(r.out, "nb_2\tFree\t@free_bot (no chat yet)\n\t(no instruction") {
		t.Fatalf("list: %+v", r)
	}
	r = f.run(t, "hs-me", "notify", "send", "--bot", "алерты", "--silent", "готово,", "выкатил")
	if r.code != 0 || r.out != "sent → Анна (Алерты), 2 messages\n" {
		t.Fatalf("send: %+v", r)
	}
	if c := f.last(); c.Path != "/api/cli/notify/send" || c.Body["bot"] != "алерты" || c.Body["text"] != "готово, выкатил" ||
		c.Body["silent"] != true || c.Body["session"] != "hs-me" {
		t.Fatalf("body: %+v", c.Body)
	}
	if r := f.run(t, "hs-me", "notify", "send", "--bot=Алерты", "-"); r.code != 0 || f.last().Body["text"] != "piped text" || f.last().Body["silent"] != false {
		t.Fatalf("stdin: %+v %+v", r, f.last().Body)
	}
	r = f.run(t, "hs-me", "notify", "send", "--bot", "nobody", "x")
	if r.code != 1 || !strings.Contains(r.errs, "refused: not-found — there is no bot by that name") {
		t.Fatalf("refusal: %+v", r)
	}
	if r := f.run(t, "hs-me", "notify", "send", "text only"); r.code != 0 || r.out != "sent → Anna\n" || f.last().Body["bot"] != "" {
		t.Fatalf("no bot is the shared one: %+v %+v", r, f.last().Body)
	}
	if r := f.run(t, "hs-me", "notify", "send", "--bot", "x"); r.code != 2 {
		t.Fatalf("no text is usage: %+v", r)
	}
}

func TestDeliverSendsTheAbsolutePathAndTheTitle(t *testing.T) {
	f := newFake(t)
	r := f.run(t, "hs-me", "deliver", "notes/todo.md", "--title", "Plan")
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.errs)
	}
	got := f.last()
	if got.Path != "/api/cli/deliver" || got.Method != "POST" || got.Body["session"] != "hs-me" || got.Body["path"] != "/home/a/work/notes/todo.md" || got.Body["title"] != "Plan" {
		t.Fatalf("sent %+v", got)
	}
	if !strings.Contains(r.out, "Delivered notes.md") || !strings.Contains(r.out, "delivered.harnessly.invalid") {
		t.Fatalf("output %q", r.out)
	}
	if r := f.run(t, "hs-me", "deliver"); r.code != 2 {
		t.Fatalf("no path: exit %d", r.code)
	}
	if r := f.run(t, "", "deliver", "x.zip"); r.code == 0 {
		t.Fatal("outside a pane there is nobody to hand a file to")
	}
}
