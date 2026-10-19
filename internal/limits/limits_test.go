package limits

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func codexbarEntry() Data {
	return Data{
		"ts": 1000.0, "source": "codexbar",
		"five_hour":     map[string]any{"used_percentage": 46.0, "resets_at": 5000.0, "at": 1000.0, "source": "codexbar"},
		"seven_day":     map[string]any{"used_percentage": 81.0, "resets_at": 9000.0, "at": 1000.0, "source": "codexbar"},
		"plan":          map[string]any{"name": "Claude Max 20x", "at": 1000.0},
		"codexbarError": nil,
	}
}

func TestStatuslineKeepsCodexbarFieldsAndYieldsToAFreshPoll(t *testing.T) {
	early := Merge(codexbarEntry(), Data{"ts": 1060.0, "five_hour": map[string]any{"used_percentage": 35.0, "resets_at": 5000.0}, "seven_day": nil})
	if early["five_hour"].(map[string]any)["used_percentage"] != 46.0 {
		t.Fatalf("a fresh codexbar value lost to the statusline: %v", early["five_hour"])
	}
	late := Merge(codexbarEntry(), Data{"ts": 1181.0, "five_hour": map[string]any{"used_percentage": 50.0, "resets_at": 5000.0}, "seven_day": nil})
	same(t, "five hour", 0, late["five_hour"], map[string]any{"used_percentage": 50.0, "resets_at": 5000.0, "at": 1181.0})
	same(t, "plan", 0, late["plan"], codexbarEntry()["plan"])
	same(t, "seven day", 0, late["seven_day"], codexbarEntry()["seven_day"])
}

func TestCacheWritesAtomicallyAndKeepsBothProviders(t *testing.T) {
	file := filepath.Join(t.TempDir(), "sub", "rate-limits.json")
	c := &Cache{File: file}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			p := Providers[i%2]
			if _, err := c.Write(p, Data{"ts": float64(1000 + i), "five_hour": map[string]any{"used_percentage": float64(i), "resets_at": 1.0}, "seven_day": nil}); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	raw, _ := os.ReadFile(file)
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil || got["claude"] == nil || got["codex"] == nil || got["ts"] != 1019.0 {
		t.Fatalf("%v %s", err, raw)
	}
	if strings.Contains(string(raw), "\n") {
		t.Fatal("written with whitespace")
	}
	left, _ := filepath.Glob(file + ".*")
	if len(left) != 0 {
		t.Fatalf("temp files left: %v", left)
	}
}

func TestWatchReportsOnlyANewTs(t *testing.T) {
	file := filepath.Join(t.TempDir(), "rate-limits.json")
	_ = os.WriteFile(file, []byte(`{"ts":1}`), 0o600)
	got := make(chan float64, 10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go Watch(ctx, file, func(d map[string]any) { got <- d["ts"].(float64) })
	if ts := <-got; ts != 1 {
		t.Fatalf("first read %v", ts)
	}
	time.Sleep(50 * time.Millisecond)
	_ = os.WriteFile(file, []byte(`{"ts":1,"claude":{}}`), 0o600) // same ts: not reported
	_ = os.WriteFile(file+".tmp", []byte(`{"ts":3}`), 0o600)
	_ = os.Rename(file+".tmp", file)
	select {
	case ts := <-got:
		if ts != 3 {
			t.Fatalf("reported %v", ts)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no report after a replace")
	}
	select {
	case ts := <-got:
		t.Fatalf("reported twice: %v", ts)
	case <-time.After(700 * time.Millisecond):
	}
}

func TestHistoryRecordsNewMeasurementsAndRotates(t *testing.T) {
	file := filepath.Join(t.TempDir(), "limits-history.jsonl")
	h := &History{File: file}
	entry := func(used, at float64) map[string]any {
		return map[string]any{"ts": at, "claude": map[string]any{"ts": at, "five_hour": map[string]any{"used_percentage": used, "resets_at": 1000.0, "at": at}}}
	}
	for _, step := range []struct{ used, at, now float64 }{{46, 100, 100}, {46, 100, 500}, {46, 130, 130}, {46, 160, 161}, {47, 170, 170}} {
		if err := h.Record(entry(step.used, step.at), step.now); err != nil {
			t.Fatal(err)
		}
	}
	raw, _ := os.ReadFile(file)
	want := `{"t":100,"p":"claude","w":{"five_hour":[46,1000,100]}}
{"t":161,"p":"claude","w":{"five_hour":[46,1000,160]}}
{"t":170,"p":"claude","w":{"five_hour":[47,1000,170]}}
`
	if string(raw) != want {
		t.Fatalf("got\n%s", raw)
	}
	if st, _ := os.Stat(file); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode())
	}
	_ = os.WriteFile(file, []byte(string(raw)+"garbage\n\n"+`{"t":1,"p":"claude","w":{}}`+"\n"), 0o600)
	if err := RotateHistory(file, 161+HistoryKeepSec); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(file); string(raw) != strings.Join(strings.Split(want, "\n")[1:], "\n") {
		t.Fatalf("rotated:\n%s", raw)
	}
	if err := RotateHistory(filepath.Join(t.TempDir(), "missing"), 0); err != nil {
		t.Fatal(err)
	}
}

func TestCodexbarPollWritesDataOrOnlyTheError(t *testing.T) {
	c := &Cache{File: filepath.Join(t.TempDir(), "rate-limits.json")}
	clock := func() float64 { return 2000 }
	ok := func(_ context.Context, bin string, args []string) ([]byte, error) {
		if bin != "/bin/codexbar" || strings.Join(args, " ") != "usage --provider claude --source oauth --format json" {
			t.Fatalf("%s %v", bin, args)
		}
		return []byte(`[{"provider":"claude","account":"me@example.com","usage":{"primary":{"usedPercent":12,"resetsAt":1800000000000,"windowMinutes":300},"loginMethod":"Claude Max"}}]`), nil
	}
	if _, err := PollCodexbarOnce(context.Background(), "/bin/codexbar", ok, c, clock); err != nil {
		t.Fatal(err)
	}
	failed := func(context.Context, string, []string) ([]byte, error) {
		return nil, errors.New("таймаут 20 с")
	}
	if _, err := PollCodexbarOnce(context.Background(), "/bin/codexbar", failed, c, func() float64 { return 2100 }); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(c.File)
	if strings.Contains(string(raw), "example.com") {
		t.Fatalf("the account identity was stored: %s", raw)
	}
	claude := c.Read()["claude"]
	same(t, "kept window", 0, claude["five_hour"], map[string]any{"used_percentage": 12.0, "resets_at": 1800000000.0, "at": 2000.0, "source": "codexbar"})
	same(t, "error", 0, claude["codexbarError"], map[string]any{"message": "codexbar: таймаут 20 с", "at": 2100.0})
}

func TestCodexbarCLIFailures(t *testing.T) {
	dir := t.TempDir()
	if _, err := runCodexbarCLI(context.Background(), filepath.Join(dir, "missing"), nil); err == nil || err.Error() != "не запустился (ENOENT)" {
		t.Fatalf("%v", err)
	}
	script := filepath.Join(dir, "codexbar")
	_ = os.WriteFile(script, []byte("#!/bin/sh\necho '[{\"provider\":\"claude\",\"error\":{\"message\":\"no login\"}}]'\nexit 1\n"), 0o755)
	out, err := runCodexbarCLI(context.Background(), script, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, failure := ParseCodexbar(out, 1); failure != "codexbar: no login" {
		t.Fatalf("%q", failure)
	}
	silent := filepath.Join(dir, "silent")
	_ = os.WriteFile(silent, []byte("#!/bin/sh\nexit 3\n"), 0o755)
	if _, err := runCodexbarCLI(context.Background(), silent, nil); err == nil || err.Error() != "не запустился (3)" {
		t.Fatalf("%v", err)
	}
}

func TestCodexUsagePollSendsTheLoginAndWritesCodex(t *testing.T) {
	var auth, account, originator string
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, account, originator = r.Header.Get("Authorization"), r.Header.Get("ChatGPT-Account-Id"), r.Header.Get("originator")
		w.WriteHeader(status)
		fmt.Fprint(w, `{"plan_type":"pro","rate_limit":{"primary_window":{"used_percent":3,"limit_window_seconds":604800,"reset_after_seconds":100}}}`)
	}))
	defer srv.Close()
	home := t.TempDir()
	_ = os.WriteFile(filepath.Join(home, "auth.json"), []byte(`{"tokens":{"access_token":"tok","account_id":"acc"}}`), 0o600)
	login := ReadCodexLogin(home)
	c := &Cache{File: filepath.Join(t.TempDir(), "rate-limits.json")}
	_, _ = c.Write("codex", Data{"ts": 1.0, "five_hour": map[string]any{"used_percentage": 9.0, "resets_at": 5.0}, "seven_day": nil})
	if _, err := PollCodexUsageOnce(context.Background(), *login, nil, srv.URL, c, func() float64 { return 3000 }); err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer tok" || account != "acc" || originator != "codex_cli_rs" {
		t.Fatalf("%q %q %q", auth, account, originator)
	}
	codex := c.Read()["codex"]
	if codex["five_hour"] != nil || codex["plan"].(map[string]any)["name"] != "ChatGPT Pro" {
		t.Fatalf("a complete source must clear the window the plan lacks: %v", codex)
	}
	status = http.StatusUnauthorized
	_, _ = PollCodexUsageOnce(context.Background(), *login, nil, srv.URL, c, func() float64 { return 3100 })
	if msg := c.Read()["codex"]["codexbarError"].(map[string]any)["message"]; msg != "Codex: вход Codex истёк, откройте Codex на машине" {
		t.Fatalf("%v", msg)
	}
	if ReadCodexLogin(t.TempDir()) != nil {
		t.Fatal("no auth.json is no login")
	}
}

func TestPollersSkipAFreshCodexValue(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	defer srv.Close()
	home := t.TempDir()
	_ = os.WriteFile(filepath.Join(home, "auth.json"), []byte(`{"tokens":{"access_token":"tok"}}`), 0o600)
	c := &Cache{File: filepath.Join(t.TempDir(), "rate-limits.json")}
	_, _ = c.Write("codex", Data{"ts": 1000.0, "source": "codex-usage", "five_hour": nil, "seven_day": nil})
	p := &Pollers{Cache: c, CodexHome: home, URL: srv.URL, Clock: func() float64 { return 1059 }}
	p.pollCodexUsage(context.Background())
	if calls != 0 {
		t.Fatal("asked again within half an interval")
	}
	p.Clock = func() float64 { return 1061 }
	p.pollCodexUsage(context.Background())
	if calls != 1 {
		t.Fatalf("calls %d", calls)
	}
}

func TestCodexTranscriptTail(t *testing.T) {
	file := filepath.Join(t.TempDir(), "rollout.jsonl")
	line := `{"type":"event_msg","payload":{"type":"token_count","rate_limits":{"primary":{"used_percent":12,"window_minutes":300,"resets_at":1800000000}}}}`
	long := `{"type":"event_msg","payload":{"type":"token_count","rate_limits":{"primary":{"used_percent":99,"window_minutes":300,"resets_at":1}}},"pad":"` + strings.Repeat("x", codexTailBytes) + `"}`
	_ = os.WriteFile(file, []byte(long+"\n"+line+"\n"), 0o600)
	got := CodexTranscriptLimits(file, 5)
	same(t, "transcript", 0, got, map[string]any{"ts": 5.0, "five_hour": map[string]any{"used_percentage": 12.0, "resets_at": 1800000000.0}, "seven_day": nil})
	if CodexTranscriptLimits(filepath.Join(t.TempDir(), "none"), 5) != nil {
		t.Fatal("a missing transcript has no limits")
	}
}
