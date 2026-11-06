package agentbrowser

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func contractBrowser(t *testing.T, mode string) (*Manager, *Instance) {
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
	data := t.TempDir()
	m := New(Config{DataDir: data, Executable: exe, Chrome: chrome, Mode: mode})
	t.Cleanup(func() { m.Close(); cleanupHost(t, filepath.Join(data, "agent-browser", "profiles", DefaultProfile)) })
	i, err := m.Instance(t.Context(), DefaultProfile)
	must(t, err)
	return m, i
}

func TestChromeDialogMethods(t *testing.T) {
	for _, mode := range []string{"headless", "xvfb"} {
		t.Run(mode, func(t *testing.T) {
			_, i := contractBrowser(t, mode)
			tab, err := i.NewTab(t.Context(), "chat", "agent", "about:blank")
			must(t, err)
			_, err = i.Call(t.Context(), tab.ID, "Page.enable", nil)
			must(t, err)
			p, c, err := i.lookup(tab.ID)
			must(t, err)
			i.mu.Lock()
			b := p.binding
			i.mu.Unlock()
			for _, probe := range []struct {
				method string
				params any
			}{
				{"Runtime.evaluate", map[string]any{"expression": "1"}},
				{"Runtime.enable", nil},
				{"DOM.getDocument", nil},
				{"DOMSnapshot.captureSnapshot", map[string]any{"computedStyles": []string{}}},
				{"Accessibility.getFullAXTree", nil},
				{"Page.getLayoutMetrics", nil},
				{"Page.captureScreenshot", map[string]any{"format": "jpeg"}},
				{"Page.captureScreenshot", map[string]any{"format": "jpeg", "fromSurface": false}},
				{"Page.captureScreenshot", map[string]any{"format": "jpeg", "captureBeyondViewport": false}},
				{"Page.captureScreenshot", map[string]any{"format": "jpeg",
					"clip": map[string]any{"x": 0, "y": 0, "width": 800, "height": 600, "scale": 1}}},
				{"Page.getFrameTree", nil},
				{"Page.getResourceTree", nil},
				{"DOM.enable", nil},
				{"Accessibility.enable", nil},
				{"Input.insertText", map[string]any{"text": "test"}},
				{"Input.dispatchMouseEvent", map[string]any{"type": "mouseMoved", "x": 10, "y": 10}},
				{"Input.dispatchKeyEvent", map[string]any{"type": "keyUp", "key": "a", "code": "KeyA"}},
				{"Emulation.setDeviceMetricsOverride", map[string]any{
					"width": 800, "height": 600, "deviceScaleFactor": 1, "mobile": false}},
				{"Emulation.clearDeviceMetricsOverride", nil},
			} {
				baseline, stop := context.WithTimeout(t.Context(), 2*time.Second)
				_, baselineErr := c.Call(baseline, probe.method, probe.params, b.session)
				stop()
				must(t, baselineErr)
				if probe.method == "Emulation.setDeviceMetricsOverride" {
					_, err = c.Call(t.Context(), "Emulation.clearDeviceMetricsOverride", nil, b.session)
					must(t, err)
				}
				if probe.method == "Emulation.clearDeviceMetricsOverride" {
					_, err = c.Call(t.Context(), "Emulation.setDeviceMetricsOverride", map[string]any{
						"width": 800, "height": 600, "deviceScaleFactor": 1, "mobile": false}, b.session)
					must(t, err)
				}
				evaluate(t, i, tab.ID, `setTimeout(()=>alert('probe'),50);true`)
				eventually(t, time.Second, func() bool { return i.Tabs("chat")[0].Dialog != nil })
				ctx, cancel := context.WithTimeout(t.Context(), 400*time.Millisecond)
				start := time.Now()
				_, err = c.Call(ctx, probe.method, probe.params, b.session)
				cancel()
				t.Logf("%s %v: %v (%s)", probe.method, probe.params, err, time.Since(start).Round(time.Millisecond))
				if rendererCommand(probe.method) {
					if !errors.Is(err, context.DeadlineExceeded) {
						t.Fatalf("renderer command unexpectedly completed: %s: %v", probe.method, err)
					}
					_, guardErr := i.Call(t.Context(), tab.ID, probe.method, probe.params)
					if !errors.Is(guardErr, ErrDialogOpen) {
						t.Fatalf("missing guard for %s: %v", probe.method, guardErr)
					}
				} else {
					must(t, err)
				}

				_, err = c.Call(t.Context(), "Page.handleJavaScriptDialog", map[string]any{"accept": true}, b.session)
				must(t, err)
				eventually(t, time.Second, func() bool { return i.Tabs("chat")[0].Dialog == nil })
			}
		})
	}
}

func TestChromeDialogReconnect(t *testing.T) {
	for _, mode := range []string{"headless", "xvfb"} {
		t.Run(mode, func(t *testing.T) {
			m, i := contractBrowser(t, mode)
			tab, err := i.AgentNewTab(t.Context(), "chat", "about:blank")
			must(t, err)
			evaluate(t, i, tab.ID, `setTimeout(()=>alert('survives'),50);true`)
			eventually(t, time.Second, func() bool { return i.Tabs("chat")[0].Dialog != nil })
			must(t, i.SetControl(tab.ID, "person"))

			m.Close()
			reconnected := New(m.cfg)
			t.Cleanup(reconnected.Close)
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			i, err = reconnected.InstanceFor(ctx, "chat")
			must(t, err)

			if got := string(evaluate(t, i, tab.ID, "1")); got != "1" {
				t.Fatal(got)
			}
			if i.Tabs("chat")[0].Control != "person" {
				t.Fatal("control lost after reconnect")
			}
			evaluate(t, i, tab.ID, `setTimeout(()=>alert('new session'),50);true`)
			eventually(t, time.Second, func() bool { return i.Tabs("chat")[0].Dialog != nil })
			must(t, i.HandleDialog(t.Context(), tab.ID, true, ""))
			eventually(t, time.Second, func() bool { return i.Tabs("chat")[0].Dialog == nil })
			t.Log("relay cancelled the dialog before detach; reconnect works; " +
				"new dialog is observable and answerable; control persisted")

		})
	}
}
