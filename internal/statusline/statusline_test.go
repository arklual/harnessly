package statusline

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// These differ from the previous runtime on purpose (fixed quirks, expected differences in a shadow run): the person's
// command had no time limit and was never killed, and a command running the platform's own script posted here again.

func commandFixture(t *testing.T, command string) *fixture {
	t.Helper()
	f := newFixture(t, "tok", map[string]string{"cli-token": "tok"}, time.Now().UnixMilli())
	if command != "" {
		f.write(filepath.Join(f.root, "home", ".claude", "settings.json"), `{"statusLine":{"command":`+strconv.Quote(command)+`}}`)
	}
	return f
}

func alive(pid int) bool { return syscall.Kill(pid, 0) == nil }

func readPID(t *testing.T, file string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		raw, _ := os.ReadFile(file)
		if pid, err := strconv.Atoi(strings.TrimSpace(string(raw))); err == nil && pid > 0 {
			return pid
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no pid in %s", file)
	return 0
}

func waitGone(t *testing.T, pids ...int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for _, pid := range pids {
		for alive(pid) {
			if time.Now().After(deadline) {
				t.Fatalf("process %d still runs", pid)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
}

var tokenHeaders = map[string]string{"x-cli-token": "tok", "content-type": "application/json"}

func TestCommandIsKilledWithItsChildrenAfterTheTimeout(t *testing.T) {
	dir := t.TempDir()
	f := commandFixture(t, "echo $$ > "+dir+"/sh; sleep 30 & echo $! > "+dir+"/child; wait")
	f.sink.Timeout = 300 * time.Millisecond
	start := time.Now()
	a := f.post("POST", "/api/status/statusline", tokenHeaders, `{}`, true)
	if a.status != http.StatusNoContent {
		t.Fatalf("status %d %s", a.status, a.body)
	}
	if took := time.Since(start); took > 3*time.Second {
		t.Fatalf("answered after %s", took)
	}
	waitGone(t, readPID(t, dir+"/sh"), readPID(t, dir+"/child"))
}

func TestCommandIsKilledWhenTheCallerGoesAway(t *testing.T) {
	dir := t.TempDir()
	f := commandFixture(t, "echo $$ > "+dir+"/sh; sleep 30 & echo $! > "+dir+"/child; wait")
	f.sink.Timeout = time.Minute
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "POST", f.srv.URL+"/api/status/statusline", strings.NewReader(`{}`))
	for k, v := range tokenHeaders {
		req.Header.Set(k, v)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if res, err := http.DefaultClient.Do(req); err == nil {
			res.Body.Close()
		}
	}()
	shPID, childPID := readPID(t, dir+"/sh"), readPID(t, dir+"/child")
	cancel()
	<-done
	waitGone(t, shPID, childPID)
}

func TestFastCommandAnswersItsOutput(t *testing.T) {
	f := commandFixture(t, "read -r line; printf 'got %s' \"$line\"; exit 2")
	a := f.post("POST", "/api/status/statusline", tokenHeaders, `{"b":1,"a":2}`, true)
	if a.status != http.StatusOK || string(a.body) != `got {"b":1,"a":2}` || a.header.Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Fatalf("%d %q %v", a.status, a.body, a.header)
	}
}

func TestCommandRunningThePlatformScriptIsRefused(t *testing.T) {
	dir := t.TempDir()
	f := commandFixture(t, "")
	_ = os.WriteFile(f.sink.Script, []byte("#!/bin/sh\nexit 0\n"), 0o755)
	// The data folder reached through another name, as an old data folder name links to the current one.
	alias := filepath.Join(f.root, "home", ".alias")
	if err := os.Symlink(f.data, alias); err != nil {
		t.Fatal(err)
	}
	ran := filepath.Join(dir, "ran")
	refused := []string{
		`sh "` + f.sink.Script + `"`,
		"sh ~/.alias/statusline.sh",
		"bash $HOME/.alias/statusline.sh | head -1",
		"sh '${HOME}/.alias/statusline.sh'",
		"FOO=1 sh " + alias + "/../data/statusline.sh",
		"cat " + alias + "/statusline.sh|sh",
	}
	for _, c := range refused {
		f.write(filepath.Join(f.root, "home", ".claude", "settings.json"), `{"statusLine":{"command":`+strconv.Quote("touch "+ran+"; "+c)+`}}`)
		a := f.post("POST", "/api/status/statusline", tokenHeaders, `{}`, true)
		if a.status != http.StatusNoContent {
			t.Errorf("%s: status %d %q", c, a.status, a.body)
		}
		if _, err := os.Stat(ran); err == nil {
			t.Errorf("%s: the command ran", c)
			_ = os.Remove(ran)
		}
	}
	// Another script of that name is the person's own.
	own := filepath.Join(dir, "statusline.sh")
	_ = os.WriteFile(own, []byte("printf mine\n"), 0o755)
	f.write(filepath.Join(f.root, "home", ".claude", "settings.json"), `{"statusLine":{"command":`+strconv.Quote("sh "+own)+`}}`)
	if a := f.post("POST", "/api/status/statusline", tokenHeaders, `{}`, true); a.status != http.StatusOK || string(a.body) != "mine" {
		t.Fatalf("own script: %d %q", a.status, a.body)
	}
}
