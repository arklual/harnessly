package limits

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func sample(t *testing.T, name string) any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "vendors", name))
	if err != nil {
		t.Fatal(err)
	}
	var body any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	return body
}

func win(t *testing.T, d Data, key string) (used, reset float64) {
	t.Helper()
	w, ok := object(d[key])
	if !ok {
		t.Fatalf("no %s window in %v", key, d)
	}
	return num(w["used_percentage"]), num(w["resets_at"])
}

func near(a, b float64) bool { return a-b < 0.01 && b-a < 0.01 }

func TestParseKimiUsageCounts(t *testing.T) {
	d := ParseKimiUsage(sample(t, "kimi-counts.json"), 1000)
	if used, reset := win(t, d, "seven_day"); !near(used, 375.0/2048*100) || !near(reset, 1767972193.373) {
		t.Fatalf("week: %v %v", used, reset)
	}
	if used, reset := win(t, d, "five_hour"); !near(used, 9.5) || !near(reset, 1767711924.374) {
		t.Fatalf("five hours: %v %v", used, reset)
	}
	if d[WindowMonth] != nil || d["source"] != SourceKimiUsages {
		t.Fatalf("entry: %v", d)
	}
}

func TestParseKimiUsageRatiosReplaceCounts(t *testing.T) {
	d := ParseKimiUsage(sample(t, "kimi-ratios.json"), 1000)
	if used, reset := win(t, d, "five_hour"); used != 0 || reset != 1789589744 {
		t.Fatalf("five hours: %v %v", used, reset)
	}
	if used, _ := win(t, d, WindowMonth); !near(used, 0.56) {
		t.Fatalf("month: %v", used)
	}
	if d["seven_day"] != nil {
		t.Fatalf("a week the answer does not have: %v", d["seven_day"])
	}
}

func TestParseZaiQuota(t *testing.T) {
	d := ParseZaiQuota(sample(t, "zai-quota.json"), 1000)
	if used, reset := win(t, d, "five_hour"); used != 25 || reset != 1785816000 {
		t.Fatalf("five hours: %v %v", used, reset)
	}
	if used, reset := win(t, d, "seven_day"); used != 9 || reset != 1786291200 {
		t.Fatalf("week: %v %v", used, reset)
	}
	if d[WindowMonth] != nil {
		t.Fatalf("the MCP count is not a window of the model: %v", d[WindowMonth])
	}
	if plan, _ := object(d["plan"]); plan["name"] != "GLM Pro" {
		t.Fatalf("plan: %v", d["plan"])
	}
}

// A window nothing was spent of has no reset yet; a spent one without a reset is not shown.
func TestParseZaiQuotaWithoutReset(t *testing.T) {
	d := ParseZaiQuota(sample(t, "zai-fresh.json"), 1000)
	if used, reset := win(t, d, "five_hour"); used != 0 || reset != 1000+5*3600 {
		t.Fatalf("five hours: %v %v", used, reset)
	}
	if d["seven_day"] != nil {
		t.Fatalf("a spent window without a reset: %v", d["seven_day"])
	}
}

func TestParseGrokCredits(t *testing.T) {
	d := ParseGrokCredits(sample(t, "grok-credits.json"), 1000)
	if used, reset := win(t, d, "seven_day"); used != 90 || !near(reset, 1786622400.123) {
		t.Fatalf("week: %v %v", used, reset)
	}
}

func TestParseMoonshotBalance(t *testing.T) {
	d := ParseMoonshotBalance(sample(t, "moonshot-balance.json"), "USD", 1000)
	b, _ := object(d["balance"])
	if b["amount"] != 49.58 || b["currency"] != "USD" {
		t.Fatalf("balance: %v", d)
	}
}

func TestParsersTakeNothingFromAStrangeAnswer(t *testing.T) {
	for _, body := range []any{nil, "x", map[string]any{}, map[string]any{"data": map[string]any{"limits": []any{map[string]any{"type": "TOKENS_LIMIT"}}}},
		map[string]any{"usage": map[string]any{"limit": "0", "used": "1"}}, map[string]any{"config": map[string]any{"creditUsagePercent": 5}}} {
		if ParseKimiUsage(body, 1) != nil || ParseZaiQuota(body, 1) != nil || ParseGrokCredits(body, 1) != nil || ParseMoonshotBalance(body, "USD", 1) != nil {
			t.Fatalf("made limits out of %v", body)
		}
	}
}

func profilesFor(t *testing.T, srv *httptest.Server, now *float64, accounts ...Account) *Profiles {
	t.Helper()
	return &Profiles{
		Cache:    &ProfileCache{File: filepath.Join(t.TempDir(), "provider-limits.json")},
		Accounts: func() []Account { return accounts },
		Client:   srv.Client(),
		URL:      func(string, Vendor) string { return srv.URL },
		Clock:    func() float64 { return *now },
	}
}

func keyOf(token string, login bool) func(context.Context) (Credential, error) {
	return func(context.Context) (Credential, error) { return Credential{Token: token, Login: login}, nil }
}

func TestProfilesAskNoOftenerThanTheInterval(t *testing.T) {
	var asked atomic.Int32
	var auth atomic.Value
	body, _ := os.ReadFile(filepath.Join("testdata", "vendors", "zai-quota.json"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		auth.Store(r.Header.Get("Authorization"))
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	now := 1785800000.0
	p := profilesFor(t, srv, &now, Account{Profile: "glm", Account: "work", Credential: keyOf("k-1", false)})
	p.Refresh(context.Background())
	p.Refresh(context.Background())
	now += ProfileMinInterval.Seconds() - 1
	p.Refresh(context.Background())
	if asked.Load() != 1 || auth.Load() != "k-1" {
		t.Fatalf("asked %d times with %q", asked.Load(), auth.Load())
	}
	e := p.Cache.Read()["glm/work"]
	if e == nil || e["profile"] != "glm" || e["account"] != "work" || e["five_hour"] == nil {
		t.Fatalf("entry: %v", p.Cache.Read())
	}
	now += 2
	p.Refresh(context.Background())
	if asked.Load() != 2 {
		t.Fatalf("asked %d times after the interval", asked.Load())
	}
}

func TestProfilesFailureKeepsTheLastAnswerAndWaits(t *testing.T) {
	var asked atomic.Int32
	fail := false
	body, _ := os.ReadFile(filepath.Join("testdata", "vendors", "kimi-counts.json"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		if fail {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	now := 1767700000.0
	p := profilesFor(t, srv, &now, Account{Profile: "kimi-code", Credential: keyOf("k", false)})
	p.Refresh(context.Background())
	fail = true
	now += ProfileMinInterval.Seconds() + 1
	p.Refresh(context.Background())
	e := p.Cache.Read()["kimi-code"]
	if msg, _ := object(e["error"]); msg["message"] == nil || e["seven_day"] == nil {
		t.Fatalf("entry after a failure: %v", e)
	}
	now += ProfileMinInterval.Seconds() + 1
	p.Refresh(context.Background())
	if asked.Load() != 2 {
		t.Fatalf("a failed request was repeated at once: %d", asked.Load())
	}
	// An unofficial source is not trusted past its failure: nothing of its last answer stays.
	grok := Account{Profile: "xai", Credential: keyOf("t", true)}
	_ = p.Cache.Put(grok, Data{"ts": 1.0, "seven_day": map[string]any{"used_percentage": 1.0, "resets_at": 2.0}}, 1)
	p.Accounts = func() []Account { return []Account{grok} }
	p.Refresh(context.Background())
	if g := p.Cache.Read()["xai"]; g["seven_day"] != nil || g["error"] == nil {
		t.Fatalf("an unofficial entry after a failure: %v", g)
	}
}

func TestProfilesUnknownIsNoEntry(t *testing.T) {
	var asked atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { asked.Add(1) }))
	defer srv.Close()
	now := 1000.0
	// Grok by an API key has no pool to ask; a profile no vendor answers for is skipped; a gone account is dropped.
	p := profilesFor(t, srv, &now, Account{Profile: "xai", Credential: keyOf("xai-key", false)}, Account{Profile: "gpt", Credential: keyOf("k", true)})
	_ = p.Cache.Put(Account{Profile: "glm"}, Data{"ts": 999.0}, 999)
	p.Refresh(context.Background())
	if asked.Load() != 0 || len(p.Cache.Read()) != 0 {
		t.Fatalf("asked %d, entries %v", asked.Load(), p.Cache.Read())
	}
}

// Z.ai refuses a key with HTTP 200 and its own code (the answer it gives a request with no credential).
func TestProfilesReadARefusalInsideASuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":1001,"msg":"Authentication parameter not received in Header, unable to authenticate","success":false}`))
	}))
	defer srv.Close()
	now := 1000.0
	p := profilesFor(t, srv, &now, Account{Profile: "glm", Credential: keyOf("bad", false)})
	p.Refresh(context.Background())
	e := p.Cache.Read()["glm"]
	msg, _ := object(e["error"])
	if msg["message"] != "the provider did not accept the key" && msg["message"] != "провайдер не принял ключ" {
		t.Fatalf("entry: %v", e)
	}
	if msg["rejected"] != true {
		t.Fatalf("a refused key is not marked: %v", e)
	}
	if e["five_hour"] != nil {
		t.Fatalf("windows out of a refusal: %v", e)
	}
}

func TestParseBalancesOfAPIProviders(t *testing.T) {
	var deepseek, openrouter any
	_ = json.Unmarshal([]byte(`{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"7.00"},{"currency":"USD","total_balance":"50.00","granted_balance":"10.00","topped_up_balance":"40.00"}]}`), &deepseek)
	_ = json.Unmarshal([]byte(`{"data":{"total_credits":25.5,"total_usage":3.25}}`), &openrouter)
	if b, _ := object(ParseDeepSeekBalance(deepseek, 1)["balance"]); b["amount"] != 50.0 || b["currency"] != "USD" {
		t.Fatalf("deepseek: %v", b)
	}
	if b, _ := object(ParseOpenRouterCredits(openrouter, 1)["balance"]); b["amount"] != 22.25 || b["currency"] != "USD" {
		t.Fatalf("openrouter: %v", b)
	}
	if ParseDeepSeekBalance(openrouter, 1) != nil || ParseOpenRouterCredits(deepseek, 1) != nil {
		t.Fatal("a balance out of another vendor's answer")
	}
	if v, ok := VendorOf(APIProfile("deepseek")); !ok || v.Host != "api.deepseek.com" {
		t.Fatalf("vendor: %+v", v)
	}
}

// One more sign-in of Codex is a profile of its own: its windows are asked the way the CLI asks them, with the token
// and the account of that sign-in, and kept apart from the CLI's own entry and from another sign-in's.
func TestProfilesAskTheWindowsOfACodexSignIn(t *testing.T) {
	seen := map[string]string{}
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen[r.Header.Get("Authorization")] = r.Header.Get("ChatGPT-Account-Id") + "|" + r.Header.Get("originator")
		mu.Unlock()
		used := "12"
		if r.Header.Get("ChatGPT-Account-Id") == "acct-b" {
			used = "80"
		}
		_, _ = w.Write([]byte(`{"plan_type":"plus","rate_limit":{"primary_window":{"used_percent":` + used + `,"limit_window_seconds":18000,"reset_after_seconds":600}}}`))
	}))
	defer srv.Close()
	now := 1785800000.0
	cred := func(token, account string, login bool) func(context.Context) (Credential, error) {
		return func(context.Context) (Credential, error) {
			return Credential{Token: token, Account: account, Login: login}, nil
		}
	}
	p := profilesFor(t, srv, &now,
		Account{Profile: "cx-aaaaaaaaaa", Credential: cred("t-a", "acct-a", true)},
		Account{Profile: "cx-bbbbbbbbbb", Credential: cred("t-b", "acct-b", true)},
		// A key is no plan: it has no windows to ask.
		Account{Profile: "cx-cccccccccc", Credential: cred("k-c", "", false)})
	p.Refresh(context.Background())
	if len(seen) != 2 || seen["Bearer t-a"] != "acct-a|codex_cli_rs" || seen["Bearer t-b"] != "acct-b|codex_cli_rs" {
		t.Fatalf("asked: %v", seen)
	}
	entries := p.Cache.Read()
	a, _ := object(entries["cx-aaaaaaaaaa"]["five_hour"])
	b, _ := object(entries["cx-bbbbbbbbbb"]["five_hour"])
	if a["used_percentage"] != 12.0 || b["used_percentage"] != 80.0 || a["resets_at"] != now+600 {
		t.Fatalf("entries: %v", entries)
	}
	if _, has := entries["cx-aaaaaaaaaa"]["codexbarError"]; has || entries["cx-cccccccccc"] != nil {
		t.Fatalf("entries: %v", entries)
	}
}
