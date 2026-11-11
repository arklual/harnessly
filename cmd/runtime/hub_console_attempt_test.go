package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func openTab(t *testing.T, c *hubConsole) string {
	t.Helper()
	rec := c.do("POST", "/agents/api/agent/terminal-tab", `{"command":"echo hi; read -r x"}`, asAgent)
	var tab struct {
		TabID string `json:"tabId"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &tab); err != nil || tab.TabID == "" {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	return tab.TabID
}

func TestHubConsoleAttemptStatesAndCancel(t *testing.T) {
	c, _ := consoleRig(t)
	c.claims = func(r *http.Request) map[string]any { return map[string]any{"sub": r.Header.Get("X-Test-Sub")} }
	vera := map[string]string{"X-Test-Session": "yes", "X-Test-Sub": "vera"}
	anna := map[string]string{"X-Test-Session": "yes", "X-Test-Sub": "anna"}
	note := "/agents/api/agent/console-state"

	id := openTab(t, c)
	other := openTab(t, c)
	screen := "/agents/api/console/" + id + "/screen"

	// Only the machine agent may note a state, and only a known one.
	if rec := c.do("POST", note, `{"tabId":"`+id+`","state":"waiting"}`, vera); rec.Code != 401 {
		t.Errorf("a person noted a state: %d", rec.Code)
	}
	if rec := c.do("POST", note, `{"tabId":"`+id+`","state":"bogus"}`, asAgent); rec.Code != 400 {
		t.Errorf("unknown state: %d", rec.Code)
	}
	rec := c.do("POST", note, `{"tabId":"`+id+`","state":"waiting","provider":"demo","label":"Demo CLI","expiresAt":4102444800000}`, asAgent)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"cancelled":false`) {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	rec = c.do("GET", screen, "", vera)
	if !strings.Contains(rec.Body.String(), `"label":"Demo CLI"`) || !strings.Contains(rec.Body.String(), `"state":"waiting"`) {
		t.Fatalf("attempt not shown: %s", rec.Body.String())
	}

	// Another person cannot cancel it; the person who holds it can, and only this console ends.
	if rec := c.do("POST", "/agents/api/console/"+id+"/cancel", "", anna); rec.Code != 403 {
		t.Errorf("another person cancelled: %d", rec.Code)
	}
	if rec := c.do("POST", "/agents/api/console/"+id+"/cancel", "", vera); rec.Code != 200 {
		t.Fatalf("cancel: %d", rec.Code)
	}
	if c.tm.HasSession(c.sessionName(id)) {
		t.Error("the cancelled console still runs")
	}
	if !c.tm.HasSession(c.sessionName(other)) {
		t.Error("cancel ended another console")
	}
	// The agent learns of the cancel, and cannot turn it into a success afterwards.
	rec = c.do("POST", note, `{"tabId":"`+id+`","state":"connected","label":"Demo CLI"}`, asAgent)
	if !strings.Contains(rec.Body.String(), `"cancelled":true`) {
		t.Errorf("agent not told: %s", rec.Body.String())
	}
	rec = c.do("GET", screen, "", vera)
	if !strings.Contains(rec.Body.String(), `"state":"cancelled"`) {
		t.Errorf("state after cancel: %s", rec.Body.String())
	}

	// A verified connection ends the attempt and closes its terminal only.
	rec = c.do("POST", note, `{"tabId":"`+other+`","state":"connected","label":"Demo CLI"}`, asAgent)
	if rec.Code != 200 || c.tm.HasSession(c.sessionName(other)) {
		t.Errorf("connected: %d, terminal alive %v", rec.Code, c.tm.HasSession(c.sessionName(other)))
	}
}

func TestConsolePageIsGuidedWithTerminalUnderTroubleshooting(t *testing.T) {
	p := consolePage()
	for _, want := range []string{`id="card"`, `id="openBtn"`, `<details id="term"`, `id="cancelBtn"`, `id="cb" hidden`} {
		if !strings.Contains(p, want) {
			t.Errorf("page lacks %s", want)
		}
	}
	if strings.Contains(p, "Machine terminal") {
		t.Error("the raw terminal title is back")
	}
	if i, j := strings.Index(p, `id="screen"`), strings.Index(p, `<details id="term"`); j < 0 || i < j {
		t.Error("the terminal is outside the troubleshooting disclosure")
	}
}
