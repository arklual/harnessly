package agentlogin

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

func TestLoadCLIs(t *testing.T) {
	dir := t.TempDir()
	if list, bad := LoadCLIs(filepath.Join(dir, "none.json")); list != nil || bad != nil {
		t.Fatalf("a missing file: %v %v", list, bad)
	}
	path := filepath.Join(dir, CLIsFile)
	body := `[
	  {"id": "example-cli", "label": "Example CLI", "login": ["example-cli", "login"], "status": ["example-cli", "whoami"], "timeout": 120},
	  {"id": "claude", "label": "Not mine", "login": ["x"], "status": ["x"]},
	  {"id": "Bad id", "label": "x", "login": ["x"], "status": ["x"]},
	  {"id": "no-status", "label": "x", "login": ["x"]},
	  {"id": "no-label", "label": " ", "login": ["x"], "status": ["x"]},
	  {"id": "line-break", "label": "x", "login": ["x", "a\nb"], "status": ["x"]},
	  {"id": "example-cli", "label": "Twice", "login": ["x"], "status": ["x"]}
	]`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	list, bad := LoadCLIs(path)
	if len(list) != 1 || list[0].ID != "example-cli" || list[0].TTL(time.Minute) != 2*time.Minute {
		t.Fatalf("%+v", list)
	}
	if len(bad) != 6 {
		t.Fatalf("%d refused: %v", len(bad), bad)
	}
	for _, err := range bad {
		if strings.Contains(err.Error(), "a\nb") {
			t.Errorf("a refusal repeats the command: %v", err)
		}
	}
	if _, ok := FindCLI(path, "claude"); ok {
		t.Error("an agent's name is a CLI")
	}
	if c, ok := FindCLI(path, "example-cli"); !ok || c.Label != "Example CLI" {
		t.Errorf("%+v %v", c, ok)
	}
}

// TestFakeExampleCLI is not a test: it is the generic CLI the tests below sign in, run as a child of the test binary.
// `login` listens on a loopback port, prints its authorization address (and hands it to $BROWSER) and waits for the
// provider's redirect; a redirect with the right state signs this account in and the listener goes away, so a replay
// finds nobody. `status` exits 0 once signed in.
func TestFakeExampleCLI(t *testing.T) {
	if os.Getenv("HL_FAKE_EXAMPLE_CLI") != "1" {
		t.Skip("the fake CLI of the other tests")
	}
	args := os.Args
	for i, a := range args {
		if a == "--" {
			args = args[i+1:]
			break
		}
	}
	marker := filepath.Join(os.Getenv("HOME"), ".example-cli-signed-in")
	switch args[0] {
	case "status":
		if _, err := os.Stat(marker); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	case "login":
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			os.Exit(2)
		}
		state := "st-example-123"
		url := fmt.Sprintf("https://auth.example.com/authorize?redirect_uri=http://%s/callback&state=%s", ln.Addr(), state)
		fmt.Println("Sign in at: " + url)
		if b := os.Getenv("BROWSER"); b != "" {
			out, _ := runBrowser(b, url)
			fmt.Print("browser: " + out)
		}
		done := make(chan struct{})
		srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/callback" || r.URL.Query().Get("state") != state || r.URL.Query().Get("code") == "" {
				http.Error(w, "bad callback", http.StatusBadRequest)
				return
			}
			_ = os.WriteFile(marker, []byte("ok"), 0o600)
			fmt.Fprintln(w, "signed in")
			close(done)
		})}
		go func() { _ = srv.Serve(ln) }()
		select {
		case <-done:
		case <-time.After(30 * time.Second):
		}
		_ = srv.Close()
		fmt.Println("Signed in.")
		os.Exit(0)
	}
	os.Exit(3)
}

func runBrowser(name, arg string) (string, error) {
	var out bytes.Buffer
	cmd := exec.Command(name, arg)
	cmd.Stdout = &out
	err := cmd.Run()
	return out.String(), err
}

// TestCLIConsoleLogin is #58's scenario on the machine: a CLI described in cli-logins.json signs in by console, the
// console shows the address the CLI printed, the provider's redirect reaches the CLI's loopback listener (the runtime's
// console page makes that request, #43), and the login ends done with the CLI's own status.
func TestCLIConsoleLogin(t *testing.T) {
	b := newBench(t, Timing{})
	b.rig.Link("echo")
	b.rig.Link("sh")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	b.rig.Script("example-cli", "HL_FAKE_EXAMPLE_CLI=1 exec env HL_FAKE_EXAMPLE_CLI=1 '"+exe+"' -test.run='^TestFakeExampleCLI$' -- \"$@\"")
	path := filepath.Join(b.rig.Home, CLIsFile)
	if err := os.WriteFile(path, []byte(`[{"id":"example-cli","label":"Example CLI","login":["example-cli","login"],"status":["example-cli","status"]}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	b.mgr.cfg.CLI = func(id string) (CLI, bool) { return FindCLI(path, id) }
	b.mgr.cfg.Status = func(ctx context.Context, provider string) hubproto.AgentStatus {
		c, _ := FindCLI(path, provider)
		return CLIStatus(ctx, b.rig.Checker, b.rig.EnvDir, c)
	}
	var notes syncBuffer
	b.mgr.cfg.ConsoleNote = func(_ context.Context, url, provider, state string, _ int64) bool {
		fmt.Fprintln(&notes, provider+" "+state)
		return false
	}
	// The console runs the line the way the runtime's terminal does, and its output is what the person reads.
	var screen syncBuffer
	b.mgr.cfg.Console = func(ctx context.Context, provider, line string) (string, error) {
		b.mu.Lock()
		b.console = append(b.console, provider+": "+line)
		b.mu.Unlock()
		cmd, err := b.rig.Checker.LoginCmd(context.Background(), line)
		if err != nil {
			return "", err
		}
		cmd.Stdout, cmd.Stderr = &screen, &screen
		if err := cmd.Start(); err != nil {
			return "", err
		}
		go func() { _ = cmd.Wait() }()
		return b.consoleURL, nil
	}

	if st := CLIStatus(context.Background(), b.rig.Checker, b.rig.EnvDir, CLI{ID: "example-cli", Label: "Example CLI", Status: []string{"example-cli", "status"}}); st.Login != hubproto.LoggedOut || !st.Installed || st.Kind != hubproto.KindCLI {
		t.Fatalf("before: %+v", st)
	}
	if _, err := b.mgr.Start("k1", "example-cli", "link"); hubErr(err) != hubproto.CodeUnsupported {
		t.Fatalf("a CLI by link: %v", err)
	}
	if _, err := b.mgr.Start("k0", "other-cli", "console"); hubErr(err) != hubproto.CodeUnsupported {
		t.Fatalf("a CLI the file does not describe: %v", err)
	}
	if _, err := b.mgr.Start("k2", "example-cli", "console"); err != nil {
		t.Fatal(err)
	}
	b.waitFor("waiting")

	// The person reads the address in the console, signs in at the provider and ends on the loopback address.
	addr := regexp.MustCompile(`redirect_uri=http://(127\.0\.0\.1:\d+)/callback&state=(\S+)`)
	var m []string
	for deadline := time.Now().Add(20 * time.Second); m == nil && time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		m = addr.FindStringSubmatch(screen.String())
	}
	if m == nil {
		t.Fatalf("no address in the console: %q", screen.String())
	}
	if !strings.Contains(screen.String(), "browser: https://auth.example.com/") {
		t.Errorf("$BROWSER did not print the address: %q", screen.String())
	}
	callback := "http://" + m[1] + "/callback?code=secret-code-42&state=" + m[2]
	if resp, err := http.Get(callback); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("callback: %v %v", resp, err)
	}
	done := b.waitFor("done")
	if done.Status == nil || done.Status.Provider != "example-cli" || done.Status.Kind != hubproto.KindCLI ||
		done.Status.Label != "Example CLI" || done.Status.Login != hubproto.LoggedIn {
		t.Fatalf("%+v", done.Status)
	}
	// A replay finds no listener: the CLI took its one redirect.
	if resp, err := http.Get(callback); err == nil {
		resp.Body.Close()
		for deadline := time.Now().Add(5 * time.Second); err == nil && time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
			resp, err = http.Get(callback)
			if err == nil {
				resp.Body.Close()
			}
		}
		if err == nil {
			t.Error("the callback was taken twice")
		}
	}

	// The page was told the attempt waited, and that it connected (after the status check), nothing else.
	if n := notes.String(); !strings.HasPrefix(n, "example-cli waiting\n") || !strings.HasSuffix(n, "example-cli connected\n") || strings.Contains(n, "secret") {
		t.Errorf("notes: %q", n)
	}

	b.mu.Lock()
	cmds, logins := b.console, b.logins
	b.mu.Unlock()
	inner := ""
	if len(cmds) == 1 {
		inner, _ = agents.UnwrapAnyShell(strings.TrimPrefix(cmds[0], "example-cli: "))
	}
	if inner != "exec env BROWSER=echo DISPLAY= WAYLAND_DISPLAY= 'example-cli' 'login'" {
		t.Errorf("console asked for %v", cmds)
	}
	for _, l := range logins {
		if s := fmt.Sprintf("%+v", l); strings.Contains(s, "secret-code-42") {
			t.Errorf("the authorization code reached the hub: %s", s)
		}
	}
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// TestCLIConsoleCancelledOnThePage: the person cancels on the console page; the next note tells the manager, which ends
// the login as cancelled without a status ever saying logged in.
func TestCLIConsoleCancelledOnThePage(t *testing.T) {
	b := newBench(t, Timing{})
	path := filepath.Join(b.rig.Home, CLIsFile)
	if err := os.WriteFile(path, []byte(`[{"id":"example-cli","label":"Example CLI","login":["example-cli","login"],"status":["example-cli","status"]}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	b.mgr.cfg.CLI = func(id string) (CLI, bool) { return FindCLI(path, id) }
	b.mgr.cfg.Status = func(context.Context, string) hubproto.AgentStatus {
		return hubproto.AgentStatus{Login: hubproto.LoggedOut}
	}
	b.mgr.cfg.Console = func(context.Context, string, string) (string, error) { return b.consoleURL, nil }
	var n atomic.Int32
	b.mgr.cfg.ConsoleNote = func(_ context.Context, _, _, state string, _ int64) bool {
		return n.Add(1) > 1 && state == "waiting"
	}
	if _, err := b.mgr.Start("k9", "example-cli", "console"); err != nil {
		t.Fatal(err)
	}
	b.waitFor("cancelled")
}
