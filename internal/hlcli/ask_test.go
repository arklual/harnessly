package hlcli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// askRuntime answers the ask routes: a new ask, then a wait that is still waiting once and closes the next time.
type askRuntime struct {
	mu      sync.Mutex
	created map[string]any
	waits   int
	outcome string // the closed ask's JSON fields after "status"
}

func (a *askRuntime) serve(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	a.mu.Lock()
	defer a.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	deadline := time.Now().Add(30 * time.Minute).UnixMilli()
	head := `{"ask":{"id":"ask_0123456789ab","kind":"approval","question":"Ship?","to":{"username":"anna_dev"},"chat":"Team","deadline":` +
		jsonInt(deadline) + `,`
	switch {
	case r.URL.Path == "/api/cli/asks" && r.Method == "POST":
		_ = json.Unmarshal(raw, &a.created)
		if a.created["to"] == "@stranger" {
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":"unknown-person"}`))
			return
		}
		_, _ = w.Write([]byte(head + `"status":"waiting","replies":[]}}`))
	case strings.HasSuffix(r.URL.Path, "/wait"):
		a.waits++
		if a.waits == 1 {
			_, _ = w.Write([]byte(head + `"status":"waiting","replies":[]}}`))
			return
		}
		_, _ = w.Write([]byte(head + a.outcome + `}}`))
	default:
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"error":"Not found"}`))
	}
}

func jsonInt(n int64) string { b, _ := json.Marshal(n); return string(b) }

func runAsk(t *testing.T, outcome string, args ...string) (result, *askRuntime) {
	t.Helper()
	rt := &askRuntime{outcome: outcome}
	srv := httptest.NewServer(http.HandlerFunc(rt.serve))
	t.Cleanup(srv.Close)
	env := map[string]string{"HARNESSLY_URL": srv.URL, "HARNESSLY_CLI_TOKEN": "tok"}
	return runWith(t, t.TempDir(), env, "hs-me", args...), rt
}

func TestAskApprovalOutcomes(t *testing.T) {
	approved := `"status":"answered","decision":"approved","answer":{"from":{"username":"anna_dev"},"text":"","choice":"approved","via":"telegram"},` +
		`"replies":[{"from":{"username":"anna_dev"},"choice":"approved","via":"telegram"},{"from":{"username":"lead"},"text":"nice","via":"telegram"}]`
	r, rt := runAsk(t, approved, "ask", "approve", "--to", "Anna", "--link", "https://example.com/rfc", "--timeout", "30m", "Ship", "it?")
	if r.code != 0 || !strings.HasPrefix(r.out, "asked ask_0123456789ab → @anna_dev in «Team»") || !strings.Contains(r.out, "approved by @anna_dev") ||
		!strings.Contains(r.out, "also @lead: nice") {
		t.Fatalf("approved: %d %q %q", r.code, r.out, r.errs)
	}
	if rt.created["kind"] != "approval" || rt.created["to"] != "Anna" || rt.created["question"] != "Ship it?" ||
		rt.created["timeoutSec"] != float64(1800) || rt.created["session"] != "hs-me" {
		t.Fatalf("request: %+v", rt.created)
	}
	changes := `"status":"answered","decision":"changes","answer":{"from":{"username":"anna_dev"},"choice":"changes","via":"web"},"replies":[]`
	if r, _ := runAsk(t, changes, "ask", "approve", "--to", "Anna", "x"); r.code != exitNotApproved || !strings.Contains(r.out, "(in harnessly)") {
		t.Fatalf("changes: %d %q", r.code, r.out)
	}
	if r, _ := runAsk(t, `"status":"timeout","replies":[]`, "ask", "--to", "Anna", "x"); r.code != exitNoAnswer || !strings.Contains(r.out, "no answer in time") {
		t.Fatalf("timeout: %d %q", r.code, r.out)
	}
}

func TestAskQuestionWithOptionsAndRefusal(t *testing.T) {
	answered := `"status":"answered","answer":{"from":{"username":"anna_dev"},"text":"Tomorrow","choice":"Tomorrow","via":"telegram"},"replies":[]`
	r, rt := runAsk(t, answered, "ask", "--to=Anna", "-o", "Today", "-o", "Tomorrow", "When?")
	if r.code != 0 || !strings.Contains(r.out, "answered by @anna_dev: Tomorrow") {
		t.Fatalf("answer: %d %q %q", r.code, r.out, r.errs)
	}
	if opts, _ := rt.created["options"].([]any); len(opts) != 2 || rt.created["kind"] != "question" {
		t.Fatalf("request: %+v", rt.created)
	}
	r, _ = runAsk(t, "", "ask", "--to", "@stranger", "hi")
	if r.code != 1 || !strings.Contains(r.errs, "refused: unknown-person") {
		t.Fatalf("refusal: %d %q", r.code, r.errs)
	}
	if r, _ := runAsk(t, "", "ask", "--to", "Anna"); r.code != exitUsage {
		t.Fatalf("no question: %d", r.code)
	}
}
