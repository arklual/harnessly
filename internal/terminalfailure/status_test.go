package terminalfailure

import (
	"strings"
	"testing"
)

// only what may pass is retried — no answer, a throttle, the vendor's own error.
func TestClassifyStatusTable(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   Retry
		reason string
	}{
		{0, RetryAgain, ""},
		{200, RetryAgain, ""},
		{400, RetryNever, "Неверный запрос"},
		{401, RetryNever, "недействителен"},
		{402, RetryNever, "Оплата"},
		{403, RetryNever, "Нет доступа"},
		{404, RetryNever, "не найдена"},
		{408, RetryAgain, ""},
		{409, RetryAgain, ""},
		{413, RetryNever, "слишком большой"},
		{422, RetryNever, "отклонил"},
		{429, RetryAgain, ""},
		{500, RetryAgain, ""},
		{502, RetryAgain, ""},
		{503, RetryAgain, ""},
		{529, RetryAgain, ""},
	} {
		got, reason := ClassifyStatus(tc.status)
		if got != tc.want || !strings.Contains(reason, tc.reason) || (tc.want == RetryAgain) != (reason == "") {
			t.Errorf("%d: %v %q, want %v %q", tc.status, got, reason, tc.want, tc.reason)
		}
	}
}

func TestDetectorStopsOnTheFirstRetryOfARefusedCall(t *testing.T) {
	for _, tc := range []struct {
		line   string
		status int
	}{
		{"✻ 403 Forbidden · Retrying in 5s · attempt 1/10\r\n", 403},
		{"  ⎿  API Error (403 {\"error\":{\"message\":\"no access\"}}) · Retrying in 1 seconds… (attempt 1/10)\r\n", 403},
		{"  ⎿  API Error: 401 {\"type\":\"authentication_error\"} · Retrying in 2 seconds… (attempt 2/10)\r\n", 401},
		{"✻ 400 invalid_request_error · Retrying in 1s · attempt 1/10\r\n", 400},
	} {
		got, ok := NewDetector().Write([]byte(tc.line))
		if !ok || got.Kind != Rejected || got.Status != tc.status || !got.Terminal || got.NeedsTarget {
			t.Fatalf("%q: %+v %v", tc.line, got, ok)
		}
		if _, reason := ClassifyStatus(tc.status); !strings.HasPrefix(got.Detail, reason) {
			t.Fatalf("%q: the reason leads the detail: %q", tc.line, got.Detail)
		}
	}
}

func TestDetectorLeavesRetriesThatMayPass(t *testing.T) {
	for _, text := range []string{
		"✻ 429 Too Many Requests · Retrying in 5s · attempt 1/10\r\n",
		"  ⎿  API Error (500 Internal Server Error) · Retrying in 1 seconds… (attempt 1/10)\r\n",
		"  ⎿  API Error (529 overloaded_error) · Retrying in 4 seconds… (attempt 3/10)\r\n",
		"  ⎿  API Error (Connection error.) · Retrying in 2 seconds… (attempt 2/10)\r\n",
		// A later retryable notice is the CLI's latest word: an older refusal above it has been retried past.
		"✻ 403 Forbidden · Retrying in 5s · attempt 1/10\r\n✻ 503 Service Unavailable · Retrying in 9s · attempt 2/10\r\n",
		// The person's or the agent's words quoting a notice are not a notice.
		"❯ why do I get «403 Forbidden · Retrying in 5s · attempt 1/10»?\r\n",
		"● The CLI printed 403 · Retrying in 5s · attempt 1/10 earlier.\r\n",
	} {
		if got, ok := NewDetector().Write([]byte(text)); ok {
			t.Errorf("%q: %+v", text, got)
		}
	}
}

func TestObserveScreenSkipsARefusalLeftFromBefore(t *testing.T) {
	line := "✻ 403 Forbidden · Retrying in 5s · attempt 1/10"
	d := NewDetector()
	d.PrimeScreen(line)
	if got, ok := d.ObserveScreen(line + "\n❯ "); ok {
		t.Fatalf("an old notice still on screen: %+v", got)
	}
	d = NewDetector()
	d.PrimeScreen("❯ ")
	if got, ok := d.ObserveScreen(line); !ok || got.Kind != Rejected {
		t.Fatalf("a new notice: %+v %v", got, ok)
	}
}
