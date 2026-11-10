package hlcli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// HRN-883: harnessly migrate reads and steers the runtime's move; --json is the runtime's answer as it is.
func TestMigrateCommands(t *testing.T) {
	status := `{"migration":"0001-data-to-hub","total":420,"verified":412,"failed":8,"doc":"docs/migrations/0001-data-to-hub.md","hub":"on",
"failures":[{"kind":"uploads","object":"up_2","state":"failed","code":"MIG-READ","error":"permission denied","attempts":2}]}`
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-cli-token") != "tok" {
			w.WriteHeader(403)
			return
		}
		body, _ := io.ReadAll(r.Body)
		asked = append(asked, r.Method+" "+r.URL.Path+" "+string(body))
		switch r.URL.Path {
		case "/api/cli/migrate":
			_, _ = w.Write([]byte(status))
		case "/api/cli/migrate/retry", "/api/cli/migrate/skip":
			_, _ = w.Write([]byte(`{"marked":1,"status":` + status + `}`))
		case "/api/cli/migrate/verify":
			_, _ = w.Write([]byte(`{"verify":{"checked":412},"status":` + status + `}`))
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)

	r := runConn(t, srv.URL, "migrate", "status")
	if r.code == 0 || !strings.Contains(r.out, "412 of 420 objects in the hub") || !strings.Contains(r.out, "uploads/up_2  MIG-READ  permission denied (2 attempts)") ||
		!strings.Contains(r.out, "harnessly docs read self-host/0001-data-to-hub") {
		t.Fatalf("status: %d %q %q", r.code, r.out, r.errs)
	}
	r = runConn(t, srv.URL, "migrate", "status", "--json")
	var raw map[string]any
	if r.code != 0 || json.Unmarshal([]byte(r.out), &raw) != nil || raw["failed"] != float64(8) {
		t.Fatalf("status --json: %d %q", r.code, r.out)
	}
	if r = runConn(t, srv.URL, "migrate", "skip"); r.code == 0 {
		t.Fatalf("skip of nothing: %d %q", r.code, r.out)
	}
	if r = runConn(t, srv.URL, "migrate", "retry", "nonsense"); r.code == 0 {
		t.Fatalf("retry of no KIND/OBJECT: %d", r.code)
	}
	asked = nil
	r = runConn(t, srv.URL, "migrate", "skip", "handoff/drafts/o1/tab-1/h1.jsonl")
	if !strings.Contains(r.out, "1 objects left on the machine only") || len(asked) != 1 ||
		!strings.Contains(asked[0], `{"objects":[{"kind":"handoff","object":"drafts/o1/tab-1/h1.jsonl"}]}`) {
		t.Fatalf("skip: %q %q", r.out, asked)
	}
	if r = runConn(t, srv.URL, "migrate", "verify"); r.code != 0 || !strings.Contains(r.out, "412 objects asked of the hub, 0 missing") {
		t.Fatalf("verify: %d %q %q", r.code, r.out, r.errs)
	}
}
