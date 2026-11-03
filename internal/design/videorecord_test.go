package design

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func TestParseRecordScenarioDefaultsAndYAML(t *testing.T) {
	sc, err := ParseRecordScenario([]byte(`
name: demo
url: https://app.example.com/login
steps:
  - do: click
    on: "text=Sign in"
  - {do: type, into: "#q", text: "hello"}
  - {do: wait, ms: 500}
`))
	if err != nil {
		t.Fatal(err)
	}
	if sc.Viewport.Width != 1920 || sc.Viewport.Height != 1080 || sc.Scale != 2 || sc.FPS != 30 || sc.Theme != "light" {
		t.Fatalf("defaults: %+v", sc)
	}
}

func TestParseRecordScenarioJSON(t *testing.T) {
	if _, err := ParseRecordScenario([]byte(`{"steps":[{"do":"open","url":"http://localhost:3000"},{"do":"key","key":"Enter"}]}`)); err != nil {
		t.Fatal(err)
	}
}

func TestParseRecordScenarioRefuses(t *testing.T) {
	cases := map[string]string{
		"no steps":             `{"url": "http://a.test"}`,
		"unknown field":        `{"url": "http://a.test", "steps": [{"do": "click", "on": "a", "onn": "b"}]}`,
		"unknown verb":         `{"url": "http://a.test", "steps": [{"do": "drag", "on": "a"}]}`,
		"no page opened":       `{"steps": [{"do": "click", "on": "a"}]}`,
		"click without target": `{"url": "http://a.test", "steps": [{"do": "click"}]}`,
		"file address":         `{"steps": [{"do": "open", "url": "file:///etc/passwd"}]}`,
		"text and secret":      `{"url": "http://a.test", "steps": [{"do": "type", "text": "a", "secret": "PW"}]}`,
		"secret is not a name": `{"url": "http://a.test", "steps": [{"do": "type", "secret": "hunter2 is it"}]}`,
		"type nothing":         `{"url": "http://a.test", "steps": [{"do": "type", "into": "#a"}]}`,
		"unknown key":          `{"url": "http://a.test", "steps": [{"do": "key", "key": "Hyper"}]}`,
		"scroll both":          `{"url": "http://a.test", "steps": [{"do": "scroll", "by": 3, "to": "#a"}]}`,
		"too fine a shot":      `{"url": "http://a.test", "viewport": {"width": 3840, "height": 2160}, "scale": 3, "steps": [{"do": "wait", "ms": 10}]}`,
		"bad name":             `{"name": "Bad Name", "url": "http://a.test", "steps": [{"do": "wait", "ms": 10}]}`,
		"wait without time":    `{"url": "http://a.test", "steps": [{"do": "wait"}]}`,
		"theme":                `{"url": "http://a.test", "theme": "sepia", "steps": [{"do": "wait", "ms": 10}]}`,
		"negative pause":       `{"url": "http://a.test", "steps": [{"do": "wait", "ms": 10, "pause": -1}]}`,
		"not a document":       `: : :`,
	}
	for name, src := range cases {
		if _, err := ParseRecordScenario([]byte(src)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestRecordLogRedactsSecrets(t *testing.T) {
	lg := RecordLog{Events: []RecordEvent{
		{Type: "open", URL: "https://x.test/login?token=Sup3rSecret%21&a=1", Title: "Sup3rSecret! page"},
		{Type: "type", Text: "my Sup3rSecret! is here", On: "#Sup3rSecret!"},
		{Type: "click", Label: "ok"},
	}}
	lg.redact([]string{"Sup3rSecret!", "ab"})
	raw, _ := json.Marshal(lg)
	if strings.Contains(string(raw), "Sup3rSecret") {
		t.Fatalf("a secret survived: %s", raw)
	}
	if !strings.Contains(string(raw), RecordMask) || !strings.Contains(string(raw), `"label":"ok"`) {
		t.Fatalf("masking lost the shape: %s", raw)
	}
}

func TestRecordLogRedactsEveryFormOfASecret(t *testing.T) {
	const secret = `p@ss w"rd/9&x`
	forms := []string{secret, url.QueryEscape(secret), url.PathEscape(secret), strings.ReplaceAll(url.QueryEscape(secret), "+", "%20"),
		strings.ToLower(url.QueryEscape(secret))}
	j, _ := json.Marshal(secret)
	forms = append(forms, string(j[1:len(j)-1]))
	for _, f := range forms {
		lg := RecordLog{Events: []RecordEvent{{Type: "open", URL: "https://x.test/a/" + f + "?k=" + f + "#" + f, Title: "t " + f, Text: f}}}
		lg.redact([]string{secret})
		raw, _ := json.Marshal(lg)
		if leaks(raw, []string{secret}) {
			t.Errorf("form %q survived: %s", f, raw)
		}
		if lg.Events[0].URL != "https://x.test/a/"+RecordMask {
			t.Errorf("address %q", lg.Events[0].URL)
		}
	}
	// the last check sees a secret that no masking caught
	for _, f := range forms[:5] {
		raw, _ := json.Marshal(RecordLog{Events: []RecordEvent{{Type: "key", Text: "a " + f + " b"}}})
		if !leaks(raw, []string{secret}) {
			t.Errorf("leak of %q not seen", f)
		}
	}
}
