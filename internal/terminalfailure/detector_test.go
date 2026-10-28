package terminalfailure

import "testing"

func boolp(v bool) *bool { return &v }

func TestStructuredQuotaNeedsAnExplicitNonTransientSignal(t *testing.T) {
	tests := []struct {
		name string
		in   Structured
		want bool
	}{
		{"non-transient rate limit", Structured{Status: 429, Code: "rate_limit", Transient: boolp(false), Detail: "quota exhausted"}, true},
		{"quota code", Structured{Status: 429, Code: "insufficient_quota", Detail: "no credits"}, true},
		{"transient", Structured{Status: 429, Code: "rate_limit", Transient: boolp(true)}, false},
		{"retry after", Structured{Status: 429, Code: "rate_limit", Transient: boolp(false), RetryAfter: "30"}, false},
		{"bare 429", Structured{Status: 429, Code: "rate_limit"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Classify(tt.in)
			if ok != tt.want {
				t.Fatalf("Classify(%+v) ok=%v, want %v", tt.in, ok, tt.want)
			}
			if ok && (got.Kind != QuotaExhausted || !got.Terminal || !got.NeedsTarget) {
				t.Fatalf("failure: %+v", got)
			}
		})
	}
}

func TestTerminalDetectorStripsANSIJoinsChunksAndEmitsOnce(t *testing.T) {
	d := NewDetector()
	chunks := []string{
		"\x1b[31mAPI Error: 429 {\"error\":{\"type\":\"exceeded_cur",
		"rent_quota_er",
		"ror\",\"message\":\"quota is exhausted\"}}\x1b[0m\r\n",
	}
	for i, chunk := range chunks {
		got, ok := d.Write([]byte(chunk))
		if i < len(chunks)-1 && ok {
			t.Fatalf("detected from incomplete chunk %d: %+v", i, got)
		}
		if i == len(chunks)-1 && (!ok || got.Kind != QuotaExhausted || !got.Terminal || !got.NeedsTarget) {
			t.Fatalf("last chunk: %+v, %v", got, ok)
		}
	}
	if got, ok := d.Write([]byte("insufficient_quota")); ok {
		t.Fatalf("duplicate detection: %+v", got)
	}
}

func TestTerminalDetectorDoesNotPromoteAThrottle(t *testing.T) {
	for _, text := range []string{
		"API Error: 429 rate_limit",
		"429 too many requests; Retry-After: 30",
		`{"apiErrorStatus":429,"error":"rate_limit","apiErrorIsTransient":true}`,
	} {
		d := NewDetector()
		if got, ok := d.Write([]byte(text)); ok {
			t.Errorf("%q: %+v", text, got)
		}
	}
}

func TestTerminalDetectorCanPromoteTheFinalResponseAfterTransientRetries(t *testing.T) {
	d := NewDetector()
	if got, ok := d.Write([]byte(`API Error: 429 {"error":"rate_limit","apiErrorIsTransient":true,"retryAfter":30}` + "\r\n")); ok {
		t.Fatalf("transient retry: %+v", got)
	}
	got, ok := d.Write([]byte(`API Error: 429 {"error":{"type":"exceeded_current_quota_error"},"apiErrorIsTransient":false}` + "\r\n"))
	if !ok || got.Kind != QuotaExhausted {
		t.Fatalf("final non-transient response: %+v, %v", got, ok)
	}
	if duplicate, ok := d.Write([]byte("quota_exhausted\r\n")); ok {
		t.Fatalf("duplicate final response: %+v", duplicate)
	}
}

func TestTerminalDetectorDoesNotPromoteQuotedOrUserText(t *testing.T) {
	for _, text := range []string{
		"quota_exhausted\r\n",
		"Please explain quota_exhausted\r\n",
		"❯ quota_exhausted\r\n",
		"● The string quota_exhausted is an internal error code.\r\n",
		"❯ You've reached your 5-hour usage limit. Your quota will reset when the current window ends.\r\n",
		"● You've reached your 5-hour usage limit. Your quota will reset when the current window ends.\r\n",
		`❯ API Error: 429 {"error":{"type":"quota_exhausted"}}` + "\r\n",
		`● API Error: 429 {"error":{"type":"quota_exhausted"}}` + "\r\n",
		`{"type":"quota_exhausted"}` + "\r\n",
	} {
		d := NewDetector()
		if got, ok := d.Write([]byte(text)); ok {
			t.Errorf("%q: %+v", text, got)
		}
	}
}

func TestTerminalDetectorPromotesKimiUsageLimitRetry(t *testing.T) {
	d := NewDetector()
	text := "✻ 429 You've reached your 5-hour usage limit. Your quota will reset when the current window ends. · Retrying in 23s · attempt 9/10\r\n"
	got, ok := d.Write([]byte(text))
	if !ok || got.Kind != QuotaExhausted || !got.Terminal || !got.NeedsTarget {
		t.Fatalf("screenshot quota line: %+v, %v", got, ok)
	}
}
