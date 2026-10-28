package main

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
	"time"

	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/secrets"
)

var routeFakeValue = "sk" + "-proj-" + strings.Repeat("Xy3", 12)

// captureStdio sends the process's stdout and stderr (where the runtime's log lines go) to a buffer for the test.
func captureStdio(t *testing.T) func() string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = w, w
	var buf bytes.Buffer
	done := make(chan struct{})
	go func() { _, _ = io.Copy(&buf, r); close(done) }()
	restored := false
	restore := func() string {
		if !restored {
			restored = true
			os.Stdout, os.Stderr = oldOut, oldErr
			_ = w.Close()
			<-done
		}
		return buf.String()
	}
	t.Cleanup(func() { restore() })
	return restore
}

func safeRig(t *testing.T) (func(method, path, body string, h map[string]string) (int, string), string, string) {
	t.Helper()
	dir, state := t.TempDir(), t.TempDir()
	t.Setenv("RUNTIME_SECRETS_KEY_FILE", filepath.Join(state, "safe.key"))
	t.Setenv("RUNTIME_SECRETS_FILES_DIR", t.TempDir())
	if err := os.WriteFile(filepath.Join(dir, "cli-token"), []byte("tok"), 0o600); err != nil {
		t.Fatal(err)
	}
	d := deps{DataDir: dir, Home: t.TempDir(), CLI: &gate.CLIToken{DataDir: dir, Headers: []string{"x-cli-token"}},
		SessionOK: func(r *http.Request) bool { return r.Header.Get("X-Test-Session") == "yes" }}
	sf := newSafe(d)
	t.Cleanup(sf.files.Close)
	routes := sf.routes(d, func(session string) (string, string) {
		if session == "hs-agent" {
			return "c-1", "Release notes"
		}
		return "", ""
	})
	return func(method, path, body string, h map[string]string) (int, string) {
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		for k, v := range h {
			req.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		for _, rt := range routes {
			if rt.Match(req) {
				rt.Handler.ServeHTTP(w, req)
				return w.Code, w.Body.String()
			}
		}
		t.Fatalf("no route for %s", path)
		return 0, ""
	}, dir, state
}

func TestSafeRoutesAskPutUse(t *testing.T) {
	logs := captureStdio(t)
	call, dir, _ := safeRig(t)
	page := map[string]string{"X-Test-Session": "yes"}
	cli := map[string]string{"x-cli-token": "tok"}
	var answers []string
	keep := func(code int, body string) (int, string) {
		answers = append(answers, body)
		return code, body
	}

	// Each caller by its own door.
	if code, _ := call("GET", "/api/platform/secrets", "", cli); code != http.StatusUnauthorized {
		t.Fatalf("the cli-token is not a person: %d", code)
	}
	if code, _ := call("POST", "/api/cli/secrets/use", `{"names":["a"]}`, page); code != http.StatusForbidden {
		t.Fatalf("a page session does not read values: %d", code)
	}
	if code, _ := call("PUT", "/api/platform/secrets/openai-key", `{"value":"x"}`, map[string]string{"X-Test-Session": "yes", "Sec-Fetch-Site": "cross-site"}); code != http.StatusForbidden {
		t.Fatalf("another site's page: %d", code)
	}

	// The agent asks; the page sees the card; the person puts the value; the agent's wait ends ready.
	type askAnswer struct {
		Status   string `json:"status"`
		PageOpen bool   `json:"pageOpen"`
	}
	got := make(chan askAnswer, 1)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, body := call("POST", "/api/cli/secrets/ask", `{"session":"hs-agent","name":"openai-key","why":"call the API","wait":5}`, cli)
		var a askAnswer
		_ = json.Unmarshal([]byte(body), &a)
		got <- a
	}()
	var asks struct {
		Asks []secrets.Ask `json:"asks"`
	}
	deadline := time.Now().Add(3 * time.Second)
	for len(asks.Asks) == 0 && time.Now().Before(deadline) {
		_, body := call("GET", "/agents/api/platform/secret-asks", "", page)
		_ = json.Unmarshal([]byte(body), &asks)
		time.Sleep(5 * time.Millisecond)
	}
	if len(asks.Asks) != 1 || asks.Asks[0].Name != "openai-key" || asks.Asks[0].Title != "Release notes" || asks.Asks[0].Why != "call the API" {
		t.Fatalf("the card: %+v", asks)
	}
	code, body := keep(call("PUT", "/api/platform/secrets/openai-key", `{"value":"`+routeFakeValue+`\n"}`, page))
	if code != 200 || !strings.Contains(body, `"created":true`) {
		t.Fatalf("put: %d %s", code, body)
	}
	if a := <-got; a.Status != "ready" || !a.PageOpen {
		t.Fatalf("the agent's wait: %+v", a)
	}
	wg.Wait()
	// Asked again, a kept secret is ready at once.
	if _, body := keep(call("POST", "/api/cli/secrets/ask", `{"session":"hs-agent","name":"openai-key","why":"x","wait":5}`, cli)); !strings.Contains(body, `"status":"ready"`) {
		t.Fatal(body)
	}

	// Lists carry no value.
	keep(call("GET", "/api/platform/secrets", "", page))
	keep(call("GET", "/api/cli/secrets", "", cli))

	// The one way out: a command's use, recorded with the conversation.
	code, body = call("POST", "/api/cli/secrets/use", `{"session":"hs-agent","names":["openai-key"]}`, cli)
	if code != 200 || !strings.Contains(body, routeFakeValue) {
		t.Fatalf("use: %d", code)
	}
	code, body = keep(call("GET", "/api/platform/secrets", "", page))
	if code != 200 || !strings.Contains(body, `"title":"Release notes"`) || strings.Contains(body, `"lastUsedAt":0`) {
		t.Fatalf("the use is not shown: %s", body)
	}
	code, body = keep(call("POST", "/api/cli/secrets/file", `{"session":"hs-agent","name":"openai-key","ttl":60}`, cli))
	var f struct {
		Path string `json:"path"`
	}
	_ = json.Unmarshal([]byte(body), &f)
	if raw, err := os.ReadFile(f.Path); code != 200 || err != nil || string(raw) != routeFakeValue {
		t.Fatalf("file: %d %s %v", code, body, err)
	}
	if code, body := keep(call("POST", "/api/cli/secrets/use", `{"names":["nope"]}`, cli)); code != 404 || body != `{"error":"not-found"}`+"\n" {
		t.Fatalf("missing: %d %s", code, body)
	}

	// The person declines another ask.
	go func() {
		_, body := call("POST", "/api/cli/secrets/ask", `{"session":"hs-agent","name":"aws-key","why":"deploy","wait":5}`, cli)
		var a askAnswer
		_ = json.Unmarshal([]byte(body), &a)
		got <- a
	}()
	asks.Asks = nil
	for len(asks.Asks) == 0 && time.Now().Before(deadline.Add(3*time.Second)) {
		_, body := call("GET", "/api/platform/secret-asks", "", page)
		_ = json.Unmarshal([]byte(body), &asks)
		time.Sleep(5 * time.Millisecond)
	}
	if len(asks.Asks) != 1 {
		t.Fatalf("second card: %+v", asks)
	}
	if code, _ := call("POST", "/api/platform/secret-asks/"+asks.Asks[0].ID+"/decline", "", page); code != 200 {
		t.Fatal(code)
	}
	if a := <-got; a.Status != "declined" {
		t.Fatalf("declined: %+v", a)
	}

	// Removal, with the page's session only.
	if code, _ := call("DELETE", "/api/platform/secrets/openai-key", "", cli); code != http.StatusUnauthorized {
		t.Fatal("an agent removes a secret")
	}
	if code, _ := keep(call("DELETE", "/api/platform/secrets/openai-key", "", page)); code != 200 {
		t.Fatal(code)
	}
	if code, _ := call("PUT", "/api/platform/secrets/..%2Fx", `{"value":"v"}`, page); code != 400 {
		t.Fatalf("bad name: %d", code)
	}

	// Nothing but the use and the file carried the value: not the lists, the asks, the file on disk, the log.
	raw, _ := os.ReadFile(filepath.Join(dir, "secrets", "safe.json"))
	all := strings.Join(answers, "\n") + string(raw) + logs()
	if strings.Contains(all, routeFakeValue) || strings.Contains(all, "Xy3Xy3") {
		t.Fatalf("the value leaked into an answer, the safe's file or the log")
	}
	if !strings.Contains(logs(), "safe: secrets used") {
		t.Fatal("the use is not logged")
	}
}
