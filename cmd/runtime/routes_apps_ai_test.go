package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/apps"
)

// an app spends the person's model only with its right and within its budget of the day; a call that would
// go past it is refused before the model is asked, and so is a chat past the day's number.
func TestAppAIAndRunRoutes(t *testing.T) {
	call, conv := appRig(t)
	conv.Apps = []string{"acme.demo"}
	conv.Cwd = t.TempDir()
	page := map[string]string{"X-Test-Session": "yes"}
	pkg := filepath.Join(t.TempDir(), "demo")
	if err := apps.Scaffold(pkg, "acme.demo", "Демо", "pane-chat"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(pkg, apps.ManifestPath))
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	m["permissions"] = []string{"ai", "agents.run"}
	m["budget"] = map[string]any{"usdPerDay": 0.03, "runsPerDay": 1}
	raw, _ = json.Marshal(m)
	if err := os.WriteFile(filepath.Join(pkg, apps.ManifestPath), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if code, body := call("POST", "/api/cli/apps/install", `{"dir":"`+pkg+`"}`, map[string]string{"x-cli-token": "tok"}); code != http.StatusOK {
		t.Fatalf("install: %d %s", code, body)
	}
	agreeRig(t)
	_, body := call("POST", platformAPI+"/apps/session", `{"app":"acme.demo","view":"main","target":"pane","conversation":"tab-1"}`, page)
	var sess struct{ Session string }
	_ = json.Unmarshal([]byte(body), &sess)

	asked := 0
	was := appAIRun
	appAIRun = func(context.Context, string, []string, []string, string) ([]byte, error) {
		asked++
		return []byte(`{"type":"result","is_error":false,"result":"ok","total_cost_usd":0.004}`), nil
	}
	t.Cleanup(func() { appAIRun = was })
	ai := func(rest string) (int, string) {
		return call("POST", platformAPI+"/apps/ai", `{"session":"`+sess.Session+`",`+rest+`}`, page)
	}
	if code, body := ai(`"prompt":"Привет","maxTokens":200`); code != http.StatusOK || !strings.Contains(body, `"text":"ok"`) {
		t.Fatalf("first call: %d %s", code, body)
	}
	if code, body := ai(`"prompt":"x","model":"opus"`); code != http.StatusBadRequest || asked != 1 {
		t.Fatalf("a model it may not ask for: %d %s", code, body)
	}
	// Spend until the worst of the next call does not fit: refused, and the model is not asked.
	var code int
	for range 20 {
		if code, body = ai(`"prompt":"Привет","maxTokens":200`); code != http.StatusOK {
			break
		}
	}
	before := asked
	if code != http.StatusForbidden || !strings.Contains(body, apps.CodeQuotaExceeded) {
		t.Fatalf("over budget: %d %s", code, body)
	}
	if code, _ := ai(`"prompt":"Привет","maxTokens":200`); code != http.StatusForbidden || asked != before {
		t.Fatalf("a refused call asked the model: %d", code)
	}
	if s := apps.Open(decRigDir).SpentToday("acme.demo", nowUTC()); s.USD > 0.03 {
		t.Fatalf("spent %v of 0.03", s.USD)
	}

	run := func(rest string) (int, string) {
		return call("POST", platformAPI+"/apps/agents/run", `{"session":"`+sess.Session+`",`+rest+`}`, page)
	}
	if code, body := run(`"prompt":"Проверь тесты\nи доложи"`); code != http.StatusOK || !strings.Contains(body, "tab-new") {
		t.Fatalf("run: %d %s", code, body)
	}
	if len(rigCreated) != 1 || rigCreated[0].ParentID != "tab-1" || rigCreated[0].Cwd != conv.Cwd ||
		rigCreated[0].Title != "Демо: Проверь тесты" || rigCreated[0].First.Text != "Проверь тесты\nи доложи" {
		t.Fatalf("created %+v", rigCreated)
	}
	if code, body := run(`"prompt":"Ещё"`); code != http.StatusForbidden || !strings.Contains(body, apps.CodeQuotaExceeded) || len(rigCreated) != 1 {
		t.Fatalf("second run past the day's one: %d %s", code, body)
	}
	list, _ := apps.Open(decRigDir).ReadJournal("acme.demo", 50)
	var whats []string
	for _, e := range list {
		whats = append(whats, e.What)
	}
	if j := strings.Join(whats, " "); !strings.Contains(j, "ai") || !strings.Contains(j, "agents.run") {
		t.Fatalf("journal: %s", j)
	}
}

func nowUTC() time.Time { return time.Now().UTC() }
