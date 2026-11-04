package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/secrets"
	"github.com/ly-solutions/harnessly/internal/tmux"
)

func TestSafeRoutesSocketCallerProjectIsolation(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("socket ownership requires Linux /proc")
	}
	tmuxBin, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("no tmux")
	}
	curlBin, err := exec.LookPath("curl")
	if err != nil {
		t.Skip("no curl")
	}
	shellBin, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	logs := captureStdio(t)
	value := "gh" + "p_" + strings.Repeat("S8", 18)
	defer func() {
		if strings.Contains(logs(), value) {
			t.Error("runtime logs exposed a secret value")
		}
	}()

	// Short, private socket paths avoid the Unix socket limit even under a long test name.
	socketDir, err := os.MkdirTemp("/tmp", "hs-test-safe-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(socketDir) })
	t.Setenv("TMUX_TMPDIR", socketDir)
	socket := fmt.Sprintf("hs-test-safe-%d-%d", os.Getpid(), rand.IntN(1_000_000_000))
	runTmux := func(args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, tmuxBin, append([]string{"-f", "/dev/null", "-L", socket}, args...)...)
		cmd.WaitDelay = time.Second
		return cmd.CombinedOutput()
	}
	t.Cleanup(func() { _, _ = runTmux("kill-server") })

	dir := t.TempDir()
	write := func(name string, raw []byte) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	token := "test-cli-" + strings.Repeat("q6", 20)
	write("cli-token", []byte(token))
	headerFile := write("request.headers", []byte("X-CLI-Token: "+token+"\nContent-Type: application/json\n"))
	d := deps{DataDir: dir, CLI: &gate.CLIToken{DataDir: dir, Headers: []string{"X-CLI-Token"}},
		SessionOK: func(*http.Request) bool { return false }}
	sf := &safe{store: secrets.Open(filepath.Join(dir, "safe"), filepath.Join(dir, "state", "safe.key")),
		asks: secrets.NewAsks(), files: secrets.NewFiles(filepath.Join(dir, "files"))}
	t.Cleanup(sf.files.Close)
	for _, name := range []string{"scoped-a", "scoped-b", "legacy-key"} {
		if _, _, err := sf.store.Put(name, value); err != nil {
			t.Fatal(err)
		}
		if name != "legacy-key" {
			project := "project-" + strings.TrimPrefix(name, "scoped-")
			if _, err := sf.store.Metadata(name, []string{"https://example.com"}, []string{project}); err != nil {
				t.Fatal(err)
			}
		}
	}
	sessionOf := func(pane string) string { return "hs-safe-" + pane }
	convOfSession := func(session string) string {
		for _, pane := range []string{"a", "b"} {
			if session == sessionOf(pane) {
				return "chat-" + pane
			}
		}
		return ""
	}
	callers := &callerCache{panes: tmux.New(tmux.Names{Socket: socket, Prefix: "hs-safe-"}), convOf: convOfSession}
	identity := func(r *http.Request, session string) secrets.Use {
		actual, _ := callers.of(r)
		if actual == "" || session != "" && convOfSession(session) != actual {
			return secrets.Use{}
		}
		return secrets.Use{Conv: actual, Project: "project-" + strings.TrimPrefix(actual, "chat-"), Title: "Verified " + actual}
	}
	routes := sf.routes(d, func(session string) (string, string) { return convOfSession(session), "Claimed title" }, identity)

	type requestCase struct {
		label, pane, path, kind, name, forbiddenName string
		body                                         map[string]any
		status                                       int
	}
	var cases []requestCase
	for _, pane := range []string{"a", "b"} {
		other := "a"
		if pane == "a" {
			other = "b"
		}
		own, foreign := "scoped-"+pane, "scoped-"+other
		add := func(label, endpoint, kind, name, session string, status int) {
			body := map[string]any{"name": name, "names": []string{name}, "session": session,
				"project": "project-" + other, "conv": "chat-" + other, "why": "sign in", "wait": 0}
			forbidden := ""
			if status == http.StatusNotFound {
				forbidden = name
			}
			cases = append(cases, requestCase{label: pane + "-" + label, pane: pane, path: "/api/cli/secrets/" + endpoint,
				kind: kind, name: name, forbiddenName: forbidden, body: body, status: status})
		}
		add("own-use", "use", "values", own, sessionOf(pane), http.StatusOK)
		add("own-file", "file", "file", own, sessionOf(pane), http.StatusOK)
		add("own-use-no-claim", "use", "values", own, "", http.StatusOK)
		for _, endpoint := range []string{"use", "file"} {
			add("foreign-"+endpoint, endpoint, "denied", foreign, sessionOf(pane), http.StatusNotFound)
			add("forged-"+endpoint, endpoint, "denied", foreign, sessionOf(other), http.StatusNotFound)
			add("unknown-session-"+endpoint, endpoint, "denied", own, "hs-safe-missing", http.StatusNotFound)
			kind := "values"
			if endpoint == "file" {
				kind = "file"
			}
			add("legacy-"+endpoint, endpoint, kind, "legacy-key", sessionOf(pane), http.StatusOK)
			add("legacy-forged-"+endpoint, endpoint, kind, "legacy-key", sessionOf(other), http.StatusOK)
		}
		add("forged-ask", "ask", "denied", foreign, sessionOf(other), http.StatusNotFound)
		add("unknown-session-ask", "ask", "denied", own, "hs-safe-missing", http.StatusNotFound)
		add("own-ask", "ask", "ready", own, sessionOf(pane), http.StatusOK)
		for _, claim := range []string{pane, other} {
			cases = append(cases, requestCase{label: pane + "-list-claim-" + claim, pane: pane,
				path: "/api/cli/secrets?session=" + sessionOf(claim) + "&project=project-" + claim,
				kind: "list", name: own, forbiddenName: foreign, status: http.StatusOK})
		}
	}
	type observation struct {
		label, actual, cacheControl string
		status                      int
		body                        []byte
	}
	observations := make(chan observation, len(cases))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actual, _ := callers.of(r)
		rr := httptest.NewRecorder()
		matched := false
		for _, route := range routes {
			if route.Match(r) {
				route.Handler.ServeHTTP(rr, r)
				matched = true
				break
			}
		}
		if !matched {
			http.NotFound(rr, r)
		}
		for key, values := range rr.Header() {
			w.Header()[key] = values
		}
		w.WriteHeader(rr.Code)
		_, _ = w.Write(rr.Body.Bytes())
		observations <- observation{label: r.URL.Query().Get("case"), actual: actual, status: rr.Code,
			cacheControl: rr.Header().Get("Cache-Control"), body: bytes.Clone(rr.Body.Bytes())}
	}))
	defer server.Close()
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
	for _, pane := range []string{"a", "b"} {
		var script strings.Builder
		script.WriteString("set -eu\n")
		for _, tc := range cases {
			if tc.pane != pane {
				continue
			}
			url := server.URL + tc.path
			if strings.Contains(url, "?") {
				url += "&"
			} else {
				url += "?"
			}
			url += "case=" + tc.label
			// Curl ignores user configuration, bypasses proxies, and prints only its status.
			cmd := quote(curlBin) + " -q --silent --show-error --noproxy '*' --max-time 10 --output /dev/null --write-out '%{http_code}\\n' --header " + quote("@"+headerFile)
			if tc.body != nil {
				raw, err := json.Marshal(tc.body)
				if err != nil {
					t.Fatal(err)
				}
				bodyFile := write(tc.label+".request", raw)
				cmd += " --data-binary " + quote("@"+bodyFile)
			}
			script.WriteString(cmd + " " + quote(url) + " >" + quote(filepath.Join(dir, tc.label+".status")) + " 2>" + quote(filepath.Join(dir, tc.label+".stderr")) + "\n")
		}
		script.WriteString(": >" + quote(filepath.Join(dir, pane+".done")) + "\nexec sleep 30\n")
		scriptFile := write(pane+".sh", []byte(script.String()))
		if _, err := runTmux("new-session", "-d", "-s", sessionOf(pane), "-c", dir, shellBin, scriptFile); err != nil {
			t.Fatalf("start private pane %s: %v", pane, err)
		}
	}
	// Keep both pane roots alive until curl has consumed every response body.
	deadline := time.Now().Add(30 * time.Second)
	for _, pane := range []string{"a", "b"} {
		for {
			if _, err := os.Stat(filepath.Join(dir, pane+".done")); err == nil {
				break
			} else if !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if time.Now().After(deadline) {
				t.Fatalf("private pane %s did not complete its curl requests", pane)
			}
			time.Sleep(10 * time.Millisecond)
		}
		terminal, err := runTmux("capture-pane", "-p", "-t", sessionOf(pane))
		if err != nil {
			t.Fatalf("capture private pane %s: %v", pane, err)
		}
		if bytes.Contains(terminal, []byte(value)) {
			t.Error("curl exposed a secret value in tmux output")
		}
	}
	got := make(map[string]observation, len(cases))
	for range cases {
		select {
		case observation := <-observations:
			if _, duplicate := got[observation.label]; duplicate {
				t.Fatalf("duplicate response label %q", observation.label)
			}
			got[observation.label] = observation
		case <-time.After(5 * time.Second):
			t.Fatal("a curl request did not reach the temporary server")
		}
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			answer, ok := got[tc.label]
			if !ok {
				t.Fatal("no response observation")
			}
			if answer.actual != "chat-"+tc.pane {
				t.Fatalf("socket resolved to %q, want actual pane %s", answer.actual, tc.pane)
			}
			if answer.status != tc.status {
				t.Errorf("HTTP status %d, want %d", answer.status, tc.status)
			}
			if answer.cacheControl != "no-store" {
				t.Error("secret response is cacheable")
			}
			status, err := os.ReadFile(filepath.Join(dir, tc.label+".status"))
			if err != nil || strings.TrimSpace(string(status)) != strconv.Itoa(tc.status) {
				t.Error("curl did not receive the expected HTTP status")
			}
			stderr, err := os.ReadFile(filepath.Join(dir, tc.label+".stderr"))
			if err != nil || bytes.Contains(stderr, []byte(value)) {
				t.Error("curl stderr is missing or exposed a secret value")
			}
			if tc.kind != "values" && bytes.Contains(answer.body, []byte(value)) {
				t.Error("non-value response exposed a secret value")
			}
			if tc.forbiddenName != "" && bytes.Contains(answer.body, []byte(tc.forbiddenName)) {
				t.Error("foreign or forged-session response exposed a scoped name")
			}
			switch tc.kind {
			case "denied":
				if strings.TrimSpace(string(answer.body)) != `{"error":"not-found"}` {
					t.Error("denial differs from an absent secret")
				}
			case "values":
				var body struct {
					Values map[string]string `json:"values"`
				}
				if err := json.Unmarshal(answer.body, &body); err != nil || len(body.Values) != 1 || body.Values[tc.name] != value {
					t.Error("authorized use returned the wrong secret")
				}
			case "file":
				var body struct {
					Path string `json:"path"`
				}
				if err := json.Unmarshal(answer.body, &body); err != nil {
					t.Fatal("invalid file response")
				}
				if !strings.HasPrefix(body.Path, sf.files.Dir+string(os.PathSeparator)) {
					t.Fatal("secret file escaped the temporary test directory")
				}
				raw, err := os.ReadFile(body.Path)
				if err != nil || string(raw) != value {
					t.Error("authorized secret file has the wrong value")
				}
				st, err := os.Stat(body.Path)
				if err != nil || st.Mode().Perm() != 0o600 {
					t.Error("secret file is not owner-only")
				}
			case "list":
				var body struct {
					Secrets []cliSecret `json:"secrets"`
				}
				if err := json.Unmarshal(answer.body, &body); err != nil {
					t.Fatal("invalid list response")
				}
				var names []string
				for _, v := range body.Secrets {
					names = append(names, v.Name)
				}
				slices.Sort(names)
				if !slices.Equal(names, []string{"legacy-key", tc.name}) {
					t.Error("list did not use the actual socket caller's project")
				}
			case "ready":
				var body struct {
					Status string `json:"status"`
				}
				if err := json.Unmarshal(answer.body, &body); err != nil || body.Status != secrets.AskReady {
					t.Error("same-project existing secret did not report ready")
				}
			}
		})
	}
	if len(sf.asks.Pending()) != 0 {
		t.Error("a forged-session ask opened an approval card")
	}
	// The ordinary test process has the token and a real session name, but neither pane owns its socket.
	request, err := http.NewRequest(http.MethodGet, server.URL+"/api/cli/secrets?case=outside&session="+sessionOf("a"), nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-CLI-Token", token)
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	raw, readErr := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if readErr != nil || response.StatusCode != http.StatusOK || bytes.Contains(raw, []byte(value)) || bytes.Contains(raw, []byte("scoped-")) {
		t.Error("outside process could see scoped metadata")
	}
	var outside struct {
		Secrets []cliSecret `json:"secrets"`
	}
	if err := json.Unmarshal(raw, &outside); err != nil || len(outside.Secrets) != 1 || outside.Secrets[0].Name != "legacy-key" {
		t.Error("outside process did not retain legacy unscoped visibility")
	}
}
