package limits

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The answer of 15.11.2026 for a Max plan on Fable, trimmed: no five hours, the week, Fable's own week.
const claudeUsageAnswer = `{"five_hour": null,
 "seven_day": {"utilization": 58.0, "resets_at": "2026-11-21T11:00:00.331895+00:00", "limit_dollars": null},
 "seven_day_opus": null, "seven_day_sonnet": null, "extra_usage": null,
 "limits": [
  {"kind": "weekly_all", "group": "weekly", "percent": 58, "resets_at": "2026-11-21T11:00:00.331895+00:00", "scope": null},
  {"kind": "weekly_scoped", "group": "weekly", "percent": 20, "resets_at": "2026-11-21T11:00:00.332056+00:00",
   "scope": {"model": {"id": null, "display_name": "Fable"}, "surface": null}}]}`

func parse(t *testing.T, raw string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestParseClaudeUsage(t *testing.T) {
	now := 1794775000.0
	d := ParseClaudeUsage(parse(t, claudeUsageAnswer), now)
	if d == nil {
		t.Fatal("no data")
	}
	if d["five_hour"] != nil {
		t.Fatalf("five hours: %#v, want null", d["five_hour"])
	}
	week, _ := object(d["seven_day"])
	if week["used_percentage"] != 58.0 || week["source"] != SourceClaudeUsage || week["at"] != now {
		t.Fatalf("week: %#v", week)
	}
	if r := num(week["resets_at"]); r < 1795258800 || r > 1795258801 {
		t.Fatalf("week reset: %v", r)
	}
	extra, _ := d["extra"].([]any)
	if len(extra) != 1 {
		t.Fatalf("extra: %#v", d["extra"])
	}
	fable, _ := object(extra[0])
	w, _ := object(fable["window"])
	if fable["id"] != "seven_day_model_fable" || fable["title"] != "Fable" || w["used_percentage"] != 20.0 {
		t.Fatalf("fable: %#v", fable)
	}
	if ParseClaudeUsage(parse(t, `{"error":"x"}`), now) != nil {
		t.Fatal("an answer without windows is not usage")
	}
}

func TestParseClaudeUsageFiveHours(t *testing.T) {
	d := ParseClaudeUsage(parse(t, `{"five_hour":{"utilization":11,"resets_at":"2026-11-15T23:00:00Z"},"seven_day":null}`), 1)
	five, _ := object(d["five_hour"])
	if five["used_percentage"] != 11.0 || num(five["resets_at"]) != float64(time.Date(2026, 11, 15, 23, 0, 0, 0, time.UTC).Unix()) {
		t.Fatalf("five: %#v", five)
	}
}

// A five hours the statusline left from before stays no longer: the usage answer lists no such window.
func TestClaudeUsageClearsGoneWindow(t *testing.T) {
	cache := &Cache{File: filepath.Join(t.TempDir(), "rate-limits.json")}
	stale := Data{"ts": 1794548516.0, "seenAt": 1794774990.0,
		"five_hour": map[string]any{"used_percentage": 11.0, "resets_at": 1794558600.0, "at": 1794548516.0},
		"seven_day": map[string]any{"used_percentage": 55.0, "resets_at": 1795258800.0, "at": 1794774939.0}}
	if _, err := cache.Write("claude", stale); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.Write("claude", ParseClaudeUsage(parse(t, claudeUsageAnswer), 1794775000)); err != nil {
		t.Fatal(err)
	}
	got := cache.Read()["claude"]
	if got["five_hour"] != nil {
		t.Fatalf("five hours kept: %#v", got["five_hour"])
	}
	if w, _ := object(got["seven_day"]); w["used_percentage"] != 58.0 {
		t.Fatalf("week: %#v", got["seven_day"])
	}
	if got["seenAt"] != 1794774990.0 {
		t.Fatalf("seenAt lost: %#v", got["seenAt"])
	}
	// The statusline after it brings the week alone: the five hours stay gone.
	if _, err := cache.Write("claude", Data{"ts": 1794775400.0, "seenAt": 1794775400.0, "five_hour": nil,
		"seven_day": map[string]any{"used_percentage": 59.0, "resets_at": 1795258800.0}}); err != nil {
		t.Fatal(err)
	}
	got = cache.Read()["claude"]
	if got["five_hour"] != nil || got["seenAt"] != 1794775400.0 {
		t.Fatalf("after the statusline: %#v", got)
	}
}

func TestClaudeActive(t *testing.T) {
	if claudeActive(Data{"seenAt": 1000.0}, 1000+ClaudeActiveSec+1) {
		t.Fatal("an old statusline is not a running chat")
	}
	if !claudeActive(Data{"seenAt": 1000.0}, 1100) || claudeActive(nil, 1) || claudeActive(Data{}, 1) {
		t.Fatal("active")
	}
}

func TestFetchClaudeUsage(t *testing.T) {
	status, after := http.StatusOK, ""
	var auth, beta string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, beta = r.Header.Get("Authorization"), r.Header.Get("anthropic-beta")
		if after != "" {
			w.Header().Set("Retry-After", after)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(claudeUsageAnswer))
	}))
	defer srv.Close()
	login := ClaudeLogin{AccessToken: "tok"}
	d, err := FetchClaudeUsage(context.Background(), login, srv.Client(), srv.URL, 5)
	if err != nil || d == nil || auth != "Bearer tok" || beta != claudeOAuthBeta {
		t.Fatalf("ok: %v %v %q %q", d, err, auth, beta)
	}
	var refused *errClaudeUsage
	status = http.StatusUnauthorized
	if _, err := FetchClaudeUsage(context.Background(), login, srv.Client(), srv.URL, 5); !errors.As(err, &refused) || refused.wait != claudeAuthBackoff {
		t.Fatalf("401: %v", err)
	}
	status, after = http.StatusTooManyRequests, "3600"
	if _, err := FetchClaudeUsage(context.Background(), login, srv.Client(), srv.URL, 5); !errors.As(err, &refused) || refused.wait != time.Hour {
		t.Fatalf("429: %v", err)
	}
}

func TestReadClaudeLogin(t *testing.T) {
	empty, dir := t.TempDir(), t.TempDir()
	if err := writeFile(filepath.Join(dir, ".credentials.json"), `{"claudeAiOauth":{"accessToken":"a","expiresAt":5}}`); err != nil {
		t.Fatal(err)
	}
	got := ReadClaudeLogin("", empty, dir)
	if got == nil || got.AccessToken != "a" || got.ExpiresAt != 5 {
		t.Fatalf("login: %#v", got)
	}
	if ReadClaudeLogin(empty) != nil {
		t.Fatal("no sign-in")
	}
}

func writeFile(path, body string) error { return os.WriteFile(path, []byte(body), 0o600) }
