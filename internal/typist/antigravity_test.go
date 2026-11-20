package typist

import (
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

func nativeFixture(text string) string {
	lines := strings.Split(text, "\n")
	box := "> " + lines[0]
	for _, line := range lines[1:] {
		box += "\n  " + line
	}
	footer := "Gemini 3.8 Flash · high"
	if text == "" {
		footer = "? for shortcuts    " + footer
	}
	return "Antigravity CLI\n" + strings.Repeat("─", 80) + "\n" + box + "\n" + strings.Repeat("─", 80) + "\n" + footer
}

type nativeTestClock struct{}

func (nativeTestClock) Now() time.Time      { return time.Now() }
func (nativeTestClock) Sleep(time.Duration) {}

type nativeTestPane struct {
	text            string
	alive           bool
	cancel          bool
	pasted, entered int
}

func (p *nativeTestPane) NativeAgentAlive() bool        { return p.alive }
func (p *nativeTestPane) CaptureStyled() (string, bool) { return nativeFixture(p.text), true }
func (p *nativeTestPane) Capture() (string, bool)       { return p.CaptureStyled() }
func (p *nativeTestPane) Paste(text string) error {
	p.pasted++
	p.text = text
	if p.cancel {
		p.alive = false
	}
	return nil
}
func (p *nativeTestPane) TypeLine(string) error { panic("native delivery must not call TypeLine") }
func (p *nativeTestPane) Keys(keys ...string) error {
	if len(keys) != 1 || keys[0] != "Enter" {
		panic("unexpected native key")
	}
	p.entered++
	return nil
}

func TestAntigravityInputFrames(t *testing.T) {
	for _, text := range []string{"", "hello", "first line\nsecond line"} {
		s := Read(nativeFixture(text), wire.ProviderAntigravity)
		if !s.PromptReady() || s.Box != text {
			t.Fatalf("frame %q: %+v", text, s)
		}
	}
	for _, styled := range []string{"$ ", "Choose your color scheme:\n > terminal", "Do you trust this folder?\n> Yes\n  No", strings.Replace(nativeFixture(""), "? for shortcuts", "esc to interrupt", 1)} {
		if s := Read(styled, wire.ProviderAntigravity); s.PromptReady() {
			t.Fatalf("unsafe ready screen: %q", styled)
		}
	}
}

func TestAntigravitySafeDelivery(t *testing.T) {
	for _, tc := range []struct {
		name         string
		pane         nativeTestPane
		message      Message
		outcome      Outcome
		paste, enter int
	}{
		{"normal", nativeTestPane{alive: true}, Message{Text: "hello"}, Sent, 1, 1},
		{"own", nativeTestPane{alive: true, text: "hello"}, Message{Text: "hello"}, Sent, 0, 1},
		{"foreign", nativeTestPane{alive: true, text: "other input"}, Message{Text: "hello"}, BlockedText, 0, 0},
		{"shell", nativeTestPane{alive: false}, Message{Text: "echo unsafe"}, Wait, 0, 0},
		{"dies-after-paste", nativeTestPane{alive: true, cancel: true}, Message{Text: "hello"}, Failed, 1, 0},
		{"no-steering", nativeTestPane{alive: true}, Message{Text: "hello", Steer: true}, Wait, 0, 0},
		{"no-images", nativeTestPane{alive: true}, Message{Text: "hello", Images: []string{"/tmp/p.png"}}, Wait, 0, 0},
		{"no-extra-enter", nativeTestPane{alive: true}, Message{Text: "hello", EnterOnly: true}, Wait, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := tc.pane
			r := Type(tc.message, wire.ProviderAntigravity, &p, nativeTestClock{})
			if r.Outcome != tc.outcome || p.pasted != tc.paste || p.entered != tc.enter {
				t.Fatalf("result %+v, pasted=%d entered=%d", r, p.pasted, p.entered)
			}
		})
	}
}
