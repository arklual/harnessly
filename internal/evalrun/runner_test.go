package evalrun

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const testRun = "0b3f1c7e-5a62-4d0e-9a41-2f7d3c5b8e10"

func TestGlobToRe(t *testing.T) {
	cases := []struct {
		glob, file string
		want       bool
	}{
		{"src/**/*.ts", "src/a/b/c.ts", true},
		{"src/*.ts", "src/a/c.ts", false},
		{"src/*.ts", "src/c.ts", true},
		{"a.b", "axb", false},
		{"**", "x/y/z", true},
	}
	for _, c := range cases {
		if got := globToRe(c.glob).MatchString(c.file); got != c.want {
			t.Errorf("%q vs %q: got %v", c.glob, c.file, got)
		}
	}
}

func TestReadTranscriptCountsDistinctMessages(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	lines := []string{
		`{"type":"user","message":{"content":"hi"}}`,
		`{"type":"assistant","message":{"id":"m1","usage":{"input_tokens":10,"output_tokens":1,"cache_read_input_tokens":100,"cache_creation_input_tokens":5}}}`,
		`{"type":"assistant","message":{"id":"m1","usage":{"input_tokens":10,"output_tokens":7,"cache_read_input_tokens":100,"cache_creation_input_tokens":5}}}`,
		`not json`,
		`{"type":"assistant","message":{"id":"m2","usage":{"input_tokens":20,"output_tokens":3,"cache_read_input_tokens":300,"cache_creation_input_tokens":0}}}`,
	}
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")), 0o600); err != nil {
		t.Fatal(err)
	}
	u, err := readTranscript(p)
	if err != nil {
		t.Fatal(err)
	}
	if u.calls != 2 || u.firstCtx != 115 || u.peakCtx != 320 || u.tokensIn != 435 || u.tokensOut != 10 {
		t.Fatalf("got %+v", u)
	}
}

func TestValidate(t *testing.T) {
	ok := Request{RunID: testRun, Task: Task{Key: "t", Repo: "app", Base: "abcdef1", Prompt: "p"}, Arm: Arm{Key: "a", Model: "claude-sonnet-5-5"}}
	if why := validate(&ok); why != "" {
		t.Fatalf("a good request was refused: %s", why)
	}
	for name, mut := range map[string]func(*Request){
		"run id":   func(q *Request) { q.RunID = "../x" },
		"base":     func(q *Request) { q.Task.Base = "main; rm" },
		"repo":     func(q *Request) { q.Task.Repo = ".." },
		"repo sep": func(q *Request) { q.Task.Repo = "a/b" },
		"prompt":   func(q *Request) { q.Task.Prompt = "" },
		"model":    func(q *Request) { q.Arm.Model = "x y" },
		"agent":    func(q *Request) { q.Arm.Agent = "gpt" },
		"wall":     func(q *Request) { q.Task.Limits.WallS = maxWallS + 1 },
	} {
		q := ok
		mut(&q)
		if validate(&q) == "" {
			t.Errorf("%s: a bad request was accepted", name)
		}
	}
}

func TestSafeWriteRefusesSymlinksAndEscapes(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	if err := safeWrite(root, "a/b/c.txt", "x"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "a", "b", "c.txt")); string(b) != "x" {
		t.Fatal("not written")
	}
	for _, rel := range []string{"../x", "/etc/x", ".."} {
		if safeWrite(root, rel, "x") == nil {
			t.Errorf("%q escaped", rel)
		}
	}
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skip("no symlinks here")
	}
	if safeWrite(root, "link/evil.txt", "x") == nil {
		t.Fatal("went through a symlink")
	}
	if _, err := os.Stat(filepath.Join(outside, "evil.txt")); err == nil {
		t.Fatal("wrote outside the clone")
	}
}

func TestParsePorcelainAndShortstat(t *testing.T) {
	files := parsePorcelain(" M a.go\x00?? b c.go\x00R  new.go\x00old.go\x00")
	if strings.Join(files, "|") != "a.go|b c.go|new.go" {
		t.Fatalf("got %q", files)
	}
	a, r := parseShortstat(" 3 files changed, 10 insertions(+), 2 deletions(-)\n")
	if a != 10 || r != 2 {
		t.Fatalf("got %d %d", a, r)
	}
	if a, r := parseShortstat(" 1 file changed, 4 deletions(-)\n"); a != 0 || r != 4 {
		t.Fatalf("got %d %d", a, r)
	}
}

func TestWriteCredentialsKeepsNoRefreshToken(t *testing.T) {
	home, sb := t.TempDir(), t.TempDir()
	if ok, why, _ := writeCredentials(home, sb, 1000, 0); ok || !strings.Contains(why, "нет входа") {
		t.Fatalf("no login must say so, got %v %q", ok, why)
	}
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	cred := `{"claudeAiOauth":{"accessToken":"AT","refreshToken":"RT","expiresAt":5000000,"scopes":["a"],"subscriptionType":"max"}}`
	if err := os.WriteFile(filepath.Join(home, ".claude", ".credentials.json"), []byte(cred), 0o600); err != nil {
		t.Fatal(err)
	}
	ok, why, err := writeCredentials(home, sb, 1000, 60_000)
	if err != nil || !ok {
		t.Fatalf("got %v %q %v", ok, why, err)
	}
	b, _ := os.ReadFile(filepath.Join(sb, ".claude", ".credentials.json"))
	if !strings.Contains(string(b), "AT") || strings.Contains(string(b), "RT") || strings.Contains(string(b), "refresh") {
		t.Fatalf("the copy must carry the access token only: %s", b)
	}
	// A login that ends before the run can is put off, and the hub knows that text.
	ok, why, _ = writeCredentials(home, sb, 4_990_000, 60_000)
	if ok || !strings.Contains(why, "вход по подписке") {
		t.Fatalf("an expiring login must defer the run, got %v %q", ok, why)
	}
}

func TestWritePersonalKeepsOnlySafeEnv(t *testing.T) {
	home, sb := t.TempDir(), t.TempDir()
	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	settings := `{"env":{"CLAUDE_CODE_SUBAGENT_MODEL":"m","ANTHROPIC_API_KEY":"SECRET","GITLAB_TOKEN":"SECRET2"},"enabledPlugins":{"p@m":true,"q@m":false}}`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(settings), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("rules"), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := writePersonal(home, sb)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(sb, ".claude", "settings.json"))
	if strings.Contains(string(b), "SECRET") || !strings.Contains(string(b), "CLAUDE_CODE_SUBAGENT_MODEL") {
		t.Fatalf("settings carried the wrong env: %s", b)
	}
	if len(h.plugins) != 1 || !strings.HasPrefix(h.plugins[0], "p@m@") || h.claudeMd == "" {
		t.Fatalf("got %+v", h)
	}
}

func wait(t *testing.T, r *Runner, id string) State {
	t.Helper()
	for i := 0; i < 100; i++ {
		if s, ok := r.Poll([]string{id})[id]; ok && s.Terminal() {
			return s
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("the run did not end")
	return State{}
}

func request() Request {
	return Request{RunID: testRun, Task: Task{Key: "t", Repo: "app", Base: "abcdef1", Prompt: "p", Checks: []Check{{Name: "c", Run: "true"}}}, Arm: Arm{Key: "a", Model: "m", Harness: "repo"}}
}

func TestRunWithoutSandboxIsSkipped(t *testing.T) {
	r := New(t.TempDir(), t.TempDir(), t.TempDir(), nil)
	r.BwrapPath = ""
	if err := r.Start(request()); err != nil {
		t.Fatal(err)
	}
	s := wait(t, r, testRun)
	if s.State != Skipped || !strings.Contains(s.Error, "bubblewrap") {
		t.Fatalf("got %+v", s)
	}
	if _, err := os.Stat(r.workDir(testRun)); err == nil {
		t.Fatal("the clone was left behind")
	}
}

func TestRunWithoutLoginIsSkippedNotDeferred(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	r := New(t.TempDir(), t.TempDir(), t.TempDir(), nil)
	r.BwrapPath = "/bin/true"
	if err := r.Start(request()); err != nil {
		t.Fatal(err)
	}
	s := wait(t, r, testRun)
	if s.State != Skipped || !strings.Contains(s.Error, "нет входа") || strings.Contains(s.Error, "вход по подписке") {
		t.Fatalf("got %+v", s)
	}
}

func TestStartIsIdempotentAndOneAtATime(t *testing.T) {
	r := New(t.TempDir(), t.TempDir(), t.TempDir(), nil)
	r.BwrapPath = ""
	if err := r.Start(request()); err != nil {
		t.Fatal(err)
	}
	if err := r.Start(request()); err != nil {
		t.Fatalf("asking again for the same run must not fail: %v", err)
	}
	wait(t, r, testRun)
	if err := r.Start(request()); err != nil {
		t.Fatalf("a finished run asked for again must not fail: %v", err)
	}
	bad := request()
	bad.RunID = "nope"
	if r.Start(bad) == nil {
		t.Fatal("a bad id was accepted")
	}
}

func TestPollAfterRestartReportsFailed(t *testing.T) {
	r := New(t.TempDir(), t.TempDir(), t.TempDir(), nil)
	dir := r.runDir(testRun)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(r.workDir(testRun), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte(`{"runId":"`+testRun+`","state":"running"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got := r.Poll([]string{testRun, "0b3f1c7e-5a62-4d0e-9a41-2f7d3c5b8e11", "../../etc"})
	if len(got) != 1 {
		t.Fatalf("only the known run must be reported: %+v", got)
	}
	if s := got[testRun]; s.State != Failed || !strings.Contains(s.Error, "перезапущен") {
		t.Fatalf("got %+v", s)
	}
	if _, err := os.Stat(r.workDir(testRun)); err == nil {
		t.Fatal("the orphan clone stayed")
	}
	r.Stop(testRun) // a run that is not going: nothing happens
}

func TestVersionOrder(t *testing.T) {
	if !versionLess("2.9.0", "2.10.0") || versionLess("2.10.0", "2.9.0") {
		t.Fatal("versions must compare as numbers")
	}
}
