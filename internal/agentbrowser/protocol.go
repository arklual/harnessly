package agentbrowser

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
)

// Panel protocol contract: /api/agent-browser?conv=<conversation ID>.
// Authentication, same-origin and conversation existence checks precede Upgrade.
// The profile is resolved through Manager.InstanceFor; all visible tabs in that
// profile are listed, including other conversations' tabs (owner:"other").
//
// Server text messages:
//   {t:"status",state:"starting|up|down|error",error?,profile:{id,name}}
//     Availability only. A command failure never changes browser status.
//     Before profile resolution, id and name are empty.
//   {t:"notice",level:"error",message}
//     A recoverable command, input validation or streaming error.
//   {t:"tabs",tabs:[{id,title,url,owner:"this|other",by:"agent|person",
//     viewport:{mode,w,h,dpr},crashed,control:"agent|person",agentAt,
//     dialog?:{type,message,defaultPrompt}}],current,agentTab,following,gen}
//     Tabs are in persistent creation order. agentAt is the last successful agent
//     call's Unix time in milliseconds (zero until then). dialog is absent when closed.
//     current is THIS SOCKET's displayed tab, agentTab is the chat's working tab;
//     either may be empty. following says whether current follows the agent.
//   {t:"select",x,y,w,h,options:[{value,label,disabled}],selected}
//     A native single select intercepted by this socket; coordinates are CSS pixels.
// Client also sends {t:"select.choose",index} or {t:"select.dismiss"}.
// Client sends {t:"clipboard.read",key,code,keyCode,ctrl?,meta?} only for a person's copy shortcut.
// Server replies {t:"clipboard",text}; with no selection it forwards the original key.
// Password input selections are never returned. This is not an agent tool.
// Server binary: [u32 BE width][u32 BE height][u32 BE gen][JPEG]. Dimensions are
// CSS pixels. gen starts at zero and increases only when current changes. A tabs
// message announces gen before its frames; discard frames from older generations.
//
// Client text messages (commands execute in socket order, independently of frames):
//   {t:"tab.select",id} pins this socket to any listed tab, including owner:"other".
//   {t:"tab.new",url} creates a by:"person" tab owned by conv and pins it here.
//   {t:"tab.close",id} closes the specified tab, regardless of socket selection.
//   {t:"follow"} removes the pin and resumes following agentTab (the default).
//   {t:"control",holder:"agent|person"} persists control of the displayed tab.
//   {t:"dialog",accept:boolean,text?:string} answers its JavaScript dialog.
//   {t:"viewport",mode:"fit|fixed|mobile",w,h,dpr} resizes the displayed tab.
//   {t:"mouse",type:"mousePressed|mouseReleased|mouseMoved",x,y,button?,buttons?,clickCount?}
//   {t:"wheel",x,y,dx,dy}
//   {t:"key",key,code,keyCode,alt?,ctrl?,meta?,shift?} sends a press/release pair.
//   {t:"text",text} inserts text; {t:"nav",action:"go|back|forward|reload",url?} navigates.
// Mouse and wheel also accept alt/ctrl/meta/shift. Coordinates use CSS pixels.
// Input, navigation, viewport, control and dialog operate on the displayed tab
// at execution time, without changing agentTab. URLs without a scheme gain https://.
// If a pinned tab disappears, the next tab of conv by creation order is selected,
// falling back to the previous tab; with no tabs of conv the socket follows again.
// A person's new tab becomes agentTab only if no working tab exists. AgentNewTab
// and SetAgentTab change agentTab, but never change a pinned socket's selection.
// Disconnecting the panel does not dismiss dialogs. Disconnecting the runtime from
// its managed host cancels pending dialogs before detaching its CDP sessions.
// Messages over 1 MiB are drained and rejected with notice without closing the socket.

// Status reports browser availability, never individual command failures.
type Status struct {
	T       string  `json:"t"`
	State   string  `json:"state"`
	Error   string  `json:"error,omitempty"`
	Profile Profile `json:"profile"`
}

// Notice reports a recoverable failure without changing browser availability.
type Notice struct {
	T       string `json:"t"`
	Level   string `json:"level"`
	Message string `json:"message"`
}

// PanelTab is the public metadata for one visible page.
type PanelTab struct {
	Control  string   `json:"control"`
	AgentAt  int64    `json:"agentAt"`
	Dialog   *Dialog  `json:"dialog,omitempty"`
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	URL      string   `json:"url"`
	Owner    string   `json:"owner"`
	By       string   `json:"by"`
	Viewport Viewport `json:"viewport"`
	Crashed  bool     `json:"crashed"`
}

// TabsMessage pairs a selection with the generation used by subsequent frames.
type TabsMessage struct {
	AgentTab  string     `json:"agentTab"`
	Following bool       `json:"following"`
	T         string     `json:"t"`
	Tabs      []PanelTab `json:"tabs"`
	Current   string     `json:"current"`
	Gen       uint32     `json:"gen"`
}

const panelReadLimit = 1 << 20

type watchResult struct {
	gen    uint32
	viewer *Viewer
	err    error
}

// ServeHTTP serves an authenticated panel; the caller checks session and conversation access.
func (m *Manager) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if sameorigin.Refuse(w, r) {
		return
	}
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer ws.CloseNow()
	// The library's limit closes the connection. Enforce our limit while streaming instead,
	// so an oversized paste can be rejected without destroying the panel's session.
	ws.SetReadLimit(-1)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	write := func(v any) error {
		b, e := json.Marshal(v)
		if e != nil {
			return e
		}
		wctx, stop := context.WithTimeout(ctx, 5*time.Second)
		defer stop()
		return ws.Write(wctx, websocket.MessageText, b)
	}
	profile := Profile{} // Identity is known after resolving the conversation binding.
	if write(Status{T: "status", State: "starting", Profile: profile}) != nil {
		return
	}
	incoming := make(chan Input, 32)
	failures := make(chan error, 32)
	report := func(err error) {
		if err == nil {
			return
		}
		select {
		case failures <- err:
		case <-ctx.Done():
		}
	}
	go func() {
		defer cancel()
		for {
			typ, reader, e := ws.Reader(ctx)
			if e != nil {
				return
			}
			b, e := io.ReadAll(io.LimitReader(reader, panelReadLimit+1))
			if e != nil {
				return
			}
			if len(b) > panelReadLimit {
				// Drain without retaining the payload; the next message remains readable.
				timer := time.AfterFunc(5*time.Second, func() { _ = ws.CloseNow() })
				_, e = io.Copy(io.Discard, reader)
				timer.Stop()
				if e != nil {
					return
				}
				report(errors.New("panel message exceeds 1 MiB"))
				continue
			}
			if typ != websocket.MessageText {
				continue
			}
			var in Input
			if e = json.Unmarshal(b, &in); e != nil {
				report(errors.New("invalid panel message"))
				continue
			}
			select {
			case incoming <- in:
			case <-ctx.Done():
				return
			}
		}
	}()
	conv := r.URL.Query().Get("conv")
	i, err := m.InstanceFor(ctx, conv)
	if err != nil {
		_ = write(Status{T: "status", State: "error", Error: err.Error(), Profile: profile})
		return
	}
	view := newPanelView()
	outgoing := make(chan json.RawMessage, 8)
	// Commands retain socket order while the writer keeps delivering frames and state changes.
	go func() {
		var selection panelSelect
		defer func() {
			cleanup, stop := context.WithTimeout(context.Background(), time.Second)
			defer stop()
			selection.clear(cleanup, i)
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case in := <-incoming:
				selected, _, _, _ := view.resolve(i, conv)
				if in.T == "clipboard.read" {
					text, err := i.panelClipboard(ctx, selected)
					if err != nil {
						report(err)
						continue
					}
					if text == "" {
						in.T = "key"
						report(view.input(ctx, i, conv, in))
					} else {
						raw, _ := json.Marshal(struct {
							T    string `json:"t"`
							Text string `json:"text"`
						}{"clipboard", text})
						select {
						case outgoing <- raw:
						case <-ctx.Done():
						}
					}
					continue
				}
				if in.T == "select.choose" {
					report(selection.choose(ctx, i, selected, in.Index))
					continue
				}
				if in.T == "select.dismiss" {
					selection.clear(ctx, i)
					continue
				}
				if in.T == "mouse" && in.Type == "mouseReleased" && selection.swallowRelease {
					selection.swallowRelease = false
					continue
				}
				if in.T == "mouse" && in.Type == "mousePressed" && in.Button == "left" {
					if menu := selection.hit(ctx, i, selected, in); menu != nil {
						select {
						case outgoing <- menu:
						case <-ctx.Done():
						}
						continue
					}
				} else if in.T != "mouse" {
					selection.clear(ctx, i)
				}
				report(view.input(ctx, i, conv, in))
			}
		}
	}()
	changes, unsub := i.Changes()
	defer unsub()
	results := make(chan watchResult)
	var viewer *Viewer
	var frames <-chan Frame
	var watchCancel context.CancelFunc
	selected := ""
	var gen uint32
	pending := false
	defer func() {
		if watchCancel != nil {
			watchCancel()
		}
		if viewer != nil {
			viewer.Close()
		}
	}()
	syncView := func() error {
		state, reason := i.State()
		if e := write(Status{T: "status", State: state, Error: reason, Profile: i.Profile()}); e != nil {
			return e
		}
		current, agentTab, following, busy := view.resolve(i, conv)
		if busy {
			return nil
		}
		if current != selected {
			gen++
			if watchCancel != nil {
				watchCancel()
			}
			pending = false
			if viewer != nil {
				viewer.Close()
				viewer = nil
				frames = nil
			}
			selected = current
		}
		if state == "up" && selected != "" && viewer == nil && !pending {
			watchCtx, stop := context.WithCancel(ctx)
			watchCancel = stop
			pending = true
			id, g := selected, gen
			go func() {
				v, e := i.Watch(watchCtx, id)
				select {
				case results <- watchResult{g, v, e}:
				case <-watchCtx.Done():
					if v != nil {
						v.Close()
					}
				}
			}()
		}
		tabs := []PanelTab{}
		for _, t := range i.Tabs("") {
			owner := "other"
			if t.Conversation == conv {
				owner = "this"
			}
			tabs = append(tabs, PanelTab{ID: t.ID, Title: t.Title, URL: t.URL, Owner: owner, By: t.By,
				Viewport: t.Viewport, Crashed: t.Crashed, Control: t.Control, AgentAt: t.AgentAt, Dialog: t.Dialog})
		}
		return write(TabsMessage{T: "tabs", Tabs: tabs, Current: current, Gen: gen, AgentTab: agentTab, Following: following})
	}
	if syncView() != nil {
		return
	}
	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case menu := <-outgoing:
			if write(menu) != nil {
				return
			}
		case err := <-failures:
			if write(Notice{T: "notice", Level: "error", Message: err.Error()}) != nil {
				return
			}
		case result := <-results:
			if result.gen != gen {
				if result.viewer != nil {
					result.viewer.Close()
				}
				continue
			}
			pending = false
			if result.err != nil {
				if write(Notice{T: "notice", Level: "error", Message: result.err.Error()}) != nil {
					return
				}
			} else {
				viewer = result.viewer
				frames = viewer.Frames()
			}
		case <-view.changes:
			if syncView() != nil {
				return
			}
		case <-changes:
			if syncView() != nil {
				return
			}
		case f := <-frames:
			wctx, stop := context.WithTimeout(ctx, 5*time.Second)
			err = ws.Write(wctx, websocket.MessageBinary, f.Encode(gen))
			stop()
			if err != nil {
				return
			}
		case <-ping.C:
			pctx, stop := context.WithTimeout(ctx, 5*time.Second)
			err = ws.Ping(pctx)
			stop()
			if err != nil {
				return
			}
		}
	}
}

// panelView belongs to one socket. The ordered command worker and writer share only
// this short metadata lock, never a lock held while awaiting Chrome or a network write.
type panelView struct {
	mu      sync.Mutex
	pinned  bool
	id      string
	order   uint64
	busy    bool
	changes chan struct{}
}

func newPanelView() *panelView { return &panelView{changes: make(chan struct{}, 1)} }

func (v *panelView) resolve(i *Instance, conv string) (current, agent string, following, busy bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	agent = i.Current(conv)
	if v.pinned {
		found := false
		for _, t := range i.Tabs("") {
			if t.ID == v.id {
				found = true
				break
			}
		}
		if !found {
			tabs := i.Tabs(conv)
			if len(tabs) == 0 {
				v.pinned = false
			} else {
				next := tabs[len(tabs)-1]
				for _, t := range tabs {
					if t.Order > v.order {
						next = t
						break
					}
				}
				v.id, v.order = next.ID, next.Order
			}
		}
	}
	if v.pinned {
		current = v.id
	} else {
		current = agent
	}
	return current, agent, !v.pinned, v.busy
}

func (v *panelView) pin(t Tab) {
	v.mu.Lock()
	v.pinned, v.id, v.order = true, t.ID, t.Order
	v.mu.Unlock()
	latest(v.changes, struct{}{})
}

func (v *panelView) input(ctx context.Context, i *Instance, conv string, in Input) error {
	selected, _, _, _ := v.resolve(i, conv)
	switch in.T {
	case "follow":
		v.mu.Lock()
		v.pinned = false
		v.mu.Unlock()
		latest(v.changes, struct{}{})
	case "tab.select":
		for _, t := range i.Tabs("") {
			if t.ID == in.ID {
				v.pin(t)
				return nil
			}
		}
		return errors.New("unknown browser tab")
	case "tab.new":
		// Keep the previous frame stream alive while creation acquires its final selection.
		v.mu.Lock()
		v.busy = true
		v.mu.Unlock()
		defer func() { v.mu.Lock(); v.busy = false; v.mu.Unlock(); latest(v.changes, struct{}{}) }()
		tab, err := i.NewTab(ctx, conv, "person", in.URL)
		if err == nil {
			v.pin(tab)
		}
		return err
	case "tab.close":
		return i.CloseTab(ctx, in.ID)
	case "control":
		return i.SetControl(selected, in.Holder)
	case "dialog":
		return i.HandleDialog(ctx, selected, in.Accept, in.Text)
	case "viewport":
		return i.SetViewport(ctx, selected, Viewport{in.Mode, in.W, in.H, in.DPR})
	default:
		calls := in.Calls()
		if len(calls) == 0 {
			return errors.New("invalid panel command")
		}
		for _, call := range calls {
			if _, err := i.panelCall(ctx, selected, call.Method, call.Params); err != nil {
				return err
			}
		}
	}
	return nil
}
