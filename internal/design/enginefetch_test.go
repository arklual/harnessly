package design

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The package's tests make projects from the shipped template: they get a stand-in engine.
func init() { testEngine = []byte("/* test engine */\n") }

// realEngine is a fetched board engine for the tests that run it in a browser: the file HARNESSLY_TEST_ENGINE names
// (`harnessly design engine fetch`, then upstream/design/<release>/artifact-type/dc-runtime.js of the engine folder).
// The engine is not in the tree, so without it the test is skipped.
func realEngine(t *testing.T) []byte {
	t.Helper()
	file := os.Getenv("HARNESSLY_TEST_ENGINE")
	if file == "" {
		t.Skip("HARNESSLY_TEST_ENGINE names no board engine")
	}
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestEngineHook(t *testing.T) {
	decision := func(in string, create bool) string {
		var out struct {
			H struct {
				D string `json:"permissionDecision"`
			} `json:"hookSpecificOutput"`
		}
		if err := json.Unmarshal(EngineHook([]byte(in), create), &out); err != nil {
			t.Fatal(err)
		}
		return out.H.D
	}
	cases := []struct {
		in     string
		create bool
		want   string
	}{
		{`{"tool_name":"Artifact","tool_input":{"action":"read","url":"x"}}`, false, "allow"},
		{`{"tool_name":"Artifact","tool_input":{"action":"list"}}`, false, "allow"},
		{`{"tool_name":"Artifact","tool_input":{"type_url":"t","title":"x"}}`, false, "deny"},
		{`{"tool_name":"Artifact","tool_input":{"type_url":"t","title":"x"}}`, true, "allow"},
		{`{"tool_name":"Artifact","tool_input":{"type_url":"t","url":"u"}}`, true, "deny"},
		{`{"tool_name":"Artifact","tool_input":{"action":"delete","url":"u"}}`, true, "deny"},
		{`{"tool_name":"Bash","tool_input":{"command":"ls"}}`, true, "deny"},
		{`not json`, true, "deny"},
	}
	for _, c := range cases {
		if got := decision(c.in, c.create); got != c.want {
			t.Errorf("%s create=%v: %s, want %s", c.in, c.create, got, c.want)
		}
	}
}

func TestFetchEngine(t *testing.T) {
	dir := t.TempDir()
	engine := "/* engine */"
	sum := sha256.Sum256([]byte(engine))
	hexSum := hex.EncodeToString(sum[:])
	outDir := regexp.MustCompile(`"out_dir":"([^"]+)"`)
	calls := map[string]int{}
	session := func(_ context.Context, prompt string, env []string) (string, error) {
		switch {
		case strings.Contains(prompt, `"type_url"`) && strings.Contains(prompt, `"read"`):
			calls["check"]++
			return "RELEASE 100-ab\nFILE index.html\nFILE artifact-type/dc-runtime.js\nFILE ../evil\n", nil
		case strings.Contains(prompt, `"publish"`):
			calls["create"]++
			if len(env) != 1 || env[0] != EngineHookCreateEnv+"=1" {
				t.Fatalf("the create session needs the hook's permission: %v", env)
			}
			return "CANVAS https://claude.ai/artifact/abc123", nil
		default:
			calls["download"]++
			if !strings.Contains(prompt, "https://claude.ai/artifact/abc123") || strings.Contains(prompt, "index.html") {
				t.Fatalf("download prompt: %s", prompt)
			}
			to := outDir.FindStringSubmatch(prompt)[1]
			if err := os.MkdirAll(filepath.Join(to, "artifact-type"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(to, "artifact-type", "dc-runtime.js"), []byte(engine), 0o644); err != nil {
				t.Fatal(err)
			}
			return "VERSION 200-cd\nFILE artifact-type/dc-runtime.js 12 " + hexSum + "\n", nil
		}
	}
	res, err := FetchEngine(context.Background(), FetchOptions{Dir: dir, session: session})
	if err != nil {
		t.Fatal(err)
	}
	if !res.New || res.Release != "100-ab" || res.Engine != "upstream/design/100-ab/artifact-type/dc-runtime.js" {
		t.Fatalf("result: %+v", res)
	}
	if calls["check"] != 2 || calls["create"] != 1 || calls["download"] != 1 {
		t.Fatalf("calls: %v", calls)
	}
	if readCanvas(dir) != "https://claude.ai/artifact/abc123" {
		t.Fatal("the canvas is remembered")
	}

	// The folder is a releases mirror the library reads: new projects get the fetched engine.
	l := New(Config{LibraryDir: t.TempDir(), ReleasesDir: dir, Texts: Texts{CLI: "acme"}})
	t.Cleanup(l.StopWatching)
	if s := l.Engine(); !s.Ready || s.Release == nil || *s.Release != "100-ab" || s.Fetch != "acme design engine fetch" {
		t.Fatalf("status: %+v", s)
	}
	if b, err := l.engineBytes(); err != nil || string(b) != engine {
		t.Fatalf("engine: %q %v", b, err)
	}

	// The same release again downloads nothing.
	res, err = FetchEngine(context.Background(), FetchOptions{Dir: dir, session: session})
	if err != nil || res.New || calls["download"] != 1 {
		t.Fatalf("again: %+v %v %v", res, err, calls)
	}
}

func TestFetchEngineChecksum(t *testing.T) {
	dir := t.TempDir()
	outDir := regexp.MustCompile(`"out_dir":"([^"]+)"`)
	session := func(_ context.Context, prompt string, _ []string) (string, error) {
		if strings.Contains(prompt, `"type_url"`) {
			return "RELEASE 1\nFILE artifact-type/dc-runtime.js", nil
		}
		to := outDir.FindStringSubmatch(prompt)[1]
		_ = os.MkdirAll(filepath.Join(to, "artifact-type"), 0o755)
		_ = os.WriteFile(filepath.Join(to, "artifact-type", "dc-runtime.js"), []byte("x"), 0o644)
		return "VERSION 2\nFILE artifact-type/dc-runtime.js 1 " + strings.Repeat("0", 64), nil
	}
	if _, err := FetchEngine(context.Background(), FetchOptions{Dir: dir, Canvas: "https://claude.ai/artifact/c", session: session}); err == nil || !strings.Contains(err.Error(), "sha256") {
		t.Fatalf("a checksum that differs is refused: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ReleasesFile)); err == nil {
		t.Fatal("nothing is listed after a refusal")
	}
}

func TestNoEngineRefusesNewProject(t *testing.T) {
	saved := testEngine
	testEngine = nil
	t.Cleanup(func() { testEngine = saved })
	root := t.TempDir()
	l := New(Config{LibraryDir: root, Texts: Texts{CLI: "acme"}})
	t.Cleanup(l.StopWatching)
	if s := l.Engine(); s.Ready {
		t.Fatal("no engine here")
	}
	if _, err := l.CreateProject("Landing"); err == nil || !strings.Contains(err.Error(), "acme design engine fetch") {
		t.Fatalf("refusal: %v", err)
	}
}
