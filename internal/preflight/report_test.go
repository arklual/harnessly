package preflight

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

func checkByID(t *testing.T, rep hubproto.PreflightReport, id string) hubproto.Check {
	t.Helper()
	for _, c := range rep.Checks {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("no %q check in %+v", id, rep.Checks)
	return hubproto.Check{}
}

func TestDiskAndMemoryThresholds(t *testing.T) {
	gbs := func(n float64) int64 { return int64(n * float64(gb)) }
	disk := []struct {
		free  int64
		level string
	}{
		{gbs(2.9), "fail"}, {gbs(3), "warn"}, {gbs(9.9), "warn"}, {gbs(10), "ok"}, {gbs(412), "ok"}, {0, "fail"},
	}
	for _, tc := range disk {
		if got := diskCheck(hubproto.Disk{Path: "/", Total: gbs(500), Free: tc.free}); got.Level != tc.level || got.ID != "disk" || got.Message == "" {
			t.Errorf("free %d: %+v", tc.free, got)
		}
	}
	if got := diskCheck(hubproto.Disk{Path: "/"}); got.Level != "warn" {
		t.Errorf("an unknown disk is a warning, not a pass: %+v", got)
	}
	mem := []struct {
		total, avail int64
		level        string
	}{
		{gbs(1.9), gbs(1.5), "fail"}, {gbs(2), gbs(0.9), "warn"}, {gbs(2), gbs(1), "ok"}, {gbs(64), gbs(40), "ok"}, {gbs(64), 0, "warn"},
	}
	for _, tc := range mem {
		if got := memoryCheck(tc.total, tc.avail); got.Level != tc.level || got.ID != "memory" {
			t.Errorf("%d/%d: %+v", tc.total, tc.avail, got)
		}
	}
	if got := memoryCheck(0, 0); got.Level != "warn" {
		t.Errorf("unknown memory: %+v", got)
	}
}

func TestHumanGB(t *testing.T) {
	for in, want := range map[int64]string{0: "0,0 ГБ", gb / 2: "0,5 ГБ", 3 * gb: "3,0 ГБ", 12*gb + gb/2: "13 ГБ", 412 * gb: "412 ГБ"} {
		if got := humanGB(in); got != want {
			t.Errorf("%d: %q want %q", in, got, want)
		}
	}
}

func TestClockCheck(t *testing.T) {
	if c := clockCheck(30 * time.Second); c.Level != "ok" {
		t.Errorf("%+v", c)
	}
	if c := clockCheck(-61 * time.Second); c.Level != "warn" || c.ID != "clock" || !strings.Contains(c.Message, "61") {
		t.Errorf("%+v", c)
	}
	if c := clockCheck(61 * time.Second); c.Level != "warn" {
		t.Errorf("%+v", c)
	}
}

func TestClockSkew(t *testing.T) {
	hubTime := time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead || r.URL.Path != "/healthz" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Date", hubTime.Format(http.TimeFormat))
	}))
	defer hub.Close()
	skew, ok := clockSkew(context.Background(), ReportOptions{HubURL: hub.URL + "/", Now: func() time.Time { return hubTime.Add(95 * time.Second) }})
	if !ok || skew < 94*time.Second || skew > 96*time.Second {
		t.Fatalf("%v %v", skew, ok)
	}
	if _, ok := clockSkew(context.Background(), ReportOptions{}); ok {
		t.Error("no hub, no skew")
	}
	if _, ok := clockSkew(context.Background(), ReportOptions{HubURL: "http://127.0.0.1:1"}); ok {
		t.Error("an unreachable hub gives a skew")
	}
	undated := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Header()["Date"] = nil }))
	defer undated.Close()
	if _, ok := clockSkew(context.Background(), ReportOptions{HubURL: undated.URL}); ok {
		t.Error("an undated answer gives a skew")
	}
}

func TestNearestExisting(t *testing.T) {
	dir := t.TempDir()
	if got := nearestExisting(filepath.Join(dir, "a", "b", "c")); got != dir {
		t.Errorf("%q", got)
	}
	if got := nearestExisting(dir); got != dir {
		t.Errorf("%q", got)
	}
}

func TestNodeMajor(t *testing.T) {
	v := func(s string) *string { return &s }
	for in, want := range map[*string]int{v("20.11.0"): 20, v("18.19.1"): 18, v("22.1"): 22, v("x"): 0, nil: 0} {
		if got := nodeMajor(in); got != want {
			t.Errorf("%v: %d", in, got)
		}
	}
}

func TestReport(t *testing.T) {
	home, bin := t.TempDir(), t.TempDir()
	shell := filepath.Join(t.TempDir(), "loginsh")
	if err := os.WriteFile(shell, []byte("#!/bin/sh\nprintf '%s' \""+bin+"\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	script(t, bin, "tmux", `echo "tmux 3.4"`)
	script(t, bin, "git", `echo "git version 2.43.0"`)
	script(t, bin, "node", `echo "v18.19.1"`)
	script(t, bin, "codex", `echo "codex-cli 0.152.0"`)
	script(t, bin, "which", `echo /opt/codex/bin/codex`)
	c := &Checker{Home: home, Env: []string{"PATH=/nowhere", "HOME=" + home}, Shell: shell}
	rep := c.Report(context.Background(), ReportOptions{
		WorkDir: filepath.Join(home, "work"),
		Disk:    func(string) (int64, int64, error) { return 500 * gb, 7 * gb, nil },
		Now:     func() time.Time { return time.UnixMilli(1_800_000_000_000) },
	})
	if rep.At != 1_800_000_000_000 || rep.OS != runtime.GOOS || rep.Arch != runtime.GOARCH || rep.CPUs != runtime.NumCPU() {
		t.Fatalf("%+v", rep)
	}
	if rep.Disk.Path != filepath.Join(home, "work") || rep.Disk.Total != 500*gb || rep.Disk.Free != 7*gb {
		t.Fatalf("disk %+v", rep.Disk)
	}
	if runtime.GOOS == "linux" && (rep.MemTotal <= 0 || rep.OSName == "") {
		t.Fatalf("memory %d, os %q", rep.MemTotal, rep.OSName)
	}
	tools := rep.Tools
	if !tools.Tmux.Installed || *tools.Tmux.Version != "3.4" || tools.Tmux.Compatible == nil || !*tools.Tmux.Compatible {
		t.Fatalf("tmux %+v", tools.Tmux)
	}
	if !tools.Git.Installed || !tools.Node.Installed || *tools.Node.Version != "18.19.1" || tools.Claude.Installed || !tools.Codex.Installed {
		t.Fatalf("tools %+v", tools)
	}
	if c := checkByID(t, rep, "disk"); c.Level != "warn" {
		t.Errorf("7 GB free: %+v", c)
	}
	if c := checkByID(t, rep, "tmux"); c.Level != "ok" {
		t.Errorf("%+v", c)
	}
	if c := checkByID(t, rep, "git"); c.Level != "ok" {
		t.Errorf("%+v", c)
	}
	if c := checkByID(t, rep, "node"); c.Level != "warn" || !strings.Contains(c.Message, "Codex не поставить без Node.js 20+") {
		t.Errorf("node 18: %+v", c)
	}
	for _, c := range rep.Checks {
		if c.ID == "clock" {
			t.Errorf("a clock check without a hub: %+v", c)
		}
	}
}

func TestReportMissingTools(t *testing.T) {
	home, bin := t.TempDir(), t.TempDir()
	shell := filepath.Join(t.TempDir(), "loginsh")
	if err := os.WriteFile(shell, []byte("#!/bin/sh\nprintf '%s' \""+bin+"\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	script(t, bin, "tmux", `echo "tmux 2.8"`)
	c := &Checker{Home: home, Env: []string{"PATH=/nowhere", "HOME=" + home}, Shell: shell}
	rep := c.Report(context.Background(), ReportOptions{Disk: func(string) (int64, int64, error) { return 0, 0, os.ErrPermission }})
	if rep.Disk.Path != home || rep.Disk.Total != 0 {
		t.Fatalf("disk %+v", rep.Disk)
	}
	for id, level := range map[string]string{"tmux": "fail", "git": "warn", "node": "warn", "disk": "warn"} {
		if got := checkByID(t, rep, id); got.Level != level {
			t.Errorf("%s: %+v", id, got)
		}
	}
	if c := checkByID(t, rep, "tmux"); !strings.Contains(c.Message, "2.9") {
		t.Errorf("%+v", c)
	}
	if rep.Tools.Git.Installed || rep.Tools.Node.Installed || rep.Tools.Node.Version != nil {
		t.Fatalf("%+v", rep.Tools)
	}
}

func TestReportWithClock(t *testing.T) {
	hubTime := time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Date", hubTime.Add(-10*time.Minute).Format(http.TimeFormat))
	}))
	defer hub.Close()
	home, bin := t.TempDir(), t.TempDir()
	shell := filepath.Join(t.TempDir(), "loginsh")
	_ = os.WriteFile(shell, []byte("#!/bin/sh\nprintf '%s' \""+bin+"\"\n"), 0o755)
	c := &Checker{Home: home, Env: []string{"PATH=/nowhere", "HOME=" + home}, Shell: shell}
	rep := c.Report(context.Background(), ReportOptions{HubURL: hub.URL, Now: func() time.Time { return hubTime },
		Disk: func(string) (int64, int64, error) { return 500 * gb, 400 * gb, nil }})
	if got := checkByID(t, rep, "clock"); got.Level != "warn" {
		t.Fatalf("%+v", got)
	}
}

func loginChecker(t *testing.T, shellBody string) *Checker {
	t.Helper()
	home := t.TempDir()
	shell := filepath.Join(t.TempDir(), "loginsh")
	if err := os.WriteFile(shell, []byte("#!/bin/sh\n"+shellBody+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return &Checker{Home: home, Env: []string{"PATH=/usr/bin:/bin", "HOME=" + home, "SECRET_ELSE=nope"}, Shell: shell}
}

func TestRunLogin(t *testing.T) {
	// A shell that behaves like a login shell: it prints noise as its profile would, sets a variable, then runs the
	// command it was given (-c).
	c := loginChecker(t, `echo "Welcome to the profile"; export FROM_PROFILE=yes; shift; eval "$1"`)
	res, err := c.RunLogin(context.Background(), `echo "profile=$FROM_PROFILE extra=$X home=$HOME"; echo "other=$SECRET_ELSE"; exit 3`, "X=1")
	if err != nil {
		t.Fatal(err)
	}
	if res.Exit != 3 {
		t.Fatalf("exit %d", res.Exit)
	}
	if want := "profile=yes extra=1 home=" + c.Home + "\nother=\n"; res.Stdout != want {
		t.Fatalf("stdout %q, want %q", res.Stdout, want)
	}
	// What the profile printed is not in the answer.
	if strings.Contains(res.Stdout, "Welcome") {
		t.Fatal("profile noise in the answer")
	}
}

func TestRunLoginFailures(t *testing.T) {
	c := loginChecker(t, `echo "no command run"`)
	if _, err := c.RunLogin(context.Background(), "true"); err == nil {
		t.Error("a shell that ran nothing was believed")
	}
	c.Shell = "/nonexistent/shell"
	if _, err := c.RunLogin(context.Background(), "true"); err == nil {
		t.Error("a missing shell")
	}
	slow := loginChecker(t, `shift; eval "$1"`)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if _, err := slow.RunLogin(ctx, "sleep 5"); err == nil || !strings.Contains(err.Error(), "ran out of time") {
		t.Errorf("a slow script: %v", err)
	}
}

func TestParseMarked(t *testing.T) {
	good := "noise\n\n<<HL-BEGIN>>\nline\n\n<<HL-END 0>>\n"
	if res, ok := parseMarked(good); !ok || res.Stdout != "line\n" || res.Exit != 0 {
		t.Errorf("%+v %v", res, ok)
	}
	// A script that prints the marker itself does not fool the parser: the last end mark counts.
	tricky := "\n<<HL-BEGIN>>\n<<HL-END 0>>\nstill the script\n\n<<HL-END 7>>\n"
	if res, ok := parseMarked(tricky); !ok || res.Exit != 7 {
		t.Errorf("%+v %v", res, ok)
	}
	for _, bad := range []string{"", "no marks", "\n<<HL-BEGIN>>\nnever ended", "\n<<HL-BEGIN>>\n\n<<HL-END x>>\n"} {
		if _, ok := parseMarked(bad); ok {
			t.Errorf("%q parsed", bad)
		}
	}
}
