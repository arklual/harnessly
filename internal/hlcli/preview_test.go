package hlcli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type seen struct {
	method, path, query string
	body                map[string]any
}

// previewServer answers every request with answer and records the last one.
func previewServer(t *testing.T, answer string, last *seen) map[string]string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		*last = seen{method: r.Method, path: r.URL.Path, query: r.URL.RawQuery}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &last.body); err != nil {
				t.Error(err)
			}
		}
		_, _ = w.Write([]byte(answer))
	}))
	t.Cleanup(server.Close)
	return map[string]string{"HARNESSLY_URL": server.URL, "HARNESSLY_CLI_TOKEN": "fixture"}
}

func TestPreviewRequests(t *testing.T) {
	settings := `{"slot":"localhost:4400","target":{"kind":"port","port":4400},"settings":{"throttle":{"preset":"none"},"blocked":[],"disableCache":false}}`
	cases := []struct {
		args         []string
		method, path string
		query        string
		body         string
	}{
		{[]string{"preview", "slots"}, "GET", "/api/cli/preview/slots", "", ""},
		{[]string{"preview", "network", "--errors", "--url", "api", "--since=5", "--slot", "localhost:4400"}, "GET", "/api/cli/preview/network", "errors=1&since=5&slot=localhost%3A4400&url=api", ""},
		{[]string{"preview", "network", "--third-party", "--limit", "10"}, "GET", "/api/cli/preview/network", "limit=10&thirdParty=1", ""},
		{[]string{"preview", "console", "--level", "warn"}, "GET", "/api/cli/preview/console", "level=warn", ""},
		{[]string{"preview", "server-log", "--all", "--lines", "30"}, "GET", "/api/cli/preview/server-log", "all=1&lines=30", ""},
		{[]string{"preview", "clear"}, "DELETE", "/api/cli/preview/clear", "", ""},
		{[]string{"preview", "throttle", "slow-4g"}, "PATCH", "/api/cli/preview/settings", "", `{"throttle":{"preset":"slow-4g"}}`},
		{[]string{"preview", "throttle", "custom", "--latency", "100", "--down", "0", "--up", "50"}, "PATCH", "/api/cli/preview/settings", "",
			`{"throttle":{"downKbps":0,"latencyMs":100,"preset":"custom","upKbps":50}}`},
		{[]string{"preview", "block", "posthog", "*.google-analytics.com/*"}, "PATCH", "/api/cli/preview/settings", "", `{"blockedAdd":["posthog","*.google-analytics.com/*"]}`},
		{[]string{"preview", "unblock", "posthog"}, "PATCH", "/api/cli/preview/settings", "", `{"blockedRemove":["posthog"]}`},
		{[]string{"preview", "unblock", "--all"}, "PATCH", "/api/cli/preview/settings", "", `{"blocked":[]}`},
		{[]string{"preview", "cache", "off"}, "PATCH", "/api/cli/preview/settings", "", `{"disableCache":true}`},
		{[]string{"preview", "cache", "on"}, "PATCH", "/api/cli/preview/settings", "", `{"disableCache":false}`},
	}
	for _, tc := range cases {
		var last seen
		env := previewServer(t, settings, &last)
		r := runWith(t, t.TempDir(), env, "hs-me", append([]string{"--json"}, tc.args...)...)
		if r.code != 0 {
			t.Fatalf("%v: %+v", tc.args, r)
		}
		if last.method != tc.method || last.path != tc.path || last.query != tc.query {
			t.Fatalf("%v: sent %+v", tc.args, last)
		}
		if tc.body != "" {
			got, _ := json.Marshal(last.body)
			if string(got) != tc.body {
				t.Fatalf("%v: body %s, want %s", tc.args, got, tc.body)
			}
		}
	}
}

func TestPreviewUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		{"preview", "frobnicate"},
		{"preview", "slots", "extra"},
		{"preview", "network", "--bogus"},
		{"preview", "network", "--since", "x"},
		{"preview", "network", "--limit"},
		{"preview", "throttle"},
		{"preview", "throttle", "warp"},
		{"preview", "throttle", "3g", "--down", "5"},
		{"preview", "throttle", "custom", "--latency", "5"},
		{"preview", "block"},
		{"preview", "unblock"},
		{"preview", "unblock", "--all", "x"},
		{"preview", "cache", "maybe"},
	} {
		var last seen
		env := previewServer(t, `{}`, &last)
		r := runWith(t, t.TempDir(), env, "hs-me", args...)
		if r.code == 0 || last.path != "" {
			t.Fatalf("%v: should refuse before asking the runtime: %+v %+v", args, r, last)
		}
	}
	r := runWith(t, t.TempDir(), nil, "", "preview", "--help")
	if r.code != 0 || !strings.Contains(r.out, "preview throttle") {
		t.Fatalf("help: %+v", r)
	}
}

func TestPreviewFormatsAreSafeText(t *testing.T) {
	var last seen
	answer := `{"slot":"localhost:4400","target":{"kind":"port","port":4400},"cursor":7,"matched":2,
"settings":{"throttle":{"preset":"slow-4g","latencyMs":400,"downKbps":400,"upKbps":0},"blocked":["posthog"],"disableCache":true},
"entries":[{"id":1,"at":0,"method":"GET","url":"http://a/\u001b[31mred\nline","status":200,"type":"fetch","size":2500,"durationMs":1500,"thirdParty":true},
{"id":2,"at":0,"method":"POST","url":"http://b/x","status":null,"type":"xhr","size":null,"durationMs":null,"error":"net::ERR"}]}`
	env := previewServer(t, answer, &last)
	r := runWith(t, t.TempDir(), env, "hs-me", "preview", "network")
	if r.code != 0 {
		t.Fatal(r)
	}
	for _, want := range []string{"data for diagnosis", "localhost:4400", "2 of 2 requests, cursor 7", "␛[31mred line", "[3rd-party]", "2.5 kB", "1.5 s",
		"(failed)", "[error: net::ERR]", "cache off"} {
		if !strings.Contains(r.out, want) {
			t.Errorf("missing %q in:\n%s", want, r.out)
		}
	}
	if strings.ContainsRune(r.out, 0x1b) {
		t.Fatal("raw ESC reached the terminal")
	}

	env = previewServer(t, `{"slot":"s","target":{"kind":"origin","origin":"https://x.example"},"changes":{"added":["a"],"removed":[],"missing":["b"]},
"settings":{"throttle":{"preset":"custom","latencyMs":100,"downKbps":0,"upKbps":50},"blocked":["a"],"disableCache":false}}`, &last)
	r = runWith(t, t.TempDir(), env, "hs-me", "preview", "block", "a")
	for _, want := range []string{"https://x.example", "blocked now: a", "was not blocked: b", "custom (latency 100 ms, down unlimited, up 50 kbps)", "blocked: a"} {
		if !strings.Contains(r.out, want) {
			t.Errorf("missing %q in:\n%s", want, r.out)
		}
	}

	env = previewServer(t, `{"slot":"s","target":null,"source":null,"reason":"no terminal runs the server","lines":[]}`, &last)
	r = runWith(t, t.TempDir(), env, "hs-me", "preview", "server-log")
	if !strings.Contains(r.out, "target unknown") || !strings.Contains(r.out, "No terminal to read: no terminal runs the server") {
		t.Fatal(r.out)
	}
}

func TestMemIsMemory(t *testing.T) {
	a := runWith(t, t.TempDir(), nil, "", "mem", "bogus")
	b := runWith(t, t.TempDir(), nil, "", "memory", "bogus")
	if a.code != b.code || a.errs != b.errs || !strings.Contains(a.errs, "memory takes status or sync") {
		t.Fatalf("mem=%+v memory=%+v", a, b)
	}
}
