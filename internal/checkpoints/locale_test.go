package checkpoints

import (
	"net/http"
	"testing"
)

func TestRefusalsFollowTheLocale(t *testing.T) {
	a := &API{}
	for lang, want := range map[string]string{"en": "application/json is required", "ru": "Нужен application/json"} {
		t.Setenv("RUNTIME_LOCALE", lang)
		status, body, _ := a.Serve(Request{Method: http.MethodPost, ContentType: "text/plain"})
		if status != http.StatusUnsupportedMediaType || errText(body) != want {
			t.Errorf("%s: %d %v", lang, status, body)
		}
		status, body, _ = a.Serve(Request{Method: http.MethodGet, ContentType: ""})
		if status != http.StatusBadRequest || errText(body) != map[string]string{"en": "tabId is required", "ru": "Нужен tabId"}[lang] {
			t.Errorf("%s no tab: %d %v", lang, status, body)
		}
	}
	t.Setenv("RUNTIME_LOCALE", "en")
	if got := busyText.String(); got != "The agent is working: wait for the turn to end" {
		t.Errorf("busy: %q", got)
	}
}

func errText(body any) string {
	if o, ok := body.(interface{ String(string) string }); ok {
		return o.String("error")
	}
	return ""
}
