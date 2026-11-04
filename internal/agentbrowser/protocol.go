package agentbrowser

import (
	"context"
	"encoding/json"
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
type Status struct {
	T       string  `json:"t"`
	State   string  `json:"state"`
	Error   string  `json:"error,omitempty"`
	Profile Profile `json:"profile"`
}
type PanelTab struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	URL      string   `json:"url"`
	Owner    string   `json:"owner"`
	By       string   `json:"by"`
	Viewport Viewport `json:"viewport"`
	Crashed  bool     `json:"crashed"`
}
type TabsMessage struct {
	T       string     `json:"t"`
	Tabs    []PanelTab `json:"tabs"`
	Current string     `json:"current"`
	Gen     uint32     `json:"gen"`
}

func (m *Manager) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if sameorigin.Refuse(w, r) {
		return
	}
	// sameorigin also understands the forwarded host supplied by the runtime proxy.
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer ws.CloseNow()
	ws.SetReadLimit(64 << 10)
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
	if err = write(Status{T: "status", State: "starting", Profile: profile}); err != nil {
		return
	}
	incoming := make(chan Input, 32)
	go func() {
		defer cancel()
		for {
			typ, b, e := ws.Read(ctx)
			if e != nil {
				return
			}
			if typ != websocket.MessageText {
				continue
			}
			var in Input
			if json.Unmarshal(b, &in) != nil {
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
	changes, unsub := i.Changes()
	defer unsub()
	conv := r.URL.Query().Get("conv")
	var viewer *Viewer
	var frames <-chan Frame
	selected := ""
	var gen uint32
	defer func() {
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
		if current != selected || state == "up" && current != "" && viewer == nil {
			if viewer != nil {
				viewer.Close()
				viewer = nil
				frames = nil
			}
			if selected != current {
				gen++
			}
			selected = current
			if selected != "" && state == "up" {
				v, e := i.Watch(ctx, selected)
				if e != nil {
					return e
				}
				viewer = v
				frames = v.Frames()
			}
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
	if err = syncView(); err != nil {
		return
	}
	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-changes:
			if err = syncView(); err != nil {
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
		case in := <-incoming:
			switch in.T {
			case "tab.select":
				err = i.Select(conv, in.ID)
			case "tab.new":
				_, err = i.NewTab(ctx, conv, "person", in.URL)
			case "tab.close":
				err = i.CloseTab(ctx, in.ID)
			case "viewport":
				err = i.SetViewport(ctx, selected, Viewport{in.Mode, in.W, in.H, in.DPR})
			default:
				for _, call := range in.Calls() {
					_, err = i.Call(ctx, selected, call.Method, call.Params)
					if err != nil {
						break
					}
				}
			}
			if err != nil {
				if write(Status{T: "status", State: "error", Error: err.Error(), Profile: i.Profile()}) != nil {
					return
				}
			}
			if err = syncView(); err != nil {
				return
			}
		}
	}
}
