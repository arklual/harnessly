package hlcli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
)

var cliFakeValue = "gl" + "pat-" + strings.Repeat("Q7", 10)

// safeRuntime is the runtime's safe routes for the command: one secret, and an ask that waits once then is ready.
func safeRuntime(t *testing.T) (*httptest.Server, *[]map[string]any) {
	t.Helper()
	var mu sync.Mutex
	var asks []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/cli/secrets":
			_, _ = w.Write([]byte(`{"secrets":[{"name":"gitlab-token","createdAt":1,"updatedAt":1761552000000,"lastUsedAt":0}]}`))
		case "/api/cli/secrets/use":
			names, _ := body["names"].([]any)
			if len(names) != 1 || names[0] != "gitlab-token" {
				w.WriteHeader(404)
				_, _ = w.Write([]byte(`{"error":"not-found"}`))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"values": map[string]string{"gitlab-token": cliFakeValue}})
		case "/api/cli/secrets/ask":
			mu.Lock()
			asks = append(asks, body)
			n := len(asks)
			mu.Unlock()
			status := "waiting"
			if body["name"] == "declined-one" {
				status = "declined"
			} else if n >= 2 {
				status = "ready"
			}
			_, _ = w.Write([]byte(`{"name":"x","status":"` + status + `","pageOpen":false}`))
		case "/api/cli/secrets/file":
			_, _ = w.Write([]byte(`{"path":"/dev/shm/harnessly-safe-1/ab/gitlab-token","expiresAt":1}`))
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &asks
}

func runSafe(t *testing.T, url string, args ...string) result {
	t.Helper()
	env := map[string]string{"HARNESSLY_URL": url, "HARNESSLY_CLI_TOKEN": "tok"}
	return runWith(t, t.TempDir(), env, "hs-agent", args...)
}

func TestSecretRunHandsTheValueToTheCommandAndMasksIt(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	srv, _ := safeRuntime(t)
	r := runSafe(t, srv.URL, "secret", "run", "--env", "TOKEN=gitlab-token", "--", "sh", "-c",
		`echo "token=$TOKEN"; echo "len=${#TOKEN}"; printf %s "$TOKEN" >&2; exit 3`)
	if r.code != 3 {
		t.Fatalf("the command's exit code: %d (%s)", r.code, r.errs)
	}
	if strings.Contains(r.out+r.errs, cliFakeValue) || strings.Contains(r.out+r.errs, "Q7Q7") {
		t.Fatalf("the value leaked: %q %q", r.out, r.errs)
	}
	if !strings.Contains(r.out, "token=••••••\n") || !strings.Contains(r.out, "len=26") || r.errs != "••••••" {
		t.Fatalf("out %q errs %q", r.out, r.errs)
	}
	// --file gives a path; the file holds the value and is gone after the command.
	r = runSafe(t, srv.URL, "secret", "run", "--file", "KEYFILE=gitlab-token", "--", "sh", "-c", `wc -c < "$KEYFILE"; echo "$KEYFILE"`)
	lines := strings.Fields(r.out)
	if r.code != 0 || len(lines) != 2 || lines[0] != "26" {
		t.Fatalf("file: %d %q %q", r.code, r.out, r.errs)
	}
	if _, err := os.Stat(lines[1]); err == nil {
		t.Fatalf("the file stayed: %s", lines[1])
	}
	r = runSafe(t, srv.URL, "secret", "run", "--env", "T=nope", "--", "true")
	if r.code != 1 || !strings.Contains(r.errs, "refused: not-found") {
		t.Fatalf("missing: %d %q", r.code, r.errs)
	}
}

func TestSecretCommandsNeverPrintAValue(t *testing.T) {
	srv, asks := safeRuntime(t)
	r := runSafe(t, srv.URL, "secret", "list")
	if r.code != 0 || !strings.HasPrefix(r.out, "gitlab-token\tput ") || !strings.Contains(r.out, "used never") {
		t.Fatalf("list: %d %q", r.code, r.out)
	}
	for _, sub := range []string{"get", "show", "print", "reveal"} {
		if r := runSafe(t, srv.URL, "secret", sub, "gitlab-token"); r.code != 2 || strings.Contains(r.out, cliFakeValue) {
			t.Fatalf("%s: %d %q", sub, r.code, r.out)
		}
	}
	if r := runSafe(t, srv.URL, "secret", "ask", "gitlab-token"); r.code != 2 {
		t.Fatal("ask without --why")
	}
	r = runSafe(t, srv.URL, "secret", "ask", "gitlab-token", "--why", "push the branch", "--wait", "0")
	if r.code != 0 || !strings.HasPrefix(r.out, "waiting: gitlab-token — the person's page is not open") {
		t.Fatalf("waiting: %q", r.out)
	}
	r = runSafe(t, srv.URL, "secret", "ask", "gitlab-token", "--why", "push the branch", "--replace")
	if r.code != 0 || !strings.HasPrefix(r.out, "ready: gitlab-token") {
		t.Fatalf("ready: %q %q", r.out, r.errs)
	}
	if last := (*asks)[len(*asks)-1]; last["session"] != "hs-agent" || last["why"] != "push the branch" || last["replace"] != true {
		t.Fatalf("ask body: %v", last)
	}
	if r := runSafe(t, srv.URL, "secret", "ask", "declined-one", "--why", "x"); r.code != 1 || !strings.Contains(r.errs, "declined") {
		t.Fatalf("declined: %d %q", r.code, r.errs)
	}
	if r := runSafe(t, srv.URL, "secret", "file", "gitlab-token"); r.code != 0 || r.out != "/dev/shm/harnessly-safe-1/ab/gitlab-token\n" {
		t.Fatalf("file: %q", r.out)
	}
}

func TestMaskerCatchesAValueSplitBetweenWrites(t *testing.T) {
	var out bytes.Buffer
	m := newMasker(&out, []string{"secret-value", "abc", "secret-value-long"})
	for _, chunk := range []string{"a: secr", "et-val", "ue; b: secret-value-long; tail secre"} {
		_, _ = m.Write([]byte(chunk))
	}
	if strings.Contains(out.String(), "secre") {
		t.Fatalf("written before the end was known: %q", out.String())
	}
	m.Flush()
	if got := out.String(); got != "a: ••••••; b: ••••••; tail secre" {
		t.Fatalf("%q", got)
	}
	// Ordinary output is not held back.
	out.Reset()
	_, _ = m.Write([]byte("hello abc\n"))
	if out.String() != "hello abc\n" {
		t.Fatalf("short values are not masked, nothing is held: %q", out.String())
	}
}
