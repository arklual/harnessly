package limits

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/locale"
)

// Codex subscription usage straight from ChatGPT, the endpoint Codex's own /status reads: the Codex transcript carries
// limits only while a Codex session runs, so without one the numbers froze for days. The request spends nothing. The
// login is only read: Codex refreshes its own tokens, and a refresh token rotated here would log Codex out.

const (
	CodexUsageURL      = "https://chatgpt.com/backend-api/wham/usage"
	CodexUsageInterval = 120 * time.Second
	codexUsageTimeout  = 15 * time.Second
)

// CodexHome is $CODEX_HOME, else ~/.codex.
func CodexHome(env, home string) string {
	if env != "" {
		return env
	}
	return filepath.Join(home, ".codex")
}

// CodexLogin is the ChatGPT login of Codex.
type CodexLogin struct {
	AccessToken string
	AccountID   string
}

// ReadCodexLogin is the login in <codex home>/auth.json; nil for an API-key login or no login at all.
func ReadCodexLogin(codexHome string) *CodexLogin {
	raw, err := os.ReadFile(filepath.Join(codexHome, "auth.json"))
	if err != nil {
		return nil
	}
	var parsed map[string]any
	if json.Unmarshal(raw, &parsed) != nil {
		return nil
	}
	tokens, ok := object(parsed["tokens"])
	if !ok {
		return nil
	}
	access, _ := tokens["access_token"].(string)
	if access == "" {
		return nil
	}
	account, _ := tokens["account_id"].(string)
	return &CodexLogin{AccessToken: access, AccountID: account}
}

func windowKeyOfSeconds(s float64) string {
	if math.Abs(s-5*3600) <= 3600 {
		return "five_hour"
	}
	if math.Abs(s-7*86400) <= 86400 {
		return "seven_day"
	}
	return ""
}

// ParseCodexUsage is both windows as the plan has them now. A Pro plan has only the weekly one, so the missing window
// is written as null and replaces whatever an older transcript copy left there. Nil when the answer has no window.
func ParseCodexUsage(body any, now float64) Data {
	b, ok := object(body)
	if !ok {
		return nil
	}
	rl, ok := object(b["rate_limit"])
	if !ok {
		return nil
	}
	data := Data{"ts": now, "five_hour": nil, "seven_day": nil, "source": SourceCodexUsage, "codexbarError": nil}
	for _, raw := range []any{rl["primary_window"], rl["secondary_window"]} {
		w, ok := object(raw)
		if !ok || !finite(w["used_percent"]) || !finite(w["limit_window_seconds"]) {
			continue
		}
		key := windowKeyOfSeconds(w["limit_window_seconds"].(float64))
		if key == "" {
			continue
		}
		var reset float64
		switch {
		case finite(w["reset_at"]):
			reset = w["reset_at"].(float64)
		case finite(w["reset_after_seconds"]):
			reset = jsRound(now + w["reset_after_seconds"].(float64))
		default:
			continue
		}
		data[key] = map[string]any{"used_percentage": w["used_percent"], "resets_at": reset, "at": now, "source": SourceCodexUsage}
	}
	if data["five_hour"] == nil && data["seven_day"] == nil {
		return nil
	}
	if plan, ok := b["plan_type"].(string); ok && plan != "" {
		r, size := utf8.DecodeRuneInString(plan)
		data["plan"] = map[string]any{"name": "ChatGPT " + string(unicode.ToUpper(r)) + plan[size:], "at": now}
	}
	return data
}

// PollCodexUsageOnce asks ChatGPT once and writes the answer (or the failure) as Codex's entry.
func PollCodexUsageOnce(ctx context.Context, login CodexLogin, client *http.Client, url string, cache *Cache, clock func() float64) (Data, error) {
	if client == nil {
		client = http.DefaultClient
	}
	now := clock()
	data, failure := fetchCodexUsage(ctx, login, client, url, now)
	if data == nil {
		data = Data{"ts": now, "five_hour": nil, "seven_day": nil, "codexbarError": map[string]any{"message": "Codex: " + failure, "at": now}}
	}
	_, err := cache.Write("codex", data)
	return data, err
}

func fetchCodexUsage(ctx context.Context, login CodexLogin, client *http.Client, url string, now float64) (Data, string) {
	ctx, cancel := context.WithTimeout(ctx, codexUsageTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, Sanitize(err.Error())
	}
	req.Header.Set("Authorization", "Bearer "+login.AccessToken)
	if login.AccountID != "" {
		req.Header.Set("ChatGPT-Account-Id", login.AccountID)
	}
	req.Header.Set("originator", "codex_cli_rs")
	res, err := client.Do(req)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, "The operation was aborted due to timeout"
		}
		return nil, "fetch failed"
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		if res.StatusCode == http.StatusUnauthorized {
			return nil, locale.Pick("the Codex sign-in has expired: open Codex on the machine", "вход Codex истёк, откройте Codex на машине")
		}
		return nil, "HTTP " + strconv.Itoa(res.StatusCode)
	}
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, Sanitize(err.Error())
	}
	var body any
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, Sanitize(strings.TrimPrefix(err.Error(), "json: "))
	}
	if data := ParseCodexUsage(body, now); data != nil {
		return data, ""
	}
	return nil, locale.Pick("the answer has no limit windows", "в ответе нет окон лимита")
}
