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

	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/notifybots"
)

const notifyToken = "123456789:AAH-abcdefghijklmnopqrstuvwxyz0123456"

// fakeBotAPI is getMe and sendMessage of one bot.
func fakeBotAPI(t *testing.T) *[]map[string]any {
	t.Helper()
	var sent []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		switch r.URL.Path {
		case "/bot" + notifyToken + "/getMe":
			_, _ = w.Write([]byte(`{"ok":true,"result":{"id":4242,"is_bot":true,"first_name":"Ping","username":"ping_bot"}}`))
		case "/bot" + notifyToken + "/sendMessage":
			sent = append(sent, body)
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
		default:
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"ok":false,"error_code":401,"description":"Unauthorized"}`))
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("RUNTIME_TELEGRAM_API_URL", srv.URL)
	return &sent
}

func notifyRig(t *testing.T) (func(method, path, body string, h map[string]string) (int, string), string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cli-token"), []byte("tok"), 0o600); err != nil {
		t.Fatal(err)
	}
	d := deps{DataDir: dir, CLI: &gate.CLIToken{DataDir: dir, Headers: []string{"x-cli-token"}},
		SessionOK: func(r *http.Request) bool { return r.Header.Get("X-Test-Session") == "yes" }}
	routes := notifyBotRoutes(d)
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
	}, dir
}

func TestNotifyBotRoutes(t *testing.T) {
	sent := fakeBotAPI(t)
	call, dir := notifyRig(t)
	session := map[string]string{"X-Test-Session": "yes"}
	cli := map[string]string{"x-cli-token": "tok"}

	// Each caller by its own door: the page by its session, an agent by the cli-token.
	if code, _ := call("GET", "/agents/api/platform/notify-bots", "", cli); code != http.StatusUnauthorized {
		t.Fatalf("the cli-token is not a person: %d", code)
	}
	if code, _ := call("GET", "/api/cli/notify/bots", "", session); code != http.StatusForbidden {
		t.Fatalf("a page session is not an agent: %d", code)
	}
	if code, _ := call("POST", "/api/cli/notify/send", `{"bot":"x","text":"y"}`, nil); code != http.StatusForbidden {
		t.Fatalf("send without a token: %d", code)
	}

	code, body := call("POST", "/agents/api/platform/notify-bots", `{"token":"123456789:BBH-abcdefghijklmnopqrstuvwxyz0123456"}`, session)
	if code != http.StatusBadRequest || !strings.Contains(body, `"error":"invalid-token"`) || strings.Contains(body, "BBH-") {
		t.Fatalf("a token Telegram does not know: %d %s", code, body)
	}
	code, body = call("POST", "/agents/api/platform/notify-bots", `{"token":"`+notifyToken+`","instruction":"когда закончил"}`, session)
	var added struct{ Bot notifybots.View }
	_ = json.Unmarshal([]byte(body), &added)
	if code != http.StatusOK || added.Bot.Username != "ping_bot" || !added.Bot.TokenSet || strings.Contains(body, "abcdefghij") {
		t.Fatalf("add: %d %s", code, body)
	}
	id := added.Bot.ID

	code, body = call("GET", "/agents/api/platform/notify-bots", "", session)
	if code != http.StatusOK || !strings.Contains(body, `"username":"ping_bot"`) || strings.Contains(body, "abcdefghij") {
		t.Fatalf("list: %d %s", code, body)
	}
	if code, body := call("POST", "/api/cli/notify/send", `{"bot":"ping","text":"hi"}`, cli); code != http.StatusBadRequest || !strings.Contains(body, "not-bound") {
		t.Fatalf("no chat yet: %d %s", code, body)
	}
	code, body = call("POST", "/agents/api/platform/notify-bots/"+id+"/bind", "", session)
	if code != http.StatusOK || !strings.Contains(body, `"link":"https://t.me/ping_bot?start=`) {
		t.Fatalf("bind: %d %s", code, body)
	}
	if code, body := call("DELETE", "/agents/api/platform/notify-bots/"+id+"/bind", "", session); code != http.StatusOK || !strings.Contains(body, `"binding":null`) {
		t.Fatalf("stop binding: %d %s", code, body)
	}

	// A chat as a binding leaves it.
	if _, err := notifybots.Open(dir).Update(id, func(b *notifybots.Bot) error {
		b.Chat = &notifybots.Chat{ID: 77, Title: "Анна", Type: "private"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	code, body = call("GET", "/api/cli/notify/bots", "", cli)
	if code != http.StatusOK || !strings.Contains(body, `"chat":"Анна"`) || !strings.Contains(body, `"bound":true`) || strings.Contains(body, "abcdefghij") ||
		strings.Contains(body, "77") {
		t.Fatalf("agents' list: %d %s", code, body)
	}
	if code, body := call("POST", "/api/cli/notify/send", `{"bot":"ping","text":"**done**","silent":true}`, cli); code != http.StatusOK || !strings.Contains(body, `"messages":1`) {
		t.Fatalf("send: %d %s", code, body)
	}
	if code, body := call("POST", "/agents/api/platform/notify-bots/"+id+"/test", "", session); code != http.StatusOK {
		t.Fatalf("test: %d %s", code, body)
	}
	if len(*sent) != 2 || (*sent)[0]["text"] != "<b>done</b>" || (*sent)[0]["disable_notification"] != true {
		t.Fatalf("sent: %v", *sent)
	}
	if code, body := call("PUT", "/agents/api/platform/notify-bots/"+id, `{"name":"Алерты"}`, session); code != http.StatusOK || !strings.Contains(body, `"name":"Алерты"`) {
		t.Fatalf("rename: %d %s", code, body)
	}
	if code, _ := call("DELETE", "/agents/api/platform/notify-bots/"+id, "", session); code != http.StatusOK {
		t.Fatalf("remove: %d", code)
	}
	if code, _ := call("DELETE", "/agents/api/platform/notify-bots/"+id, "", session); code != http.StatusNotFound {
		t.Fatalf("remove twice: %d", code)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "v2", notifybots.File))
	if strings.Contains(string(raw), "abcdefghij") {
		t.Fatalf("the removed bot's token stays on disk: %s", raw)
	}
}

func TestWithNotifyBotsPrompt(t *testing.T) {
	dir := t.TempDir()
	store := &notifybots.Store{Path: filepath.Join(dir, "v2", notifybots.File)}
	base := filepath.Join(dir, "workspaces", "ws", "claude-prompt.md")
	if err := os.MkdirAll(filepath.Dir(base), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(base, []byte("# harnessly workspace\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	shared := &notifybots.Shared{CachePath: notifybots.SharedCachePath(dir)}
	prompt := withNotifyBots(store, shared, filepath.Join(dir, "launch-prompts"), func(string) string { return base })
	if got := prompt("ws"); got != base {
		t.Fatalf("no bots: the workspace's own file, got %s", got)
	}
	if err := store.Add(notifybots.Bot{ID: "nb_1", Name: "Алерты", Token: notifyToken, Instruction: "когда закончил",
		Chat: &notifybots.Chat{ID: 1, Title: "Анна"}}); err != nil {
		t.Fatal(err)
	}
	got := prompt("ws")
	raw, err := os.ReadFile(got)
	if err != nil || got == base {
		t.Fatalf("a file of its own: %s %v", got, err)
	}
	text := string(raw)
	if !strings.HasPrefix(text, "# harnessly workspace\n\n## Telegram notifications") || !strings.Contains(text, `**Алерты** (chat "Анна"): когда закончил`) ||
		strings.Contains(text, "AAH-") {
		t.Fatalf("prompt:\n%s", text)
	}
	if again := prompt("ws"); again != got {
		t.Fatalf("the same bots, the same file: %s %s", again, got)
	}

	// The shared bot connected (as last seen): its block, and the person's own bots are off while it is there.
	cache, _ := json.Marshal(notifybots.SharedState{Enabled: true, Bot: "harnessly_bot", Chat: &notifybots.SharedChat{Title: "Анна"}, Instruction: "когда нужен ответ"})
	if err := os.WriteFile(notifybots.SharedCachePath(dir), cache, 0o600); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(prompt("ws"))
	text = string(raw)
	if !strings.Contains(text, `harnessly notify send "<text>"`) || !strings.Contains(text, "когда нужен ответ") || strings.Contains(text, "Алерты") {
		t.Fatalf("shared prompt:\n%s", text)
	}
	t.Setenv("RUNTIME_NOTIFY_OWN_BOTS", "on")
	raw, _ = os.ReadFile(prompt("ws"))
	if text := string(raw); !strings.Contains(text, "когда нужен ответ") || !strings.Contains(text, "Алерты") {
		t.Fatalf("both:\n%s", text)
	}
}

// fakeAgent is the machine agent's loopback /notify/call answering as the cloud would; agent.json in dir points at it.
func fakeAgent(t *testing.T, dir string, answer func(body map[string]any) (int, string)) *[]map[string]any {
	t.Helper()
	var calls []map[string]any
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/notify/call" || r.Header.Get("x-agent-local-token") != "local" {
			w.WriteHeader(403)
			return
		}
		var body map[string]any
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		mu.Lock()
		calls = append(calls, body)
		status, text := answer(body)
		mu.Unlock()
		out, _ := json.Marshal(map[string]any{"status": status, "body": json.RawMessage(text)})
		_, _ = w.Write(out)
	}))
	t.Cleanup(srv.Close)
	port := srv.URL[strings.LastIndex(srv.URL, ":")+1:]
	if err := os.WriteFile(filepath.Join(dir, "agent.json"), []byte(`{"port":`+port+`,"token":"local"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return &calls
}

func TestNotifySharedRoutes(t *testing.T) {
	fakeBotAPI(t)
	call, dir := notifyRig(t)
	session := map[string]string{"X-Test-Session": "yes"}
	cli := map[string]string{"x-cli-token": "tok"}

	// No machine agent: no shared bot, the person's own bots as before.
	code, body := call("GET", "/agents/api/platform/notify-shared", "", session)
	if code != http.StatusOK || !strings.Contains(body, `"enabled":false`) || !strings.Contains(body, `"ownBots":true`) {
		t.Fatalf("no agent: %d %s", code, body)
	}
	if code, body := call("POST", "/api/cli/notify/send", `{"text":"hi"}`, cli); code != http.StatusNotFound || !strings.Contains(body, "no-shared") {
		t.Fatalf("send without a shared bot: %d %s", code, body)
	}

	linked := `{"enabled":true,"bot":"harnessly_bot","chat":{"title":"Анна"},"instruction":"когда закончил","binding":null}`
	calls := fakeAgent(t, dir, func(b map[string]any) (int, string) {
		switch b["action"] {
		case "status", "instruction", "unlink":
			return 200, linked
		case "link":
			return 200, `{"enabled":true,"bot":"harnessly_bot","chat":null,"instruction":"","binding":{"link":"https://t.me/harnessly_bot?start=c0de","expiresAt":1}}`
		case "send":
			return 200, `{"chat":"Анна","messages":1}`
		}
		return 400, `{"error":"bad"}`
	})
	if code, _ := call("GET", "/agents/api/platform/notify-shared", "", cli); code != http.StatusUnauthorized {
		t.Fatalf("the cli-token is not a person: %d", code)
	}
	code, body = call("GET", "/agents/api/platform/notify-shared", "", session)
	if code != http.StatusOK || !strings.Contains(body, `"bot":"harnessly_bot"`) || !strings.Contains(body, `"ownBots":false`) {
		t.Fatalf("status: %d %s", code, body)
	}
	if code, body := call("POST", "/agents/api/platform/notify-bots", `{"token":"`+notifyToken+`"}`, session); code != http.StatusForbidden || !strings.Contains(body, "disabled") {
		t.Fatalf("own bots while the shared one is on: %d %s", code, body)
	}
	if code, body := call("POST", "/agents/api/platform/notify-shared/link", "", session); code != http.StatusOK || !strings.Contains(body, "start=c0de") {
		t.Fatalf("link: %d %s", code, body)
	}
	if code, _ := call("PUT", "/agents/api/platform/notify-shared", `{"instruction":"когда закончил"}`, session); code != http.StatusOK {
		t.Fatalf("instruction: %d", code)
	}
	if code, _ := call("POST", "/agents/api/platform/notify-shared/test", "", session); code != http.StatusOK {
		t.Fatalf("test: %d", code)
	}
	if code, body := call("POST", "/api/cli/notify/send", `{"text":"**done**","silent":true}`, cli); code != http.StatusOK || !strings.Contains(body, `"chat":"Анна"`) {
		t.Fatalf("send: %d %s", code, body)
	}
	code, body = call("GET", "/api/cli/notify/bots", "", cli)
	if code != http.StatusOK || !strings.Contains(body, `"linked":true`) || !strings.Contains(body, `"bots":[]`) {
		t.Fatalf("agents' list: %d %s", code, body)
	}
	if code, _ := call("DELETE", "/agents/api/platform/notify-shared/link", "", session); code != http.StatusOK {
		t.Fatalf("unlink: %d", code)
	}
	var actions []string
	for _, c := range *calls {
		actions = append(actions, c["action"].(string))
		if c["action"] == "send" {
			parts, _ := c["parts"].([]any)
			if len(parts) != 1 {
				t.Errorf("parts: %v", c)
			}
		}
	}
	if got := strings.Join(actions, ","); got != "status,link,instruction,send,status,send,status,unlink" {
		t.Errorf("calls: %s", got)
	}
}
