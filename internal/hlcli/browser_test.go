package hlcli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBrowserHelpAndArguments(t *testing.T) {
	for _, args := range [][]string{{"browser", "--help"}, {"help", "browser"}, {"browser"}} {
		r := runWith(t, t.TempDir(), nil, "", args...)
		if r.code != 0 || !strings.Contains(r.out, "browser <tool>") || !strings.Contains(r.out, "snapshot") {
			t.Fatalf("help: %+v", r)
		}
	}
	if !strings.Contains(helpText, "harnessly browser") {
		t.Fatal("missing main help")
	}
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/cli/browser/browser_navigate" {
			t.Errorf("wrong path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"opened"}]}`))
	}))
	defer server.Close()
	r := runWith(t, t.TempDir(), map[string]string{"HARNESSLY_URL": server.URL, "HARNESSLY_CLI_TOKEN": "fixture"}, "hs-me", "-w", "workspace", "browser", "navigate", "--json", `{"url":"http://localhost:4400"}`)
	if r.code != 0 {
		t.Fatalf("call: %+v", r)
	}
	if body["session"] != "hs-me" {
		t.Fatal(body)
	}
	arguments := body["arguments"].(map[string]any)
	if arguments["url"] != "http://localhost:4400" {
		t.Fatal(arguments)
	}
}
