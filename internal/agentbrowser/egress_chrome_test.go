package agentbrowser

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// awaitString evaluates a promise in a tab and gives its value as a string.
func awaitString(t *testing.T, i *Instance, id, expr string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	b, err := i.Call(ctx, id, "Runtime.evaluate", map[string]any{"expression": expr, "awaitPromise": true,
		"returnByValue": true, "userGesture": true})
	must(t, err)
	var r struct {
		Result struct {
			Value any `json:"value"`
		} `json:"result"`
	}
	must(t, json.Unmarshal(b, &r))
	return fmt.Sprint(r.Result.Value)
}

func portOfURL(t *testing.T, raw string) string {
	t.Helper()
	_, port, err := net.SplitHostPort(strings.TrimPrefix(raw, "http://"))
	must(t, err)
	return port
}

// The agent's Chrome reaches the machine's dev servers and nothing else of the machine or its networks, opens no
// dangerous scheme, keeps downloads in its profile within the limit, runs niced with a renderer bound, closes itself
// once no runtime is connected, and keeps the session cookies across that.
func TestChromeEgressDownloadsAndIdle(t *testing.T) {
	chrome, err := FindChrome()
	if err != nil {
		skipWithoutChrome(t, err)
	}
	if testing.Short() {
		t.Skip("drives Chrome")
	}
	secret := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		fmt.Fprint(w, "PLATFORM-SECRET")
	}))
	defer secret.Close()
	// The runtime's own port stands for the platform's: the deployment keeps it from the preview.
	t.Setenv("RUNTIME_RESERVED_PORTS", portOfURL(t, secret.URL))
	big := strings.Repeat("x", 64<<10)
	dev := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/small":
			w.Header().Set("Content-Disposition", `attachment; filename="small.txt"`)
			fmt.Fprint(w, "small download")
		case "/big":
			w.Header().Set("Content-Disposition", `attachment; filename="big.txt"`)
			w.Header().Set("Content-Length", strconv.Itoa(len(big)))
			fmt.Fprint(w, big)
		default:
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<body>dev fixture</body>`)
		}
	}))
	defer dev.Close()
	data := t.TempDir()
	exe, err := os.Executable()
	must(t, err)
	cfg := Config{DataDir: data, Executable: exe, Chrome: chrome, Mode: "headless", MaxTabs: 4, Nice: 7,
		DownloadLimit: 16 << 10, Idle: 2 * time.Second}
	dir := filepath.Join(data, "agent-browser", "profiles", DefaultProfile)
	t.Cleanup(func() { cleanupHost(t, dir) })
	m := New(cfg)
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()
	i, err := m.Instance(ctx, DefaultProfile)
	if err != nil {
		log, _ := os.ReadFile(filepath.Join(dir, "host.log"))
		t.Fatalf("startup: %v\n%s", err, log)
	}
	tab, err := i.NewTab(ctx, "chat", "agent", dev.URL)
	must(t, err)
	eventually(t, 15*time.Second, func() bool {
		return strings.Contains(string(evaluate(t, i, tab.ID, "document.body ? document.body.innerText : ''")), "dev fixture")
	})
	// A viewer keeps the runtime from closing the idle browser until the last subtest lets it.
	viewer, err := i.Watch(ctx, tab.ID)
	must(t, err)
	go func() {
		for range viewer.Frames() {
		}
	}()

	t.Run("chrome_flags_and_priority", func(t *testing.T) {
		pid := pidAt(t, dir, "chrome")
		cmdline, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
		if err != nil {
			t.Skip(err)
		}
		for _, want := range []string{"--proxy-server=http://127.0.0.1:", "--proxy-bypass-list=<-loopback>", "--renderer-process-limit=4"} {
			if !strings.Contains(string(cmdline), want) {
				t.Fatalf("Chrome runs without %s: %q", want, cmdline)
			}
		}
		niceOf := func(pid string) int {
			stat, err := os.ReadFile("/proc/" + pid + "/stat")
			must(t, err)
			fields := strings.Fields(string(stat[strings.LastIndexByte(string(stat), ')')+1:]))
			n, err := strconv.Atoi(fields[16])
			must(t, err)
			return n
		}
		if got, want := niceOf(strconv.Itoa(pid)), min(19, niceOf("self")+7); got != want {
			t.Fatalf("Chrome's nice %d, want %d", got, want)
		}
	})

	t.Run("machine_ports_and_private_networks", func(t *testing.T) {
		for _, target := range []string{secret.URL, "http://localhost:" + portOfURL(t, secret.URL),
			"http://10.0.0.1/", "http://169.254.169.254/latest/meta-data/"} {
			got := awaitString(t, i, tab.ID, `fetch(`+strconv.Quote(target)+`).then(r => r.status + ":" + r.statusText).then(s => s, e => "error")`)
			if strings.HasPrefix(got, "200") {
				t.Fatalf("page fetched %s: %s", target, got)
			}
		}
		// A dev server answers, by name and by address.
		for _, target := range []string{dev.URL, "http://localhost:" + portOfURL(t, dev.URL)} {
			if got := awaitString(t, i, tab.ID, `fetch(`+strconv.Quote(target)+`).then(r => r.text())`); !strings.Contains(got, "dev fixture") {
				t.Fatalf("dev server %s: %q", target, got)
			}
		}
		// Navigation is held to the same rule: the page of the platform's port never loads.
		_, err := i.Call(ctx, tab.ID, "Page.navigate", map[string]any{"url": secret.URL})
		must(t, err)
		time.Sleep(time.Second)
		if body := string(evaluate(t, i, tab.ID, "document.documentElement.innerText")); strings.Contains(body, "PLATFORM-SECRET") {
			t.Fatalf("navigation reached the platform: %s", body)
		}
	})

	t.Run("dangerous_schemes", func(t *testing.T) {
		for _, raw := range []string{"file:///etc/hostname", "chrome://version", "data:text/html,<b>x</b>", "javascript:document.title='x'"} {
			if _, err := i.Call(ctx, tab.ID, "Page.navigate", map[string]any{"url": raw}); err == nil {
				t.Fatalf("%s opened", raw)
			}
		}
		// A page cannot get there by itself either.
		_, err := i.Call(ctx, tab.ID, "Page.navigate", map[string]any{"url": dev.URL})
		must(t, err)
		time.Sleep(500 * time.Millisecond)
		evaluate(t, i, tab.ID, "location.href = 'file:///etc/hostname'; 1")
		time.Sleep(time.Second)
		if href := string(evaluate(t, i, tab.ID, "location.href")); strings.Contains(href, "file:") {
			t.Fatalf("page navigated to %s", href)
		}
	})

	t.Run("downloads", func(t *testing.T) {
		downloads := filepath.Join(dir, "downloads")
		click := func(path string) {
			evaluate(t, i, tab.ID, `(() => { const a = document.createElement('a'); a.href = '`+path+`'; a.download = ''; document.body.append(a); a.click(); return 1 })()`)
		}
		click("/small")
		eventually(t, 15*time.Second, func() bool {
			entries, _ := os.ReadDir(downloads)
			for _, e := range entries {
				if b, err := os.ReadFile(filepath.Join(downloads, e.Name())); err == nil && string(b) == "small download" {
					return true
				}
			}
			return false
		})
		click("/big")
		time.Sleep(3 * time.Second)
		entries, _ := os.ReadDir(downloads)
		for _, e := range entries {
			if info, err := e.Info(); err == nil && info.Size() > cfg.DownloadLimit {
				t.Fatalf("a download past the limit was kept: %s (%d bytes)", e.Name(), info.Size())
			}
		}
	})

	t.Run("idle_host_closes_and_keeps_session_cookies", func(t *testing.T) {
		evaluate(t, i, tab.ID, "document.cookie = 'kept=yes; path=/'; 1")
		viewer.Close()
		m.Close()
		eventually(t, 20*time.Second, func() bool { return !m.ProfileRunning(DefaultProfile) })
		if _, err := os.Stat(filepath.Join(dir, "session-cookies.json")); err != nil {
			t.Fatalf("session cookies not kept: %v", err)
		}
		again := New(cfg)
		defer again.Close()
		i, err := again.Instance(ctx, DefaultProfile)
		must(t, err)
		next, err := i.NewTab(ctx, "chat", "agent", dev.URL)
		must(t, err)
		eventually(t, 15*time.Second, func() bool {
			return strings.Contains(string(evaluate(t, i, next.ID, "document.cookie")), "kept=yes")
		})
		if again.staleHost(ctx, DefaultProfile) {
			t.Fatal("a host of this build answers as stale")
		}
	})
}
