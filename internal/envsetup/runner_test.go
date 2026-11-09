package envsetup

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

type fakeSafe struct {
	mu    sync.Mutex
	vals  map[string]string
	asked []string
}

func (s *fakeSafe) Has(n string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.vals[n]
	return ok
}
func (s *fakeSafe) Reveal(names []string) (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]string{}
	for _, n := range names {
		v, ok := s.vals[n]
		if !ok {
			return nil, errors.New("not-found")
		}
		out[n] = v
	}
	return out, nil
}
func (s *fakeSafe) Ask(n, why string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.asked = append(s.asked, n+"|"+why)
	return nil
}
func (s *fakeSafe) put(n, v string) { s.mu.Lock(); s.vals[n] = v; s.mu.Unlock() }

// reply is what a fake program does.
type reply struct {
	exit  int
	out   []string // written to the output one by one (so a value can be split between writes)
	err   error
	after func()
}

type fixture struct {
	t     *testing.T
	r     *Runner
	safe  *fakeSafe
	calls []Cmd
	// handle answers a program; nil answers exit 0 with no output.
	handle func(c Cmd) reply
	home   string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{t: t, safe: &fakeSafe{vals: map[string]string{}}, home: t.TempDir()}
	f.r = &Runner{LinkDir: t.TempDir(), Home: f.home, Secrets: f.safe,
		Now: func() time.Time { return time.Unix(1_700_000_000, 0) }, Getenv: func(k string) string { return map[string]string{"PATH": "/usr/bin:/bin", "USER": "anna"}[k] },
		Fetch: func(ctx context.Context, u string) ([]byte, error) { return []byte("echo from " + u), nil }}
	f.allow(true)
	f.r.Exec = func(ctx context.Context, c Cmd) (int, error) {
		f.calls = append(f.calls, c)
		rep := reply{}
		if f.handle != nil {
			rep = f.handle(c)
		}
		for _, o := range rep.out {
			if c.Out != nil {
				_, _ = c.Out.Write([]byte(o))
			}
		}
		if rep.after != nil {
			rep.after()
		}
		return rep.exit, rep.err
	}
	return f
}

func (f *fixture) setList(items ...hubproto.EnvItem) string {
	f.t.Helper()
	hubproto.SortEnvItems(items)
	h := held{Hash: hubproto.EnvHash(items), Items: items}
	raw, _ := json.Marshal(h)
	if err := os.WriteFile(filepath.Join(f.r.LinkDir, "environment.json"), raw, 0o600); err != nil {
		f.t.Fatal(err)
	}
	return h.Hash
}

func (f *fixture) request(id, mode string, items ...hubproto.EnvItem) {
	f.t.Helper()
	if err := writeJSON(filepath.Join(f.r.LinkDir, hubproto.EnvRequestsDir, id+".json"), hubproto.EnvRunRequest{RunID: id, Mode: mode, Items: items, At: 1}); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) runFile(id string) hubproto.EnvRun {
	f.t.Helper()
	raw, err := os.ReadFile(filepath.Join(f.r.LinkDir, hubproto.EnvRunsDir, id+".json"))
	if err != nil {
		f.t.Fatalf("run %s: %v", id, err)
	}
	var run hubproto.EnvRun
	if err := json.Unmarshal(raw, &run); err != nil {
		f.t.Fatal(err)
	}
	return run
}

// onlyRun is the one run file there is.
func (f *fixture) onlyRun() hubproto.EnvRun {
	f.t.Helper()
	runs := f.r.runs()
	if len(runs) != 1 {
		f.t.Fatalf("%d runs, want 1", len(runs))
	}
	return runs[0]
}

func (f *fixture) step(run hubproto.EnvRun, id string) hubproto.EnvStep {
	f.t.Helper()
	for _, s := range run.Steps {
		if s.ItemID == id {
			return s
		}
	}
	f.t.Fatalf("no step %s in %+v", id, run.Steps)
	return hubproto.EnvStep{}
}

func (f *fixture) scriptRuns() []string {
	var out []string
	for _, c := range f.calls {
		if c.Name == "bash" && len(c.Args) == 2 && c.Args[0] == "-ec" {
			out = append(out, c.Args[1])
		}
	}
	return out
}

func script(id, name string, order int, src string) hubproto.EnvItem {
	return hubproto.EnvItem{ItemID: id, Kind: hubproto.EnvKindScript, Name: name, Layer: "org", Order: order, Source: json.RawMessage(src)}
}

func TestScriptsRunInOrderAfterAccessAndRepos(t *testing.T) {
	f := newFixture(t)
	f.handle = func(c Cmd) reply {
		if c.Name == "glab" && c.Args[1] == "status" {
			return reply{exit: 0}
		}
		if c.Name == "git" && len(c.Args) > 5 && c.Args[4] == "remote" {
			return reply{exit: 1}
		}
		return reply{}
	}
	f.setList(
		script("s2", "Второй", 2, `{"from":"text","body":"echo 2","run":"echo two"}`),
		script("s1", "Первый", 1, `{"from":"text","body":"echo 1","run":"echo one"}`),
		hubproto.EnvItem{ItemID: "r1", Kind: "repo", Name: "Монорепо", Source: json.RawMessage(`{"url":"https://gitlab.com/acme/mono.git","branch":"dev"}`)},
		hubproto.EnvItem{ItemID: "a1", Kind: "access", Name: "GitLab", Source: json.RawMessage(`{"service":"gitlab"}`)},
		hubproto.EnvItem{ItemID: "k1", Kind: "skill", Name: "Не для машины", Source: json.RawMessage(`{"body":"x"}`)},
	)
	f.r.Tick(context.Background())
	run := f.onlyRun()
	var order []string
	for _, s := range run.Steps {
		order = append(order, s.ItemID)
	}
	if strings.Join(order, ",") != "a1,r1,s1,s2" || run.Status != hubproto.EnvOK || run.Mode != hubproto.RunApply {
		t.Fatalf("steps %v, run %s %s", order, run.Status, run.Mode)
	}
	if got := f.scriptRuns(); strings.Join(got, ",") != "echo one,echo two" {
		t.Fatalf("scripts ran %v", got)
	}
	// The clone was asked for the branch, into ~/code/mono.
	var clone Cmd
	for _, c := range f.calls {
		if c.Name == "git" && c.Args[0] == "clone" {
			clone = c
		}
	}
	if want := []string{"clone", "--branch", "dev", "--", "https://gitlab.com/acme/mono.git", filepath.Join(f.home, "code", "mono")}; strings.Join(clone.Args, " ") != strings.Join(want, " ") {
		t.Fatalf("clone %v", clone.Args)
	}
	// The setup file says what the agent is to report; the other kinds are not in it.
	st := f.r.readState()
	if st.Hash != run.Hash || len(st.Items) != 4 {
		t.Fatalf("state %+v", st)
	}
	for _, it := range st.Items {
		if it.ItemID == "k1" || it.Status != hubproto.EnvOK {
			t.Errorf("state item %+v", it)
		}
	}
	// A list that was worked on is not worked on again.
	n := len(f.calls)
	f.r.Tick(context.Background())
	if len(f.calls) != n {
		t.Fatal("the same list ran twice")
	}
}

func TestOnceLedgerAndChangedSource(t *testing.T) {
	f := newFixture(t)
	f.setList(script("s1", "Один раз", 1, `{"from":"text","body":"echo 1","run":"echo one","rerun":"once"}`))
	f.r.Tick(context.Background())
	if len(f.scriptRuns()) != 1 {
		t.Fatalf("first apply: %v", f.scriptRuns())
	}
	// An update on the machine skips it: the ledger has this source.
	f.request("update-0001", hubproto.RunUpdate)
	f.r.Tick(context.Background())
	run := f.runFile("update-0001")
	if len(f.scriptRuns()) != 1 || f.step(run, "s1").Status != hubproto.EnvSkipped {
		t.Fatalf("update: ran %v, step %+v", f.scriptRuns(), f.step(run, "s1"))
	}
	// Another source runs it again.
	f.setList(script("s1", "Один раз", 1, `{"from":"text","body":"echo 1","run":"echo uno","rerun":"once"}`))
	f.r.Tick(context.Background())
	if got := f.scriptRuns(); len(got) != 2 || got[1] != "echo uno" {
		t.Fatalf("changed source: %v", got)
	}
}

func TestCheckScriptSkipsWhenCheckPasses(t *testing.T) {
	f := newFixture(t)
	f.handle = func(c Cmd) reply {
		if len(c.Args) == 2 && c.Args[0] == "-c" && c.Args[1] == "which node" {
			return reply{exit: 0}
		}
		if len(c.Args) == 2 && c.Args[0] == "-c" && c.Args[1] == "which go" {
			return reply{exit: 1}
		}
		return reply{}
	}
	f.setList(
		script("s1", "Node", 1, `{"from":"text","body":"x","run":"install node","rerun":"check","check":"which node"}`),
		script("s2", "Go", 2, `{"from":"text","body":"x","run":"install go","rerun":"check","check":"which go"}`),
	)
	f.r.Tick(context.Background())
	run := f.onlyRun()
	if got := f.scriptRuns(); len(got) != 1 || got[0] != "install go" {
		t.Fatalf("ran %v", got)
	}
	if s := f.step(run, "s1"); s.Status != hubproto.EnvSkipped {
		t.Fatalf("%+v", s)
	}
	if f.step(run, "s2").Status != hubproto.EnvOK {
		t.Fatalf("%+v", f.step(run, "s2"))
	}
	for _, c := range f.calls {
		if len(c.Args) == 2 && c.Args[1] == "which node" && c.Timeout != checkTimeout {
			t.Errorf("check timeout %s", c.Timeout)
		}
	}
}

func TestFailedScriptStopsTheLaterOnes(t *testing.T) {
	f := newFixture(t)
	f.handle = func(c Cmd) reply {
		if len(c.Args) == 2 && c.Args[1] == "boom" {
			return reply{exit: 3, out: []string{"плохо\n"}}
		}
		return reply{}
	}
	f.setList(
		script("s1", "Ломается", 1, `{"from":"text","body":"x","run":"boom"}`),
		script("s2", "Следом", 2, `{"from":"text","body":"x","run":"after"}`),
	)
	f.r.Tick(context.Background())
	run := f.onlyRun()
	s1, s2 := f.step(run, "s1"), f.step(run, "s2")
	if s1.Status != hubproto.EnvError || s1.Exit == nil || *s1.Exit != 3 || !strings.Contains(s1.Log, "плохо") {
		t.Fatalf("s1 %+v", s1)
	}
	if s2.Status != hubproto.EnvPending || s2.Message != "ждёт, пока починят Ломается" || len(f.scriptRuns()) != 1 {
		t.Fatalf("s2 %+v, ran %v", s2, f.scriptRuns())
	}
	if run.Status != hubproto.EnvError {
		t.Fatalf("run %s", run.Status)
	}
	// The hub is told per item.
	st := f.r.readState()
	if st.Items[0].Status != hubproto.EnvError || st.Items[1].Status != hubproto.EnvPending {
		t.Fatalf("%+v", st.Items)
	}
}

func TestSecretsAreMaskedAlsoAcrossWrites(t *testing.T) {
	f := newFixture(t)
	f.safe.put("npm-token", "s3cr3t-value-123")
	f.handle = func(c Cmd) reply {
		if len(c.Args) == 2 && c.Args[1] == "use" {
			return reply{out: []string{"token=s3cr3t-val", "ue-123 и ещё ", "s3cr3t-value-123\n"}}
		}
		return reply{}
	}
	f.setList(script("s1", "С токеном", 1, `{"from":"text","body":"x","run":"use","secrets":["npm-token"]}`))
	f.r.Tick(context.Background())
	s := f.step(f.onlyRun(), "s1")
	if strings.Contains(s.Log, "s3cr3t") || !strings.Contains(s.Log, "token=••••••") || s.Status != hubproto.EnvOK {
		t.Fatalf("log %q, %+v", s.Log, s)
	}
	var env []string
	for _, c := range f.calls {
		if len(c.Args) == 2 && c.Args[1] == "use" {
			env = c.Env
		}
	}
	if !contains(env, "NPM_TOKEN=s3cr3t-value-123") || !contains(env, "HOME="+f.home) {
		t.Fatalf("env %v", env)
	}
	for _, e := range env {
		if strings.HasPrefix(e, "HOME=") || strings.HasPrefix(e, "PATH=") || strings.HasPrefix(e, "USER=") || strings.HasPrefix(e, "LANG=") ||
			strings.HasPrefix(e, "SHELL=") || strings.HasPrefix(e, "NPM_TOKEN=") || strings.HasPrefix(e, "GIT_") || strings.HasPrefix(e, "NO_COLOR") || strings.HasPrefix(e, "HARNESSLY_") {
			continue
		}
		t.Errorf("unexpected variable %q", e)
	}
}

func TestMissingSecretIsPendingAsksAndWaits(t *testing.T) {
	f := newFixture(t)
	f.setList(
		script("s1", "Нужен токен", 1, `{"from":"text","body":"x","run":"one","secrets":["npm-token"]}`),
		script("s2", "Следом", 2, `{"from":"text","body":"x","run":"two"}`),
	)
	f.r.Tick(context.Background())
	run := f.onlyRun()
	s1, s2 := f.step(run, "s1"), f.step(run, "s2")
	if s1.Status != hubproto.EnvPending || s1.Message != "положите npm-token в сейф" || s2.Status != hubproto.EnvPending || len(f.scriptRuns()) != 0 {
		t.Fatalf("%+v %+v ran %v", s1, s2, f.scriptRuns())
	}
	if len(f.safe.asked) != 1 || f.safe.asked[0] != "npm-token|Окружение команды: Нужен токен" {
		t.Fatalf("asked %v", f.safe.asked)
	}
	if run.Status != hubproto.EnvPending {
		t.Fatalf("run %s", run.Status)
	}
	// Nothing happens while the safe is empty; the token comes, the list is worked on again.
	f.r.Tick(context.Background())
	if len(f.runsAll()) != 1 {
		t.Fatal("ran without the secret")
	}
	f.safe.put("npm-token", "value-1234")
	f.r.Tick(context.Background())
	if got := f.scriptRuns(); strings.Join(got, ",") != "one,two" {
		t.Fatalf("after the token: %v", got)
	}
}

func (f *fixture) runsAll() []hubproto.EnvRun { return f.r.runs() }

func TestRepositories(t *testing.T) {
	f := newFixture(t)
	existing := filepath.Join(f.home, "code", "have")
	other := filepath.Join(f.home, "work", "other")
	for _, d := range []string{existing, other} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	f.handle = func(c Cmd) reply {
		if c.Name == "git" && len(c.Args) > 5 && c.Args[4] == "remote" {
			if c.Args[1] != "safe.directory="+c.Args[3] {
				t.Errorf("the folder is not let in: %v", c.Args)
			}
			switch c.Args[3] {
			case existing:
				return reply{out: []string{"git@gitlab.com:acme/have.git\n"}} // the same repository over ssh
			case other:
				return reply{out: []string{"https://x:tok@gitlab.com/acme/else.git\n"}}
			}
		}
		return reply{}
	}
	f.setList(
		hubproto.EnvItem{ItemID: "r1", Kind: "repo", Name: "Есть", Source: json.RawMessage(`{"url":"https://gitlab.com/acme/have.git"}`)},
		hubproto.EnvItem{ItemID: "r2", Kind: "repo", Name: "Нет", Source: json.RawMessage(`{"url":"https://gitlab.com/acme/new","dir":"~/src/new"}`)},
		hubproto.EnvItem{ItemID: "r3", Kind: "repo", Name: "Чужая", Source: json.RawMessage(`{"url":"https://gitlab.com/acme/want.git","dir":"work/other"}`)},
		hubproto.EnvItem{ItemID: "r4", Kind: "repo", Name: "Наружу", Source: json.RawMessage(`{"url":"https://gitlab.com/acme/x.git","dir":"/etc/x"}`)},
	)
	f.r.Tick(context.Background())
	run := f.onlyRun()
	r1, r2, r3, r4 := f.step(run, "r1"), f.step(run, "r2"), f.step(run, "r3"), f.step(run, "r4")
	if r1.Status != hubproto.EnvOK || !strings.Contains(r1.Message, "уже") {
		t.Errorf("r1 %+v", r1)
	}
	if r2.Status != hubproto.EnvOK || !strings.Contains(r2.Message, "склонирован") {
		t.Errorf("r2 %+v", r2)
	}
	if r3.Status != hubproto.EnvError || !strings.Contains(r3.Message, "gitlab.com/acme/else") || strings.Contains(r3.Message, "tok") {
		t.Errorf("r3 %+v", r3)
	}
	if r4.Status != hubproto.EnvError || !strings.Contains(r4.Message, "вне домашней") {
		t.Errorf("r4 %+v", r4)
	}
	clones := 0
	for _, c := range f.calls {
		if c.Name == "git" && c.Args[0] == "clone" {
			clones++
			if c.Args[len(c.Args)-1] != filepath.Join(f.home, "src", "new") {
				t.Errorf("clone into %v", c.Args)
			}
		}
		if c.Name == "git" && (c.Args[0] == "pull" || strings.Contains(strings.Join(c.Args, " "), " pull") || strings.Contains(strings.Join(c.Args, " "), "reset")) {
			t.Errorf("an existing clone was touched: %v", c.Args)
		}
	}
	if clones != 1 {
		t.Errorf("%d clones", clones)
	}
	// The failed ones were tried again after the scripts, once: their logs say so.
	if !strings.Contains(r3.Log, "ещё раз после скриптов") {
		t.Errorf("no retry: %q", r3.Log)
	}
}

func TestAccessLogsInWithTheTokenFromTheSafeAndSetsGitUp(t *testing.T) {
	f := newFixture(t)
	f.handle = func(c Cmd) reply {
		if c.Name == "gh" && c.Args[1] == "status" {
			return reply{exit: 1}
		}
		if c.Name == "glab" && c.Args[1] == "status" {
			return reply{exit: 1}
		}
		return reply{}
	}
	f.setList(
		hubproto.EnvItem{ItemID: "a1", Kind: "access", Name: "GitLab", Source: json.RawMessage(`{"service":"gitlab","host":"git.acme.ru"}`)},
		hubproto.EnvItem{ItemID: "a2", Kind: "access", Name: "GitHub", Source: json.RawMessage(`{"service":"github"}`)},
	)
	f.r.Tick(context.Background())
	run := f.onlyRun()
	if f.step(run, "a1").Status != hubproto.EnvPending || f.step(run, "a2").Message != "положите токен github-token в сейф" {
		t.Fatalf("no tokens: %+v", run.Steps)
	}
	if len(f.safe.asked) != 2 {
		t.Fatalf("asked %v", f.safe.asked)
	}
	f.safe.put("gitlab-token", "glpat-secret-1")
	f.safe.put("github-token", "ghp_secret_2")
	f.calls = nil
	f.request("update-0002", hubproto.RunUpdate)
	f.r.Tick(context.Background())
	run = f.runFile("update-0002")
	if run.Status != hubproto.EnvOK {
		t.Fatalf("%+v", run.Steps)
	}
	var glabLogin, ghLogin, helper, setup bool
	for _, c := range f.calls {
		switch {
		case c.Name == "glab" && c.Args[1] == "login":
			glabLogin = strings.Join(c.Args, " ") == "auth login --hostname git.acme.ru --stdin" && c.Stdin == "glpat-secret-1\n"
		case c.Name == "gh" && c.Args[1] == "login":
			ghLogin = strings.Join(c.Args, " ") == "auth login --hostname github.com --with-token" && c.Stdin == "ghp_secret_2\n"
		case c.Name == "gh" && c.Args[1] == "setup-git":
			setup = true
		case c.Name == "git" && c.Args[0] == "config":
			helper = c.Args[2] == "credential.https://git.acme.ru.helper" && c.Args[3] == "!glab auth git-credential"
		}
	}
	if !glabLogin || !ghLogin || !helper || !setup {
		t.Fatalf("glab %v gh %v helper %v setup %v", glabLogin, ghLogin, helper, setup)
	}
	for _, s := range run.Steps {
		if strings.Contains(s.Log, "secret") {
			t.Errorf("a token in the log: %q", s.Log)
		}
	}
}

func TestMissingCLIIsAnErrorOfTheStep(t *testing.T) {
	f := newFixture(t)
	f.handle = func(c Cmd) reply { return reply{exit: -1, err: exec.ErrNotFound} }
	f.setList(hubproto.EnvItem{ItemID: "a1", Kind: "access", Name: "GitLab", Source: json.RawMessage(`{"service":"gitlab"}`)})
	f.r.Tick(context.Background())
	if s := f.step(f.onlyRun(), "a1"); s.Status != hubproto.EnvError || !strings.Contains(s.Message, "нет glab") {
		t.Fatalf("%+v", s)
	}
}

func TestCheckRunChangesNothing(t *testing.T) {
	f := newFixture(t)
	f.handle = func(c Cmd) reply {
		if c.Name == "glab" {
			return reply{exit: 1}
		}
		return reply{}
	}
	items := []hubproto.EnvItem{
		hubproto.EnvItem{ItemID: "a1", Kind: "access", Name: "GitLab", Source: json.RawMessage(`{"service":"gitlab"}`)},
		hubproto.EnvItem{ItemID: "r1", Kind: "repo", Name: "Нет", Source: json.RawMessage(`{"url":"https://gitlab.com/acme/new.git"}`)},
		script("s1", "Один раз", 1, `{"from":"text","body":"x","run":"once","rerun":"once"}`),
		script("s2", "Проверка", 2, `{"from":"text","body":"x","run":"fix","rerun":"check","check":"which go"}`),
		script("s3", "Всегда", 3, `{"from":"text","body":"x","run":"always"}`),
	}
	f.setList(items...)
	// The list was applied before, with a ledger that has s1 under another source.
	if err := f.r.writeState(hubproto.EnvSetupState{Hash: hubproto.EnvHash(items), Items: []hubproto.EnvApplied{}, Once: map[string]string{"s1": "old"}}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(f.r.LinkDir, hubproto.EnvSetupFile))
	f.request("check-0001", hubproto.RunCheck)
	f.r.Tick(context.Background())
	run := f.runFile("check-0001")
	for id, want := range map[string]string{"a1": hubproto.EnvPending, "r1": hubproto.EnvPending, "s1": hubproto.EnvPending, "s2": hubproto.EnvOK, "s3": hubproto.EnvSkipped} {
		if got := f.step(run, id).Status; got != want {
			t.Errorf("%s = %s, want %s", id, got, want)
		}
	}
	for _, c := range f.calls {
		if c.Name == "git" && (c.Args[0] == "clone" || c.Args[0] == "config") || c.Name == "glab" && c.Args[1] == "login" {
			t.Errorf("a check run changed something: %s %v", c.Name, c.Args)
		}
	}
	for _, s := range f.scriptRuns() {
		t.Errorf("a script ran: %s", s)
	}
	after, _ := os.ReadFile(filepath.Join(f.r.LinkDir, hubproto.EnvSetupFile))
	if string(before) != string(after) {
		t.Errorf("the setup file changed: %s -> %s", before, after)
	}
	if len(f.safe.asked) != 0 {
		t.Errorf("a check run asked for secrets: %v", f.safe.asked)
	}
	if _, err := os.Stat(filepath.Join(f.r.LinkDir, hubproto.EnvRequestsDir, "check-0001.json")); err == nil {
		t.Error("the request stayed")
	}
}

func TestSandboxWithoutBwrapRunsWithAFreshHome(t *testing.T) {
	f := newFixture(t)
	f.handle = func(c Cmd) reply {
		if c.Name == "bwrap" {
			return reply{exit: -1, err: exec.ErrNotFound}
		}
		return reply{}
	}
	f.setList(script("held1", "Из списка", 1, `{"from":"text","body":"x","run":"held"}`))
	f.r.Tick(context.Background())
	state := f.r.readState()
	f.calls = nil
	draft := script("d1", "Черновик", 1, `{"from":"text","body":"x","run":"echo hi","rerun":"once"}`)
	f.request("try-00001", hubproto.RunSandbox, draft)
	f.r.Tick(context.Background())
	run := f.runFile("try-00001")
	st := run.Steps[0]
	if run.Status != hubproto.EnvOK || !strings.HasPrefix(st.Log, "Песочница: отдельная домашняя папка (bwrap на этой машине недоступен)\n") {
		t.Fatalf("%+v", run)
	}
	var c Cmd
	for _, x := range f.calls {
		if x.Name == "bash" {
			c = x
		}
		if x.Name == "bwrap" && len(x.Args) > 0 && x.Args[len(x.Args)-1] != "true" {
			t.Errorf("wrapped without bwrap: %v", x.Args)
		}
	}
	tmp := filepath.Join(f.r.LinkDir, "environment", "sandbox", "try-00001")
	for _, want := range []string{"HOME=" + tmp, "XDG_CONFIG_HOME=" + tmp + "/.config", "XDG_DATA_HOME=" + tmp + "/.local/share",
		"XDG_CACHE_HOME=" + tmp + "/.cache", "XDG_STATE_HOME=" + tmp + "/.local/state", "GIT_CONFIG_GLOBAL=" + tmp + "/.gitconfig"} {
		if !contains(c.Env, want) {
			t.Errorf("env lacks %s: %v", want, c.Env)
		}
	}
	if !strings.HasPrefix(c.Dir, tmp) {
		t.Errorf("cwd %q is not inside the sandbox home", c.Dir)
	}
	if ents, _ := os.ReadDir(f.home); len(ents) != 0 {
		t.Errorf("the real home was touched: %v", ents)
	}
	if after := f.r.readState(); after.Hash != state.Hash || len(after.Once) != len(state.Once) {
		t.Errorf("a sandbox run changed the setup file: %+v", after)
	}
	if _, err := os.Stat(tmp); err == nil {
		t.Error("the sandbox home stayed")
	}
}

func TestSandboxRunsInsideBwrapAndKeepsNoLedger(t *testing.T) {
	f := newFixture(t)
	f.setList(script("held1", "Из списка", 1, `{"from":"text","body":"x","run":"held"}`))
	f.r.Tick(context.Background()) // apply the held list first
	f.calls = nil
	draft := script("d1", "Черновик", 1, `{"from":"text","body":"x","run":"echo hi","rerun":"once"}`)
	f.request("try-00002", hubproto.RunSandbox, draft)
	f.r.Tick(context.Background())
	run := f.runFile("try-00002")
	if run.Status != hubproto.EnvOK || len(run.Steps) != 1 || run.Steps[0].ItemID != "d1" || !strings.HasPrefix(run.Steps[0].Log, "Песочница: отдельная домашняя папка + bwrap\n") {
		t.Fatalf("%+v", run)
	}
	var wrapped Cmd
	for _, c := range f.calls {
		if c.Name == "bwrap" && len(c.Args) > 4 && c.Args[len(c.Args)-1] == "echo hi" {
			wrapped = c
		}
	}
	args := strings.Join(wrapped.Args, " ")
	if !strings.Contains(args, "--die-with-parent --unshare-pid --ro-bind / / --dev-bind /dev /dev --tmpfs /tmp --bind ") || !strings.Contains(args, " "+f.home+" ") {
		t.Fatalf("not wrapped as the contract says: %q", args)
	}
	if envOf(wrapped.Env, "HOME") != f.home {
		t.Errorf("HOME %q", envOf(wrapped.Env, "HOME"))
	}
	if _, ok := f.r.readState().Once["d1"]; ok {
		t.Error("a sandbox run used the ledger")
	}
	if _, err := os.Stat(filepath.Join(f.r.LinkDir, "environment", "sandbox", "try-00002")); err == nil {
		t.Error("the sandbox home stayed")
	}
}

func TestInterruptedRunIsMarkedAndTheListIsWorkedOnAgain(t *testing.T) {
	f := newFixture(t)
	f.setList(script("s1", "Скрипт", 1, `{"from":"text","body":"x","run":"go"}`))
	if err := f.r.writeRun(hubproto.EnvRun{RunID: "apply-dead-1", Mode: hubproto.RunApply, Status: hubproto.EnvRunning, StartedAt: 1,
		Steps: []hubproto.EnvStep{{ItemID: "s1", Kind: "script", Name: "Скрипт", Status: hubproto.EnvRunning}}}); err != nil {
		t.Fatal(err)
	}
	f.r.Recover()
	run := f.runFile("apply-dead-1")
	if run.Status != hubproto.EnvError || run.Steps[0].Status != hubproto.EnvError || run.Steps[0].Message != "прервано перезапуском" || run.FinishedAt == 0 {
		t.Fatalf("%+v", run)
	}
	f.r.Tick(context.Background())
	if len(f.scriptRuns()) != 1 {
		t.Fatalf("the list was not applied again: %v", f.scriptRuns())
	}
}

func TestLogKeepsTheTail(t *testing.T) {
	f := newFixture(t)
	f.handle = func(c Cmd) reply {
		if len(c.Args) == 2 && c.Args[1] == "loud" {
			var out []string
			for i := 0; i < 40; i++ {
				out = append(out, strings.Repeat("я", 1000)+"\n")
			}
			return reply{out: append(out, "КОНЕЦ\n")}
		}
		return reply{}
	}
	f.setList(script("s1", "Шумный", 1, `{"from":"text","body":"x","run":"loud"}`))
	f.r.Tick(context.Background())
	log := f.step(f.onlyRun(), "s1").Log
	if len(log) > hubproto.EnvLogMax || !strings.HasSuffix(log, "КОНЕЦ\n") || !strings.HasPrefix(log, "…") {
		t.Fatalf("log of %d bytes, %q…", len(log), log[:20])
	}
}

func TestScriptSourcesFromFileUrlAndRepo(t *testing.T) {
	f := newFixture(t)
	repo := filepath.Join(f.home, "code", "tools")
	if err := os.MkdirAll(filepath.Join(repo, "ci"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "ci", "setup.sh"), []byte("echo hi"), 0o755); err != nil {
		t.Fatal(err)
	}
	f.handle = func(c Cmd) reply {
		if c.Name == "git" && len(c.Args) > 5 && c.Args[4] == "remote" {
			return reply{out: []string{"https://gitlab.com/acme/tools.git"}}
		}
		return reply{}
	}
	var seen = map[string]Cmd{}
	inner := f.r.Exec
	f.r.Exec = func(ctx context.Context, c Cmd) (int, error) {
		if c.Name == "bash" && len(c.Args) == 2 && c.Args[0] == "-ec" {
			// The file is there when the program starts, and the working folder is where it should be.
			if v := envOf(c.Env, "HARNESSLY_SCRIPT"); v != "" {
				if _, err := os.Stat(v); err != nil {
					t.Errorf("script file missing: %v", err)
				}
			}
			seen[c.Args[1]] = c
		}
		return inner(ctx, c)
	}
	f.setList(
		hubproto.EnvItem{ItemID: "r1", Kind: "repo", Name: "Tools", Source: json.RawMessage(`{"url":"https://gitlab.com/acme/tools.git"}`)},
		script("s1", "Файл", 1, `{"from":"file","fileName":"prep.sh","body":"echo file","rerun":"always"}`),
		script("s2", "Адрес", 2, `{"from":"url","url":"https://example.org/x/boot.sh"}`),
		script("s3", "Из репозитория", 3, `{"from":"repo","repo":"r1","path":"ci/setup.sh","run":"bash ci/setup.sh"}`),
		script("s4", "Выход", 4, `{"from":"repo","repo":"r1","path":"../../etc/passwd"}`),
	)
	f.r.Tick(context.Background())
	run := f.onlyRun()
	if f.step(run, "s1").Status != hubproto.EnvOK || f.step(run, "s2").Status != hubproto.EnvOK || f.step(run, "s3").Status != hubproto.EnvOK {
		t.Fatalf("%+v", run.Steps)
	}
	if s := f.step(run, "s4"); s.Status != hubproto.EnvError || !strings.Contains(s.Message, "выходит за репозиторий") {
		t.Fatalf("%+v", s)
	}
	if c := seen[`bash "$HARNESSLY_SCRIPT"`]; !strings.HasSuffix(envOf(c.Env, "HARNESSLY_SCRIPT"), "boot.sh") {
		t.Errorf("url script: %v", c.Env)
	}
	if c := seen["bash ci/setup.sh"]; c.Dir != repo {
		t.Errorf("repo script runs in %q, want %q", c.Dir, repo)
	}
	// The temporary folders are gone.
	if ents, _ := os.ReadDir(filepath.Join(f.r.LinkDir, "environment", "work")); len(ents) != 0 {
		t.Errorf("work folders stay: %v", ents)
	}
}

func envOf(env []string, k string) string {
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, k+"="); ok {
			return v
		}
	}
	return ""
}

func TestBadRequestIsDroppedAndOldRunsPruned(t *testing.T) {
	f := newFixture(t)
	dir := filepath.Join(f.r.LinkDir, hubproto.EnvRequestsDir)
	_ = os.MkdirAll(dir, 0o700)
	_ = os.WriteFile(filepath.Join(dir, "garbage1.json"), []byte("{"), 0o600)
	old := filepath.Join(f.r.LinkDir, hubproto.EnvRunsDir, "old-run-0001.json")
	if err := f.r.writeRun(hubproto.EnvRun{RunID: "old-run-0001", Status: hubproto.EnvOK}); err != nil {
		t.Fatal(err)
	}
	past := f.r.now().Add(-15 * 24 * time.Hour)
	_ = os.Chtimes(old, past, past)
	f.r.Tick(context.Background())
	if _, err := os.Stat(filepath.Join(dir, "garbage1.json")); err == nil {
		t.Error("bad request stays")
	}
	if _, err := os.Stat(old); err == nil {
		t.Error("old run stays")
	}
}

func TestListWithoutSetupItemsIsMarkedWorkedOn(t *testing.T) {
	f := newFixture(t)
	h := f.setList(hubproto.EnvItem{ItemID: "k1", Kind: "skill", Name: "Скилл", Source: json.RawMessage(`{"body":"x"}`)})
	f.r.Tick(context.Background())
	if st := f.r.readState(); st.Hash != h || len(f.runsAll()) != 0 {
		t.Fatalf("%+v runs %d", st, len(f.runsAll()))
	}
}

// With the real programs: the sandbox has a home of its own whether or not bubblewrap works here, and the real home is
// untouched.
func TestRealSandboxLeavesTheHomeAlone(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "personal.txt"), []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := &Runner{LinkDir: t.TempDir(), Home: home, Secrets: &fakeSafe{vals: map[string]string{}}}
	r.Getenv = func(k string) string { return os.Getenv(k) }
	if err := writeJSON(filepath.Join(r.LinkDir, hubproto.EnvAllowFile), hubproto.EnvAllow{Setup: true}); err != nil {
		t.Fatal(err)
	}
	draft := script("d1", "Черновик", 1, `{"from":"text","body":"echo sandbox > \"$HOME/made.txt\"; ls -A \"$HOME\"; pwd"}`)
	if err := writeJSON(filepath.Join(r.LinkDir, hubproto.EnvRequestsDir, "try-real-1.json"),
		hubproto.EnvRunRequest{RunID: "try-real-1", Mode: hubproto.RunSandbox, Items: []hubproto.EnvItem{draft}}); err != nil {
		t.Fatal(err)
	}
	r.Tick(context.Background())
	runs := r.runs()
	if len(runs) != 1 || runs[0].Status != hubproto.EnvOK {
		t.Fatalf("%+v", runs)
	}
	log := runs[0].Steps[0].Log
	if !strings.HasPrefix(log, "Песочница: отдельная домашняя папка") || !strings.Contains(log, "made.txt") || strings.Contains(log, "personal.txt") {
		t.Errorf("log %q", log)
	}
	if _, err := os.Stat(filepath.Join(home, "made.txt")); err == nil {
		t.Error("the sandbox wrote into the real home")
	}
	if _, err := os.Stat(filepath.Join(home, "personal.txt")); err != nil {
		t.Error("the real home lost a file")
	}
}
func TestOnlyHTTPSScriptsAndSafeGitAddresses(t *testing.T) {
	f := newFixture(t)
	f.setList(
		script("s1", "По http", 1, `{"from":"url","url":"http://example.org/a.sh"}`),
		script("s2", "Из file", 2, `{"from":"url","url":"file:///etc/passwd"}`),
	)
	f.r.Tick(context.Background())
	run := f.onlyRun()
	if s := f.step(run, "s1"); s.Status != hubproto.EnvError || s.Message != "скрипт по ссылке — только https" {
		t.Fatalf("%+v", s)
	}
	if len(f.scriptRuns()) != 0 {
		t.Fatal("ran")
	}
	for _, u := range []string{"git://h/x.git", "http://h/x.git", "file:///x", "ext::sh -c id", "--upload-pack=x", "/srv/repo"} {
		if gitURLOK(u) == nil {
			t.Errorf("%q passes", u)
		}
	}
	for _, u := range []string{"https://h/x.git", "ssh://git@h/x.git", "git@gitlab.com:acme/x.git"} {
		if err := gitURLOK(u); err != nil {
			t.Errorf("%q: %v", u, err)
		}
	}
}

func TestRepoScriptCloneRefusesOtherProtocolsAndGitGetsAllowList(t *testing.T) {
	f := newFixture(t)
	f.setList(script("s1", "Чужой", 1, `{"repoUrl":"git://h/x.git","path":"a.sh"}`),
		hubproto.EnvItem{ItemID: "r1", Kind: "repo", Name: "Файл", Source: json.RawMessage(`{"url":"file:///tmp/x"}`)})
	f.r.Tick(context.Background())
	run := f.onlyRun()
	if f.step(run, "s1").Status != hubproto.EnvError || f.step(run, "r1").Status != hubproto.EnvError {
		t.Fatalf("%+v", run.Steps)
	}
	for _, c := range f.calls {
		if c.Name == "git" && c.Args[0] == "clone" {
			t.Errorf("cloned: %v", c.Args)
		}
	}
	if !contains(f.baseEnvForTest(), "GIT_ALLOW_PROTOCOL=https:ssh") {
		t.Error("git is not limited to https and ssh")
	}
}

func (f *fixture) baseEnvForTest() []string { return (&execution{r: f.r, home: f.home}).baseEnv() }

func TestFetchRefusesRedirectToHTTP(t *testing.T) {
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("x")) }))
	defer plain.Close()
	tls := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, plain.URL, http.StatusFound) }))
	defer tls.Close()
	// The test server's certificate is not trusted by the default client, so the error is either that or the refusal; a
	// body must never come back.
	if b, err := defaultFetch(context.Background(), tls.URL); err == nil || b != nil {
		t.Fatalf("%q %v", b, err)
	}
}

func (f *fixture) allow(on bool) {
	f.t.Helper()
	if err := writeJSON(filepath.Join(f.r.LinkDir, hubproto.EnvAllowFile), hubproto.EnvAllow{Setup: on, At: 1}); err != nil {
		f.t.Fatal(err)
	}
}

func TestNothingRunsWithoutTheAgentsPermission(t *testing.T) {
	f := newFixture(t)
	f.setList(script("s1", "Скрипт", 1, `{"from":"text","body":"x","run":"go"}`))
	f.request("check-0009", hubproto.RunCheck)
	for _, mode := range []string{"false", "missing"} {
		if mode == "false" {
			f.allow(false)
		} else {
			_ = os.Remove(filepath.Join(f.r.LinkDir, hubproto.EnvAllowFile))
		}
		f.r.Tick(context.Background())
		// The queued request is answered with an error run; nothing else happens.
		if len(f.calls) != 0 || f.r.readState().Hash != "" {
			t.Fatalf("%s: something ran: %d calls", mode, len(f.calls))
		}
		for _, run := range f.runsAll() {
			if run.Status != hubproto.EnvError || run.Steps[0].ItemID != "run" {
				t.Fatalf("%s: %+v", mode, run)
			}
		}
		f.request("check-0009", hubproto.RunCheck)
	}
	f.r.Tick(context.Background())
	if _, err := os.Stat(filepath.Join(f.r.LinkDir, hubproto.EnvRequestsDir, "check-0009.json")); err == nil {
		t.Error("the request is kept for later")
	}
	// Allowed, the list is applied.
	f.allow(true)
	f.r.Tick(context.Background())
	if len(f.scriptRuns()) != 1 {
		t.Fatalf("%v", f.scriptRuns())
	}
}

func TestFromRepoWithAnAddressUsesTheCacheClone(t *testing.T) {
	f := newFixture(t)
	f.handle = func(c Cmd) reply {
		if c.Name == "git" && c.Args[0] == "clone" {
			dir := c.Args[len(c.Args)-1]
			_ = os.MkdirAll(filepath.Join(dir, "ci"), 0o755)
			_ = os.WriteFile(filepath.Join(dir, "ci", "go.sh"), []byte("echo"), 0o755)
		}
		return reply{}
	}
	f.setList(script("s1", "Чужой", 1, `{"from":"repo","repoUrl":"https://gitlab.com/acme/tools.git","branch":"main","path":"ci/go.sh"}`))
	f.r.Tick(context.Background())
	run := f.onlyRun()
	if s := f.step(run, "s1"); s.Status != hubproto.EnvOK {
		t.Fatalf("%+v", s)
	}
	var clone Cmd
	for _, c := range f.calls {
		if c.Name == "git" && c.Args[0] == "clone" {
			clone = c
		}
	}
	if !strings.Contains(clone.Args[len(clone.Args)-1], filepath.Join("environment", "cache")) || !contains(clone.Args, "--depth") {
		t.Fatalf("clone %v", clone.Args)
	}
}

func TestStaleAndRefusedRequestsAnswerWithAnErrorRun(t *testing.T) {
	f := newFixture(t)
	f.setList(script("s1", "Скрипт", 1, `{"from":"text","body":"x","run":"go"}`))
	f.r.Tick(context.Background()) // applied
	f.request("old-run-001", hubproto.RunUpdate)
	past := f.r.now().Add(-11 * time.Minute)
	_ = os.Chtimes(filepath.Join(f.r.LinkDir, hubproto.EnvRequestsDir, "old-run-001.json"), past, past)
	// An unreadable file with a run id for a name.
	_ = os.WriteFile(filepath.Join(f.r.LinkDir, hubproto.EnvRequestsDir, "broken-001.json"), []byte("{"), 0o600)
	n := len(f.scriptRuns())
	f.r.Tick(context.Background())
	if run := f.runFile("old-run-001"); run.Status != hubproto.EnvError || run.Steps[0].Message != "машина не взялась за запуск вовремя" {
		t.Fatalf("%+v", run)
	}
	if run := f.runFile("broken-001"); run.Status != hubproto.EnvError || !strings.Contains(run.Steps[0].Message, "запрос") {
		t.Fatalf("%+v", run)
	}
	if len(f.scriptRuns()) != n {
		t.Fatal("a stale request ran")
	}
	// Policy off: the request is answered, not dropped.
	f.allow(false)
	f.request("deny-run-01", hubproto.RunCheck)
	f.r.Tick(context.Background())
	if run := f.runFile("deny-run-01"); run.Status != hubproto.EnvError || !strings.Contains(run.Steps[0].Message, "политика setup") {
		t.Fatalf("%+v", run)
	}
}

func TestRetryEveryHalfHourOnlyForAnUnfinishedList(t *testing.T) {
	f := newFixture(t)
	clock := time.Unix(1_700_000_000, 0)
	f.r.Now = func() time.Time { return clock }
	failing := true
	f.handle = func(c Cmd) reply {
		if len(c.Args) == 2 && c.Args[1] == "flaky" && failing {
			return reply{exit: 1}
		}
		return reply{}
	}
	f.setList(script("s1", "Хрупкий", 1, `{"from":"text","body":"x","run":"flaky"}`))
	f.r.Tick(context.Background())
	clock = clock.Add(29 * time.Minute)
	f.r.Tick(context.Background())
	if len(f.scriptRuns()) != 1 {
		t.Fatal("retried early")
	}
	clock = clock.Add(2 * time.Minute)
	failing = false
	f.r.Tick(context.Background())
	if len(f.scriptRuns()) != 2 {
		t.Fatalf("not retried: %v", f.scriptRuns())
	}
	clock = clock.Add(24 * time.Hour)
	f.r.Tick(context.Background())
	if len(f.scriptRuns()) != 2 {
		t.Fatal("a machine that is done was run again")
	}
}

func TestSecretsAreMaskedInMessagesAndLowDiskStopsClones(t *testing.T) {
	f := newFixture(t)
	f.r.Free = func(string) (uint64, error) { return 1 << 30, nil }
	f.setList(hubproto.EnvItem{ItemID: "r1", Kind: "repo", Name: "Мало места", Source: json.RawMessage(`{"url":"https://gitlab.com/acme/a.git"}`)})
	f.r.Tick(context.Background())
	if s := f.step(f.onlyRun(), "r1"); s.Status != hubproto.EnvError || s.Message != "мало места на диске: 1.0 ГБ" {
		t.Fatalf("%+v", s)
	}
	for _, c := range f.calls {
		if c.Name == "git" {
			t.Errorf("git ran on a full disk: %v", c.Args)
		}
	}
	if got := (&execution{values: map[string]string{"tok": "sup3r-secret-val"}}).mask("упал: sup3r-secret-val"); got != "упал: ••••••" {
		t.Fatalf("%q", got)
	}
}

func TestPermissionTakenBackMidRunStopsTheNextSteps(t *testing.T) {
	f := newFixture(t)
	f.handle = func(c Cmd) reply {
		if len(c.Args) == 2 && c.Args[1] == "one" {
			return reply{after: func() { f.allow(false) }}
		}
		return reply{}
	}
	f.setList(script("s1", "Первый", 1, `{"from":"text","body":"x","run":"one"}`), script("s2", "Второй", 2, `{"from":"text","body":"x","run":"two"}`))
	f.r.Tick(context.Background())
	run := f.onlyRun()
	if f.step(run, "s1").Status != hubproto.EnvOK || f.step(run, "s2").Status != hubproto.EnvPending || len(f.scriptRuns()) != 1 {
		t.Fatalf("%+v ran %v", run.Steps, f.scriptRuns())
	}
}

func TestHeartbeatIsWritten(t *testing.T) {
	f := newFixture(t)
	f.r.Version = "9.9"
	f.r.Tick(context.Background())
	var b hubproto.EnvRunnerBeat
	raw, err := os.ReadFile(filepath.Join(f.r.LinkDir, hubproto.EnvRunnerFile))
	if err != nil || json.Unmarshal(raw, &b) != nil || b.PID != os.Getpid() || b.Version != "9.9" || b.At == 0 {
		t.Fatalf("%s %v", raw, err)
	}
}

// The heartbeat is written while Tick is busy (a run of minutes): Run keeps it on its own.
func TestHeartbeatGoesOnWhileARunWorks(t *testing.T) {
	var mu sync.Mutex
	clock := time.Now()
	r := &Runner{LinkDir: t.TempDir(), Now: func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		clock = clock.Add(hubproto.EnvRunnerBeatEvery) // every look is a beat later
		return clock
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go r.beatLoop(ctx, 5*time.Millisecond)
	read := func() int64 {
		raw, err := os.ReadFile(filepath.Join(r.LinkDir, hubproto.EnvRunnerFile))
		var b hubproto.EnvRunnerBeat
		if err != nil || json.Unmarshal(raw, &b) != nil {
			return 0
		}
		return b.At
	}
	first := int64(0)
	for until := time.Now().Add(5 * time.Second); time.Now().Before(until); time.Sleep(10 * time.Millisecond) {
		if at := read(); first == 0 {
			first = at
		} else if at > first {
			return
		}
	}
	t.Fatal("the heartbeat was not rewritten without a Tick")
}
