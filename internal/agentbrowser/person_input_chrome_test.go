package agentbrowser

import (
	"errors"
	"testing"
	"time"
)

func TestPanelInputOpenAlertDoesNotWait(t *testing.T) {
	for _, mode := range []string{"headless", "xvfb"} {
		t.Run(mode, func(t *testing.T) {
			_, i := contractBrowser(t, mode)
			tab, err := i.AgentNewTab(t.Context(), "chat", "about:blank")
			must(t, err)
			evaluate(t, i, tab.ID, `setTimeout(()=>alert('Fixture'),0);true`)
			eventually(t, time.Second, func() bool { return i.Tabs("chat")[0].Dialog != nil })
			began := time.Now()
			_, err = i.Call(t.Context(), tab.ID, "Input.insertText", map[string]any{"text": "ordinary"})
			if !errors.Is(err, ErrDialogOpen) {
				t.Fatalf("open dialog input: %v", err)
			}
			elapsed := time.Since(began)
			t.Logf("panel input with alert took %s", elapsed)
			if elapsed > 600*time.Millisecond {
				t.Fatal("open dialog blocked panel input")
			}
			if i.Tabs("chat")[0].PersonDocument != unknownHandoffDocument {
				t.Fatal("failed metadata lookup did not guard its unknown document")
			}
			_, err = i.Call(t.Context(), tab.ID, "Input.dispatchKeyEvent", map[string]any{"type": "keyDown", "key": "a", "code": "KeyA"})
			must(t, err)
			if time.Since(began) > 600*time.Millisecond {
				t.Fatal("keyboard input waited behind the alert")
			}
			must(t, i.HandleDialog(t.Context(), tab.ID, true, ""))
		})
	}
}
