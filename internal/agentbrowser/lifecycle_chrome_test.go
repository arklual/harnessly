package agentbrowser

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func lifecycleChrome(t *testing.T, mode string) (*Manager, Config) {
	t.Helper()
	if testing.Short() {
		t.Skip("drives Chrome")
	}
	chrome, err := FindChrome()
	if err != nil {
		t.Skip(err)
	}
	if mode == "xvfb" {
		if _, err := exec.LookPath("Xvfb"); err != nil {
			t.Skip(err)
		}
	}
	exe, err := os.Executable()
	must(t, err)
	cfg := Config{DataDir: t.TempDir(), Executable: exe, Chrome: chrome, Mode: mode}
	m := New(cfg)
	t.Cleanup(func() {
		m.Close()
		entries, _ := os.ReadDir(filepath.Join(cfg.DataDir, "agent-browser", "profiles"))
		for _, e := range entries {
			cleanupHost(t, filepath.Join(cfg.DataDir, "agent-browser", "profiles", e.Name()))
		}
	})
	return m, cfg
}

func cookieFixture(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<meta name="viewport" content="width=device-width,initial-scale=1"><title>cookie fixture</title><body>cookie fixture</body>`)
	}))
	t.Cleanup(server.Close)
	return server
}

func TestChromeStopClearRestore(t *testing.T) {
	for _, mode := range []string{"headless", "xvfb"} {
		t.Run(mode, func(t *testing.T) {
			m, _ := lifecycleChrome(t, mode)
			ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
			defer cancel()
			server := cookieFixture(t)
			i, err := m.Instance(ctx, DefaultProfile)
			must(t, err)
			a, err := i.NewTab(ctx, "chat", "agent", server.URL+"/a")
			must(t, err)
			b, err := i.NewTab(ctx, "other", "person", server.URL+"/b")
			must(t, err)
			must(t, i.SetViewport(ctx, a.ID, Viewport{Mode: "mobile", W: 390, H: 844, DPR: 1}))
			before := i.Tabs("")
			pid := pidAt(t, i.dir, "chrome")
			must(t, m.StopProfile(ctx, DefaultProfile))
			if m.ProfileRunning(DefaultProfile) {
				t.Fatal("profile still running")
			}
			time.Sleep(1500 * time.Millisecond)
			if m.ProfileRunning(DefaultProfile) || pidAt(t, i.dir, "chrome") != pid {
				t.Fatal("stop immediately reconnected")
			}
			if !m.ConversationHasTabs("chat") || !m.ConversationHasTabs("other") {
				t.Fatal("dormant tabs lost")
			}
			restarted, err := m.Instance(ctx, DefaultProfile)
			must(t, err)
			if restarted == i || pidAt(t, i.dir, "chrome") == pid {
				t.Fatal("profile did not restart")
			}
			after := restarted.Tabs("")
			if len(after) != len(before) {
				t.Fatalf("restore count: %+v", after)
			}
			for n := range before {
				old, next := before[n], after[n]
				if old.Order != next.Order || old.Conversation != next.Conversation || old.By != next.By || old.URL != next.URL || old.Viewport != next.Viewport {
					t.Fatalf("metadata changed: before=%+v after=%+v", old, next)
				}
			}
			if got := string(evaluate(t, restarted, after[0].ID, "[innerWidth,innerHeight]")); got != "[390,844]" {
				t.Fatal(got)
			}
			if restarted.Current("chat") != after[0].ID {
				t.Fatal("selection not remapped")
			}
			must(t, m.CloseConversation(ctx, "chat"))
			if m.ConversationHasTabs("chat") || len(restarted.Tabs("other")) != 1 {
				t.Fatal("active conversation cleanup")
			}
			must(t, m.StopProfile(ctx, DefaultProfile))
			must(t, m.CloseConversation(ctx, "other"))
			if m.ProfileRunning(DefaultProfile) || m.ConversationHasTabs("other") {
				t.Fatal("dormant cleanup started Chrome or retained tabs")
			}
			must(t, m.ClearProfile(ctx, DefaultProfile))
			if _, err := os.Stat(i.dir); !os.IsNotExist(err) {
				t.Fatal("profile data not erased")
			}
			_ = b
			t.Log("stop stays stopped; next access restores URL/owner/order/viewport/selection; active and dormant cleanup; clear erases data")
		})
	}
}

func TestChromeSessionCookieShutdownModes(t *testing.T) {
	for _, mode := range []string{"headless", "xvfb"} {
		for _, restore := range []string{"default", "restore-on-startup", "restore-last-session", "both"} {
			t.Run(mode+"/"+restore, func(t *testing.T) {
				if restore == "restore-last-session" || restore == "both" {
					t.Setenv("RUNTIME_BROWSER_CHROME_ARGS", os.Getenv("RUNTIME_BROWSER_CHROME_ARGS")+" --restore-last-session")
				}
				m, cfg := lifecycleChrome(t, mode)
				if restore == "restore-on-startup" || restore == "both" {
					path := filepath.Join(cfg.DataDir, "agent-browser", "profiles", DefaultProfile, "chrome", "Default")
					must(t, os.MkdirAll(path, 0700))
					must(t, os.WriteFile(filepath.Join(path, "Preferences"), []byte(`{"session":{"restore_on_startup":1}}`), 0600))
				}
				server := cookieFixture(t)
				i, err := m.Instance(t.Context(), DefaultProfile)
				must(t, err)
				a, err := i.NewTab(t.Context(), "chat", "agent", server.URL)
				must(t, err)
				eventually(t, 5*time.Second, func() bool { return string(evaluate(t, i, a.ID, "document.title")) == `"cookie fixture"` })
				evaluate(t, i, a.ID, `document.cookie='session=kept; Path=/';document.cookie='persistent=kept; Max-Age=3600; Path=/';true`)
				must(t, m.StopProfile(t.Context(), DefaultProfile))
				i, err = m.Instance(t.Context(), DefaultProfile)
				must(t, err)
				tabs := i.Tabs("chat")
				if len(tabs) != 1 {
					t.Fatalf("restored tabs: %+v", tabs)
				}
				eventually(t, 5*time.Second, func() bool { return string(evaluate(t, i, tabs[0].ID, "document.title")) == `"cookie fixture"` })
				cookies := string(evaluate(t, i, tabs[0].ID, "document.cookie"))
				if !strings.Contains(cookies, "persistent=kept") {
					t.Fatalf("persistent cookie lost: %s", cookies)
				}
				t.Logf("session_cookie_survived=%t persistent_cookie_survived=true cookies=%s", strings.Contains(cookies, "session=kept"), cookies)
				must(t, m.StopProfile(t.Context(), DefaultProfile))
			})
		}
	}
}

func TestChromeImportBasicCookies(t *testing.T) {
	for _, mode := range []string{"headless", "xvfb"} {
		t.Run(mode, func(t *testing.T) {
			m, cfg := lifecycleChrome(t, mode)
			server := cookieFixture(t)
			i, err := m.Instance(t.Context(), "source")
			must(t, err)
			a, err := i.NewTab(t.Context(), "chat", "agent", server.URL)
			must(t, err)
			eventually(t, 5*time.Second, func() bool { return string(evaluate(t, i, a.ID, "document.title")) == `"cookie fixture"` })
			evaluate(t, i, a.ID, `document.cookie='imported=kept; Max-Age=3600; Path=/';true`)
			dest := filepath.Join(cfg.DataDir, "agent-browser", "profiles", "copied", "chrome")
			if _, err = ImportChrome(t.Context(), filepath.Join(i.dir, "chrome"), dest); err == nil {
				t.Fatal("imported running Chrome")
			}
			must(t, m.StopProfile(t.Context(), "source"))
			source := filepath.Join(i.dir, "chrome")
			before, err := os.ReadFile(filepath.Join(source, "Default", "Cookies"))
			must(t, err)
			warning, err := ImportChrome(t.Context(), source, dest)
			must(t, err)
			if !strings.Contains(warning, "system keyring") {
				t.Fatal(warning)
			}
			after, err := os.ReadFile(filepath.Join(source, "Default", "Cookies"))
			must(t, err)
			if !reflect.DeepEqual(before, after) {
				t.Fatal("source modified")
			}
			j, err := m.Instance(t.Context(), "copied")
			must(t, err)
			b, err := j.NewTab(t.Context(), "other", "agent", server.URL)
			must(t, err)
			eventually(t, 5*time.Second, func() bool { return strings.Contains(string(evaluate(t, j, b.ID, "document.cookie")), "imported=kept") })
			must(t, m.StopProfile(t.Context(), "copied"))
			t.Log("live lock refused; basic-store persistent cookie works in a separate copied profile; source cookie DB unchanged")
		})
	}
}

func TestChromeCloseConversationExistingDormantHost(t *testing.T) {
	m, cfg := lifecycleChrome(t, "headless")
	i, err := m.Instance(t.Context(), DefaultProfile)
	must(t, err)
	_, err = i.NewTab(t.Context(), "chat", "agent", "about:blank")
	must(t, err)
	m.Close()
	pid := pidAt(t, i.dir, "chrome")
	next := New(cfg)
	defer next.Close()
	must(t, next.CloseConversation(t.Context(), "chat"))
	if next.ConversationHasTabs("chat") || !next.ProfileRunning(DefaultProfile) {
		t.Fatal("dormant host cleanup")
	}
	j, err := next.Instance(t.Context(), DefaultProfile)
	must(t, err)
	if len(j.Tabs("chat")) != 0 || pidAt(t, i.dir, "chrome") != pid {
		t.Fatal("cleanup restarted Chrome or failed to close owned tab")
	}
	must(t, next.StopProfile(t.Context(), DefaultProfile))
}

func TestLifecycleRegistryJSONShape(t *testing.T) {
	p := Profile{ID: "p", Name: "Profile", Color: "#123456", Kind: "managed", CDP: "private", CreatedAt: 1, LastUsedAt: 2}
	m := New(Config{DataDir: t.TempDir(), LookupProfile: func(string) (Profile, bool) { return p, true }})
	defer m.Close()
	i := newInstance(m, "p")
	defer i.cancel()
	raw, err := json.Marshal(i.Profile())
	must(t, err)
	var got Profile
	must(t, json.Unmarshal(raw, &got))
	if got.CDP != "" || got.CreatedAt != 0 || got.LastUsedAt != 0 || got.UsedBy != nil {
		t.Fatal(string(raw))
	}
	if got.ID != p.ID || got.Name != p.Name || got.Color != p.Color || got.Kind != p.Kind {
		t.Fatal(string(raw))
	}
	p.Name, p.Color = "Renamed", "#654321"
	if current := i.Profile(); current.Name != p.Name || current.Color != p.Color {
		t.Fatal(current)
	}
	if i.profile.Name == p.Name {
		t.Fatal("public lookup mutated running instance metadata")
	}
}

func TestChromeIdleRespectsViewersAndCalls(t *testing.T) {
	for _, mode := range []string{"headless", "xvfb"} {
		t.Run(mode, func(t *testing.T) {
			m, _ := lifecycleChrome(t, mode)
			m.cfg.Idle = 1200 * time.Millisecond
			i, err := m.Instance(t.Context(), DefaultProfile)
			must(t, err)
			a, err := i.NewTab(t.Context(), "chat", "agent", "about:blank")
			must(t, err)
			viewer, err := i.Watch(t.Context(), a.ID)
			must(t, err)
			defer viewer.Close()
			time.Sleep(2200 * time.Millisecond)
			if !m.ProfileRunning(DefaultProfile) {
				t.Fatal("stopped with viewer")
			}
			viewer.Close()
			result := make(chan error, 1)
			go func() {
				_, err := i.Call(t.Context(), a.ID, "Runtime.evaluate", map[string]any{
					"expression": "new Promise(resolve=>setTimeout(()=>resolve(42),3000))", "awaitPromise": true})
				result <- err
			}()
			time.Sleep(2200 * time.Millisecond)
			if !m.ProfileRunning(DefaultProfile) {
				t.Fatal("stopped during active call")
			}
			must(t, <-result)
			eventually(t, 6*time.Second, func() bool { return !m.ProfileRunning(DefaultProfile) })
			time.Sleep(1200 * time.Millisecond)
			if m.ProfileRunning(DefaultProfile) {
				t.Fatal("idle shutdown reconnected")
			}
			next, err := m.Instance(t.Context(), DefaultProfile)
			must(t, err)
			if len(next.Tabs("chat")) != 1 {
				t.Fatal("idle restart lost tab")
			}
			must(t, m.StopProfile(t.Context(), DefaultProfile))
			t.Log("viewer and active call prevent idle shutdown; idle shutdown stays stopped; next access restores tabs")
		})
	}
}

func TestChromeSandboxPreferenceLaunchAndProbe(t *testing.T) {
	for _, mode := range []string{"headless", "xvfb"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("RUNTIME_BROWSER_CHROME_ARGS", "--no-sandbox --disable-setuid-sandbox")
			m, _ := lifecycleChrome(t, mode)
			var preference atomic.Bool
			m.cfg.NoSandbox = preference.Load
			i, err := m.Instance(t.Context(), DefaultProfile)
			if err != nil {
				if !strings.Contains(err.Error(), "sandbox") {
					t.Fatal(err)
				}
				t.Log("sandbox unavailable; explicit false refuses manual no-sandbox override")
			} else {
				args, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pidAt(t, i.dir, "chrome")))
				must(t, err)
				if strings.Contains(string(args), "--no-sandbox") || strings.Contains(string(args), "--disable-setuid-sandbox") {
					t.Fatal("manual flags overrode page preference")
				}
				if s := m.Ready(t.Context()); strings.Contains(s.Sandbox, "DISABLED") {
					t.Fatal(s)
				}
				must(t, m.StopProfile(t.Context(), DefaultProfile))
			}
			preference.Store(true)
			i, err = m.Instance(t.Context(), DefaultProfile)
			must(t, err)
			args, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pidAt(t, i.dir, "chrome")))
			must(t, err)
			if !strings.Contains(string(args), "--no-sandbox") {
				t.Fatal("explicit preference missing from Chrome launch")
			}
			if s := m.Ready(t.Context()); s.Sandbox != "DISABLED explicitly by browser preference" {
				t.Fatal(s)
			}
			must(t, m.StopProfile(t.Context(), DefaultProfile))
			t.Log("page preference overrides manual flags in probe and host launch; readiness rechecks changed preference")
		})
	}
}
