package limits

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Claude subscription usage straight from Anthropic, the request Claude Code's own /usage makes (2.1.325:
// `GET /api/oauth/usage?skip_spend=1` with the sign-in's bearer and the OAuth beta header). The statusline carries only
// the windows the CLI saw on its last answer, and since 13.11 a plan without five hours sends the week alone, so a copy
// of the five hours from before stayed in the file for days. This answer lists every window the plan has: a window it
// does not list is gone. The weekly limits of one model («Fable») come as extra windows. The sign-in is only read:
// Claude Code refreshes its own token, and nothing here writes, logs or sends it anywhere but to Anthropic.

const (
	SourceClaudeUsage = "claude-usage"
	ClaudeUsageURL    = "https://api.anthropic.com/api/oauth/usage?skip_spend=1"
	// ClaudeUsageInterval: the CLI asks on its own /usage only; a few minutes keeps the numbers fresh without weight.
	ClaudeUsageInterval = 5 * time.Minute
	// ClaudeActiveSec: only while a Claude chat runs (its statusline came lately) is the account asked at all.
	ClaudeActiveSec   = 10 * 60
	claudeUsageTimout = 15 * time.Second
	claudeOAuthBeta   = "oauth-2025-04-20"
	// After a refusal the account is left alone: an expired sign-in until Claude Code renews it, a 429 at least as long as
	// the answer asks.
	claudeAuthBackoff  = 30 * time.Minute
	claudeLimitBackoff = 15 * time.Minute
	claudeMaxBackoff   = time.Hour
)

// ClaudeLogin is the bearer of the machine's Claude sign-in and when it expires (epoch ms; 0 when unknown).
type ClaudeLogin struct {
	AccessToken string
	ExpiresAt   float64
}

// ReadClaudeLogin is the first sign-in found in the folders, in order (Claude Code's own, then the machine's managed
// sign-in); nil without one.
func ReadClaudeLogin(dirs ...string) *ClaudeLogin {
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, ".credentials.json"))
		if err != nil {
			continue
		}
		var creds struct {
			ClaudeAiOauth *struct {
				AccessToken string  `json:"accessToken"`
				ExpiresAt   float64 `json:"expiresAt"`
			} `json:"claudeAiOauth"`
		}
		if json.Unmarshal(raw, &creds) != nil || creds.ClaudeAiOauth == nil || creds.ClaudeAiOauth.AccessToken == "" {
			continue
		}
		return &ClaudeLogin{AccessToken: creds.ClaudeAiOauth.AccessToken, ExpiresAt: creds.ClaudeAiOauth.ExpiresAt}
	}
	return nil
}

// isoSeconds is an ISO 8601 time as epoch seconds; NaN when it is not one.
func isoSeconds(v any) (float64, bool) {
	s, ok := v.(string)
	if !ok || s == "" {
		return 0, false
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return 0, false
	}
	return float64(t.UnixMilli()) / 1000, true
}

// claudeWindow is `{utilization, resets_at}` as the file keeps a window; nil when the answer has none.
func claudeWindow(v any, now float64) map[string]any {
	w, ok := object(v)
	if !ok || !finite(w["utilization"]) {
		return nil
	}
	reset, ok := isoSeconds(w["resets_at"])
	if !ok {
		return nil
	}
	return map[string]any{"used_percentage": w["utilization"], "resets_at": reset, "at": now, "source": SourceClaudeUsage}
}

// windowOrNull stores a missing window as JSON null, not as a nil map (which reads back as an object).
func windowOrNull(w map[string]any) any {
	if w == nil {
		return nil
	}
	return w
}

var nonWord = regexp.MustCompile(`[^a-z0-9]+`)

// ParseClaudeUsage is the plan's windows as the answer has them: the five hours and the week, null when the plan has
// none, and the weekly limit of a model as an extra window titled by the model. Nil when the answer is not usage.
func ParseClaudeUsage(body any, now float64) Data {
	b, ok := object(body)
	if !ok {
		return nil
	}
	_, hasFive := b["five_hour"]
	_, hasWeek := b["seven_day"]
	list, hasList := b["limits"].([]any)
	if !hasFive && !hasWeek && !hasList {
		return nil
	}
	data := Data{"ts": now, "source": SourceClaudeUsage, "five_hour": windowOrNull(claudeWindow(b["five_hour"], now)),
		"seven_day": windowOrNull(claudeWindow(b["seven_day"], now))}
	extra := []any{}
	seen := map[string]bool{}
	add := func(title string, window map[string]any) {
		id := "seven_day_model_" + strings.Trim(nonWord.ReplaceAllString(strings.ToLower(title), "_"), "_")
		if title == "" || window == nil || seen[id] {
			return
		}
		seen[id] = true
		extra = append(extra, map[string]any{"id": id, "title": title, "window": window})
	}
	for _, raw := range list {
		l, ok := object(raw)
		if !ok || l["kind"] != "weekly_scoped" || !finite(l["percent"]) {
			continue
		}
		scope, _ := object(l["scope"])
		model, _ := object(scope["model"])
		name, _ := model["display_name"].(string)
		add(name, claudeWindow(map[string]any{"utilization": l["percent"], "resets_at": l["resets_at"]}, now))
	}
	// The older shape names the models' weeks by key.
	for key, title := range map[string]string{"seven_day_opus": "Opus", "seven_day_sonnet": "Sonnet"} {
		add(title, claudeWindow(b[key], now))
	}
	if data["seven_day"] == nil {
		for _, raw := range list {
			if l, ok := object(raw); ok && l["kind"] == "weekly_all" {
				data["seven_day"] = windowOrNull(claudeWindow(map[string]any{"utilization": l["percent"], "resets_at": l["resets_at"]}, now))
			}
		}
	}
	data["extra"] = extra
	return data
}

// errClaudeUsage is a refused answer: its status and how long to leave the account alone.
type errClaudeUsage struct {
	status int
	wait   time.Duration
}

func (e *errClaudeUsage) Error() string { return "claude usage: HTTP " + strconv.Itoa(e.status) }

// retryAfter is the Retry-After header as a duration; 0 when absent or not seconds.
func retryAfter(h string) time.Duration {
	n, err := strconv.Atoi(strings.TrimSpace(h))
	if err != nil || n <= 0 {
		return 0
	}
	return time.Duration(n) * time.Second
}

// FetchClaudeUsage asks Anthropic once; nil data with an error on a refusal or a failure (the file keeps what it has).
func FetchClaudeUsage(ctx context.Context, login ClaudeLogin, client *http.Client, url string, now float64) (Data, error) {
	if client == nil {
		client = http.DefaultClient
	}
	ctx, cancel := context.WithTimeout(ctx, claudeUsageTimout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, errors.New("claude usage: bad request")
	}
	req.Header.Set("Authorization", "Bearer "+login.AccessToken)
	req.Header.Set("anthropic-beta", claudeOAuthBeta)
	req.Header.Set("Content-Type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		// The error may quote the request; only its kind is told.
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, errors.New("claude usage: timeout")
		}
		return nil, errors.New("claude usage: fetch failed")
	}
	defer res.Body.Close()
	switch {
	case res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden:
		return nil, &errClaudeUsage{status: res.StatusCode, wait: claudeAuthBackoff}
	case res.StatusCode == http.StatusTooManyRequests:
		return nil, &errClaudeUsage{status: res.StatusCode, wait: max(retryAfter(res.Header.Get("Retry-After")), claudeLimitBackoff)}
	case res.StatusCode < 200 || res.StatusCode > 299:
		return nil, &errClaudeUsage{status: res.StatusCode}
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, errors.New("claude usage: read failed")
	}
	var body any
	if json.Unmarshal(raw, &body) != nil {
		return nil, errors.New("claude usage: not JSON")
	}
	data := ParseClaudeUsage(body, now)
	if data == nil {
		return nil, errors.New("claude usage: no windows in the answer")
	}
	return data, nil
}

// claudeActive says a Claude chat ran lately: its statusline wrote the entry's `seenAt` within ClaudeActiveSec.
func claudeActive(entry Data, now float64) bool {
	return entry != nil && finite(entry["seenAt"]) && now-num(entry["seenAt"]) < ClaudeActiveSec
}
