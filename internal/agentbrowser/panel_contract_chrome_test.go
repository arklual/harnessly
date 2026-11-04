package agentbrowser

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

type contractSocket struct {
	ws       *websocket.Conn
	messages chan []byte
	frames   chan uint32
	errors   chan error
	ctx      context.Context
}

func openContractSocket(t *testing.T, m *Manager, conv string) *contractSocket {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(m.ServeHTTP))
	t.Cleanup(server.Close)
	ctx, cancel := context.WithCancel(t.Context())
	ws, _, err := websocket.Dial(ctx, "ws"+server.URL[4:]+"?conv="+conv, nil)
	must(t, err)
	ws.SetReadLimit(8 << 20)
	s := &contractSocket{ws: ws, messages: make(chan []byte, 512), frames: make(chan uint32, 1),
		errors: make(chan error, 1), ctx: ctx}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			typ, raw, err := ws.Read(ctx)
			if err != nil {
				s.errors <- err
				return
			}
			if typ == websocket.MessageBinary {
				if len(raw) < 12 {
					s.errors <- errors.New("short frame")
					return
				}
				latest(s.frames, binary.BigEndian.Uint32(raw[8:]))
				continue
			}
			select {
			case s.messages <- raw:
			case <-ctx.Done():
				return
			}
		}
	}()
	t.Cleanup(func() { cancel(); _ = ws.CloseNow(); <-done })
	return s
}

func (s *contractSocket) send(t *testing.T, in Input) {
	t.Helper()
	raw, err := json.Marshal(in)
	must(t, err)
	ctx, cancel := context.WithTimeout(s.ctx, 3*time.Second)
	defer cancel()
	must(t, s.ws.Write(ctx, websocket.MessageText, raw))
}

func (s *contractSocket) until(t *testing.T, match func([]byte) bool) {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case raw := <-s.messages:
			var status Status
			must(t, json.Unmarshal(raw, &status))
			if status.T == "status" && status.State != "starting" && status.State != "up" {
				t.Fatalf("availability changed: %s", raw)
			}
			if match(raw) {
				return
			}
		case err := <-s.errors:
			t.Fatal(err)
		case <-timer.C:
			t.Fatal("panel condition timed out")
		}
	}
}

func (s *contractSocket) tabs(t *testing.T, match func(TabsMessage) bool) TabsMessage {
	t.Helper()
	var got TabsMessage
	s.until(t, func(raw []byte) bool {
		must(t, json.Unmarshal(raw, &got))
		return got.T == "tabs" && match(got)
	})
	return got
}

func (s *contractSocket) notice(t *testing.T, contains string) {
	t.Helper()
	s.until(t, func(raw []byte) bool {
		var n Notice
		must(t, json.Unmarshal(raw, &n))
		return n.T == "notice" && n.Level == "error" && strings.Contains(n.Message, contains)
	})
}

func (s *contractSocket) frame(t *testing.T, gen uint32) {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case g := <-s.frames:
			if g == gen {
				return
			}
		case err := <-s.errors:
			t.Fatal(err)
		case <-timer.C:
			t.Fatalf("no frame for generation %d", gen)
		}
	}
}

func TestChromePanelContract(t *testing.T) {
	for _, mode := range []string{"headless", "xvfb"} {
		t.Run(mode, func(t *testing.T) {
			m, i := contractBrowser(t, mode)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				fmt.Fprint(w, `<textarea id="text" autofocus></textarea><script>
let n=0;setInterval(()=>document.body.style.background=n++%2?'red':'blue',80);
</script>`)
			}))
			defer server.Close()
			t.Run("independent_views", func(t *testing.T) {
				a, err := i.AgentNewTab(t.Context(), "views", server.URL)
				must(t, err)
				s1 := openContractSocket(t, m, "views")
				s2 := openContractSocket(t, m, "views")
				for _, s := range []*contractSocket{s1, s2} {
					s.tabs(t, func(msg TabsMessage) bool { return msg.Current == a.ID && msg.Following })
				}
				s1.send(t, Input{T: "tab.new", URL: server.URL})
				pinned := s1.tabs(t, func(msg TabsMessage) bool { return msg.Current != a.ID && !msg.Following })
				person := pinned.Current
				if pinned.AgentTab != a.ID || i.Current("views") != a.ID {
					t.Fatal("person replaced agent working tab")
				}
				b, err := i.AgentNewTab(t.Context(), "views", server.URL)
				must(t, err)
				s1.tabs(t, func(msg TabsMessage) bool { return msg.AgentTab == b.ID && msg.Current == person && !msg.Following })
				following := s2.tabs(t, func(msg TabsMessage) bool {
					return msg.AgentTab == b.ID && msg.Current == b.ID && msg.Following
				})
				s2.frame(t, following.Gen)
				must(t, i.SetAgentTab("views", a.ID))
				s1.tabs(t, func(msg TabsMessage) bool { return msg.AgentTab == a.ID && msg.Current == person })
				s2.tabs(t, func(msg TabsMessage) bool { return msg.AgentTab == a.ID && msg.Current == a.ID })
				must(t, i.SetAgentTab("views", b.ID))
				s2.tabs(t, func(msg TabsMessage) bool { return msg.AgentTab == b.ID && msg.Current == b.ID })
				foreign, err := i.AgentNewTab(t.Context(), "other", server.URL)
				must(t, err)
				s1.send(t, Input{T: "tab.select", ID: foreign.ID})
				selected := s1.tabs(t, func(msg TabsMessage) bool { return msg.Current == foreign.ID && !msg.Following })
				s1.frame(t, selected.Gen)
				if selected.Gen <= pinned.Gen || i.Current("views") != b.ID {
					t.Fatal("selection affected agent or generation")
				}
				if err := i.SetAgentTab("views", foreign.ID); !errors.Is(err, ErrNotOwner) {
					t.Fatal(err)
				}
				evaluate(t, i, foreign.ID, `document.getElementById('text').focus();true`)
				s1.send(t, Input{T: "text", Text: "socket-one"})
				s1.send(t, Input{T: "viewport", Mode: "fixed", W: 900, H: 700, DPR: 1})
				s1.tabs(t, func(msg TabsMessage) bool {
					for _, tab := range msg.Tabs {
						if tab.ID == foreign.ID {
							return tab.Viewport.W == 900
						}
					}
					return false
				})
				if got := string(evaluate(t, i, foreign.ID, `document.getElementById('text').value`)); got != `"socket-one"` {
					t.Fatal(got)
				}
				if got := string(evaluate(t, i, b.ID, `document.getElementById('text').value`)); got != `""` {
					t.Fatal(got)
				}
				s1.send(t, Input{T: "nav", Action: "go", URL: server.URL + "/navigated"})
				s1.tabs(t, func(msg TabsMessage) bool {
					for _, tab := range msg.Tabs {
						if tab.ID == foreign.ID {
							return strings.HasSuffix(tab.URL, "/navigated")
						}
					}
					return false
				})
				s1.send(t, Input{T: "follow"})
				resumed := s1.tabs(t, func(msg TabsMessage) bool { return msg.Current == b.ID && msg.Following })
				if resumed.Gen <= selected.Gen {
					t.Fatal("follow did not advance generation")
				}
				s2.send(t, Input{T: "tab.select", ID: a.ID})
				s2.tabs(t, func(msg TabsMessage) bool { return msg.Current == a.ID && !msg.Following })
				must(t, i.AgentCloseTab(t.Context(), "views", a.ID))
				s2.tabs(t, func(msg TabsMessage) bool { return msg.Current == person && !msg.Following })
				must(t, i.CloseTab(t.Context(), person))
				s2.tabs(t, func(msg TabsMessage) bool { return msg.Current == b.ID && !msg.Following })
				must(t, i.AgentCloseTab(t.Context(), "views", b.ID))
				s2.tabs(t, func(msg TabsMessage) bool { return msg.Current == "" && msg.AgentTab == "" && msg.Following })
				s1.send(t, Input{T: "tab.new", URL: server.URL})
				first := s1.tabs(t, func(msg TabsMessage) bool { return msg.Current != "" && !msg.Following })
				if first.AgentTab != first.Current {
					t.Fatal("first human tab did not become working tab")
				}
				t.Log("two devices: follow/pin/new/foreign input/navigation/viewport/generation/close fallback verified")
			})

			t.Run("panel_opens_during_dialog", func(t *testing.T) {
				tab, err := i.AgentNewTab(t.Context(), "late-panel", server.URL)
				must(t, err)
				evaluate(t, i, tab.ID, `setTimeout(()=>alert('waiting'),50);true`)
				eventually(t, time.Second, func() bool { return i.Tabs("late-panel")[0].Dialog != nil })
				s := openContractSocket(t, m, "late-panel")
				s.tabs(t, func(msg TabsMessage) bool {
					for _, tab := range msg.Tabs {
						if tab.ID == msg.Current {
							return tab.Dialog != nil
						}
					}
					return false
				})
				start := time.Now()
				s.send(t, Input{T: "dialog", Accept: true})
				msg := s.tabs(t, func(msg TabsMessage) bool {
					for _, tab := range msg.Tabs {
						if tab.ID == msg.Current {
							return tab.Dialog == nil
						}
					}
					return false
				})
				if time.Since(start) > 2*time.Second {
					t.Fatal("dialog answer queued behind screencast")
				}
				s.frame(t, msg.Gen)
			})
			t.Run("dialogs_control_and_notices", func(t *testing.T) {
				a, err := i.AgentNewTab(t.Context(), "dialogs", server.URL)
				must(t, err)
				s := openContractSocket(t, m, "dialogs")
				s.tabs(t, func(msg TabsMessage) bool { return msg.Current == a.ID })
				call := func(conv string) error {
					_, err := i.AgentCall(t.Context(), conv, a.ID, "Runtime.evaluate", map[string]any{"expression": "1"})
					return err
				}
				if err := call("foreign"); !errors.Is(err, ErrNotOwner) {
					t.Fatal(err)
				}
				if err := i.AgentCloseTab(t.Context(), "foreign", a.ID); !errors.Is(err, ErrNotOwner) {
					t.Fatal(err)
				}
				s.send(t, Input{T: "control", Holder: "person"})
				s.tabs(t, func(msg TabsMessage) bool {
					for _, tab := range msg.Tabs {
						if tab.ID == a.ID {
							return tab.Control == "person"
						}
					}
					return false
				})
				if err := call("dialogs"); !errors.Is(err, ErrPersonControl) {
					t.Fatal(err)
				}
				if err := i.AgentCloseTab(t.Context(), "dialogs", a.ID); !errors.Is(err, ErrPersonControl) {
					t.Fatal(err)
				}
				evaluate(t, i, a.ID, "1")
				i.save()
				restored := newInstance(m, DefaultProfile)
				for _, tab := range restored.Tabs("dialogs") {
					if tab.ID == a.ID && tab.Control != "person" {
						t.Fatal("control not persisted")
					}
				}
				s.send(t, Input{T: "control", Holder: "agent"})
				s.tabs(t, func(msg TabsMessage) bool {
					for _, tab := range msg.Tabs {
						if tab.ID == a.ID {
							return tab.Control == "agent"
						}
					}
					return false
				})
				must(t, call("dialogs"))
				s.tabs(t, func(msg TabsMessage) bool {
					for _, tab := range msg.Tabs {
						if tab.ID == a.ID {
							return tab.AgentAt > 0
						}
					}
					return false
				})
				for _, dialog := range []struct {
					kind, expression, prompt, text, want string
					accept                               bool
				}{
					{"alert", `alert('message');window.answer='ack'`, "", "", `"ack"`, true},
					{"confirm", `window.answer=confirm('message')`, "", "", "false", false},
					{"prompt", `window.answer=prompt('message','default value')`,
						"default value", "entered value", `"entered value"`, true},
				} {
					evaluate(t, i, a.ID, "setTimeout(()=>{"+dialog.expression+"},50);true")
					s.tabs(t, func(msg TabsMessage) bool {
						for _, tab := range msg.Tabs {
							if tab.ID == a.ID && tab.Dialog != nil {
								if tab.Dialog.Type != dialog.kind || tab.Dialog.Message != "message" ||
									tab.Dialog.DefaultPrompt != dialog.prompt {
									t.Fatalf("dialog: %+v", tab.Dialog)
								}
								return true
							}
						}
						return false
					})
					start := time.Now()
					if err := call("dialogs"); !errors.Is(err, ErrDialogOpen) {
						t.Fatal(err)
					}
					_, err = i.Call(t.Context(), a.ID, "Runtime.evaluate", nil)
					if !errors.Is(err, ErrDialogOpen) {
						t.Fatal(err)
					}
					if err := i.SetViewport(t.Context(), a.ID, Viewport{"fixed", 800, 600, 1}); !errors.Is(err, ErrDialogOpen) {
						t.Fatal(err)
					}
					if time.Since(start) > time.Second {
						t.Fatal("dialog guard waited for renderer")
					}
					fast, cancel := context.WithTimeout(t.Context(), 2*time.Second)
					b, err := i.AgentNewTab(fast, "isolation", server.URL)
					must(t, err)
					_, err = i.AgentCall(fast, "isolation", b.ID, "Runtime.evaluate", map[string]any{"expression": "1"})
					must(t, err)
					must(t, i.AgentCloseTab(fast, "isolation", b.ID))
					cancel()
					s.send(t, Input{T: "nav", Action: "back"})
					s.notice(t, ErrDialogOpen.Error())
					s.send(t, Input{T: "tab.close", ID: "missing"})
					s.notice(t, "unknown browser tab")
					state, _ := i.State()
					if state != "up" {
						t.Fatal(state)
					}
					s.send(t, Input{T: "dialog", Accept: dialog.accept, Text: dialog.text})
					s.tabs(t, func(msg TabsMessage) bool {
						for _, tab := range msg.Tabs {
							if tab.ID == a.ID {
								return tab.Dialog == nil
							}
						}
						return false
					})
					if got := string(evaluate(t, i, a.ID, "window.answer")); got != dialog.want {
						t.Fatalf("%s answer=%s", dialog.kind, got)
					}
					must(t, call("dialogs"))
				}
				t.Log("alert/confirm/prompt answered through socket; ownership/control/dialog errors, AgentAt, " +
					"persistence, notices and up state verified")
			})
		})
	}
}
