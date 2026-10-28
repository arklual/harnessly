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
	"github.com/ly-solutions/harnessly/internal/notifybots"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// fakeAskBot is the Bot API of one bot for asks: sends recorded, no updates, edits taken.
func fakeAskBot(t *testing.T) *[]map[string]any {
	t.Helper()
	var mu sync.Mutex
	var sent []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		switch r.URL.Path {
		case "/bot" + notifyToken + "/sendMessage":
			mu.Lock()
			sent = append(sent, body)
			mu.Unlock()
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":7}}`))
		case "/bot" + notifyToken + "/getUpdates":
			time.Sleep(50 * time.Millisecond)
			_, _ = w.Write([]byte(`{"ok":true,"result":[]}`))
		default:
			_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("RUNTIME_TELEGRAM_API_URL", srv.URL)
	return &sent
}

func TestAskRoutes(t *testing.T) {
	sent := fakeAskBot(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cli-token"), []byte("tok"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := notifybots.Open(dir).Add(notifybots.Bot{ID: "nb_1", Name: "Team", Username: "ping_bot", Token: notifyToken,
		Chat: &notifybots.Chat{ID: -5, Title: "Team chat", Type: "group"}}); err != nil {
		t.Fatal(err)
	}
	d := deps{DataDir: dir, CLI: &gate.CLIToken{DataDir: dir, Headers: []string{"x-cli-token"}},
		SessionOK: func(r *http.Request) bool { return r.Header.Get("X-Test-Session") == "yes" }}
	routes := askRoutes(d, nil)
	call := func(method, path, body string, h map[string]string) (int, string) {
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
	}
	page := map[string]string{"X-Test-Session": "yes"}
	cli := map[string]string{"x-cli-token": "tok"}

	if code, _ := call("POST", "/api/cli/asks", `{"question":"x"}`, page); code != http.StatusForbidden {
		t.Fatalf("a page is not an agent: %d", code)
	}
	if code, _ := call("GET", platformAPI+"/asks", "", cli); code != http.StatusUnauthorized {
		t.Fatalf("an agent is not the person: %d", code)
	}
	code, body := call("POST", "/api/cli/asks", `{"kind":"approval","question":"Ship it?","to":"@anna_dev","session":"hs-1"}`, cli)
	if code != http.StatusOK {
		t.Fatalf("create: %d %s", code, body)
	}
	var out struct {
		Ask map[string]any `json:"ask"`
	}
	_ = json.Unmarshal([]byte(body), &out)
	id, _ := out.Ask["id"].(string)
	if !strings.HasPrefix(id, "ask_") || strings.Contains(body, "chatId") || strings.Contains(body, notifyToken) {
		t.Fatalf("the view: %s", body)
	}
	if len(*sent) != 1 || !strings.Contains((*sent)[0]["text"].(string), "@anna_dev") {
		t.Fatalf("sent: %+v", *sent)
	}

	// The background task of `harnessly ask` is told by its output's first line.
	outFile := filepath.Join(dir, "task.output")
	_ = os.WriteFile(outFile, []byte("asked "+id+" → @anna_dev in «Team chat», waiting up to 1h0m0s\n"), 0o600)
	task := wire.BackgroundTask{Kind: wire.BgCommand, Title: "harnessly ask", OutputPath: &outFile}
	describeAsk(&task)
	if !strings.Contains(task.Title, "@anna_dev") || !strings.Contains(task.Title, "Team chat") {
		t.Fatalf("background title: %q", task.Title)
	}

	// The person approves in the browser; the agent's wait returns it, marked as seen.
	code, body = call("POST", platformAPI+"/asks/"+id+"/answer", `{"choice":"approved"}`, page)
	if code != http.StatusOK || !strings.Contains(body, `"decision":"approved"`) {
		t.Fatalf("answer: %d %s", code, body)
	}
	code, body = call("GET", "/api/cli/asks/"+id+"/wait?timeout=1", "", cli)
	if code != http.StatusOK || !strings.Contains(body, `"status":"answered"`) || strings.Contains(body, `"seenAt":0`) {
		t.Fatalf("wait: %d %s", code, body)
	}
	if code, _ := call("POST", platformAPI+"/asks/"+id+"/answer", `{"choice":"changes"}`, page); code != http.StatusConflict {
		t.Fatalf("a second answer: %d", code)
	}

	// Settings: defaults on, a bad change refused with the field.
	code, body = call("GET", platformAPI+"/asks-settings", "", page)
	if code != http.StatusOK || !strings.Contains(body, `"enabled":true`) || !strings.Contains(body, `"Team chat"`) {
		t.Fatalf("settings: %d %s", code, body)
	}
	code, body = call("PUT", platformAPI+"/asks-settings", `{"enabled":true,"defaultTimeoutMin":0,"maxTimeoutMin":10,"perHour":5}`, page)
	if code != http.StatusBadRequest || !strings.Contains(body, "defaultTimeoutMin") {
		t.Fatalf("bad settings: %d %s", code, body)
	}
}
