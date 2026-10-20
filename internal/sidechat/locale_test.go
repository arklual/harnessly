package sidechat

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	// The goldens are the previous runtime's answers, which were in Russian.
	os.Setenv("RUNTIME_LOCALE", "ru")
	os.Exit(m.Run())
}

func TestEnglishRequestErrors(t *testing.T) {
	t.Setenv("RUNTIME_LOCALE", "en")
	for body, want := range map[string]string{
		`1`:                              "JSON is required",
		`{"tabId":"t 1","question":"q"}`: "tabId is required",
		`{"tabId":"t","question":" "}`:   "The question is empty",
		`{"tabId":"t","question":"q","model":"bad model"}`:                   "Unknown model",
		`{"tabId":"t","question":"q","excerpts":[1,2,3,4,5,6]}`:              "At most 5 excerpts",
		`{"tabId":"t","question":"q","excerpts":[{"quote":""}]}`:             "The excerpt is empty",
		`{"tabId":"t","question":"q","excerpts":[{"quote":"a","source":1}]}`: "Invalid excerpt source",
		`{"tabId":"t","question":"q","history":1}`:                           "Invalid history",
	} {
		if _, msg := ParseRequest(parse(t, body)); msg != want {
			t.Errorf("%s: %q, want %q", body, msg, want)
		}
	}
	if got := failureMessage("401 Unauthorized"); got != "Claude is not signed in on this machine" {
		t.Errorf("login: %q", got)
	}
	if got := failureMessage("boom"); got != "Claude could not answer: boom" {
		t.Errorf("failure: %q", got)
	}
	if got := failureMessage(""); got != "Claude could not answer" {
		t.Errorf("empty: %q", got)
	}
	if got := cancelled().Message; got != "Stopped" {
		t.Errorf("cancelled: %q", got)
	}
	t.Setenv("RUNTIME_LOCALE", "ru")
	if got := cancelled().Message; got != "Остановлено" {
		t.Errorf("ru cancelled: %q", got)
	}
}
