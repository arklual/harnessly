package agentbrowser

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
)

// Panel protocol (/api/agent-browser?conv=<id>):
//
// Server text: {t:"status",state:"starting|up|down|error",error?,profile:{id,name}}
// and {t:"tabs",tabs:[{id,title,url,owner:"this|other",by,viewport,crashed}],current}.
// Server binary: [u32 BE width][u32 BE height][u32 BE gen][JPEG]. Dimensions are CSS
// pixels. Generation starts at zero and increments on every displayed-tab change;
// discard frames from older generations. A tabs message also carries gen so the
// panel can associate a generation with its selection before the first frame.
//
// Client text: Input (mouse, wheel, key, text, nav), {t:"tab.select",id},
// {t:"tab.new",url}, {t:"tab.close",id}, {t:"viewport",mode,w,h,dpr}.
// Input and viewport commands apply to current. The authenticated person may view
// and operate other conversations' tabs in this profile; agent APIs must filter
// by conversation separately. New tabs belong to conv and have by:"person".
// Authentication, same-origin and conversation existence checks precede Upgrade.
// Commands execute in socket order independently of frame delivery. Client messages
// are limited to 1 MiB; oversized messages return an error status without closing the socket.

// Status reports browser availability or a recoverable command error.
type Status struct {
	T       string  `json:"t"`
	State   string  `json:"state"`
	Error   string  `json:"error,omitempty"`
	Profile Profile `json:"profile"`
}

// PanelTab is the public metadata for one visible page.
type PanelTab struct {
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
	T       string     `json:"t"`
	Tabs    []PanelTab `json:"tabs"`
	Current string     `json:"current"`
	Gen     uint32     `json:"gen"`
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
	profile := Profile{ID: DefaultProfile, Name: "Common"}
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
	i, err := m.Instance(ctx, DefaultProfile)
	if err != nil {
		_ = write(Status{T: "status", State: "error", Error: err.Error(), Profile: profile})
		return
	}
	conv := r.URL.Query().Get("conv")
	// Commands retain socket order while the writer keeps delivering frames and state changes.
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case in := <-incoming:
				report(i.input(ctx, conv, in))
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
		current := i.Current(conv)
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
			tabs = append(tabs, PanelTab{t.ID, t.Title, t.URL, owner, t.By, t.Viewport, t.Crashed})
		}
		return write(TabsMessage{"tabs", tabs, current, gen})
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
		case err := <-failures:
			if write(Status{T: "status", State: "error", Error: err.Error(), Profile: i.Profile()}) != nil {
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
				if write(Status{T: "status", State: "error", Error: result.err.Error(), Profile: i.Profile()}) != nil {
					return
				}
			} else {
				viewer = result.viewer
				frames = viewer.Frames()
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

func (i *Instance) input(ctx context.Context, conv string, in Input) error {
	selected := i.Current(conv)
	switch in.T {
	case "tab.select":
		return i.Select(conv, in.ID)
	case "tab.new":
		_, err := i.NewTab(ctx, conv, "person", in.URL)
		return err
	case "tab.close":
		return i.CloseTab(ctx, in.ID)
	case "viewport":
		return i.SetViewport(ctx, selected, Viewport{in.Mode, in.W, in.H, in.DPR})
	default:
		for _, call := range in.Calls() {
			if _, err := i.Call(ctx, selected, call.Method, call.Params); err != nil {
				return err
			}
		}
	}
	return nil
}
