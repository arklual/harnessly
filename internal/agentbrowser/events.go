package agentbrowser

import (
	"encoding/base64"
	"encoding/json"
)

func (i *Instance) event(method, session string, raw json.RawMessage) {
	switch method {
	case "Target.targetCreated", "Target.targetInfoChanged":
		var e struct {
			Target target `json:"targetInfo"`
		}
		if json.Unmarshal(raw, &e) != nil {
			return
		}
		i.mu.Lock()
		p := i.upsertLocked(e.Target)
		c := i.conn
		i.mu.Unlock()
		if p != nil && c != nil && method == "Target.targetCreated" {
			i.schedulePrepare(p, c)
		}
	case "Target.targetDestroyed":
		var e struct {
			ID string `json:"targetId"`
		}
		if json.Unmarshal(raw, &e) != nil {
			return
		}
		i.mu.Lock()
		i.removeLocked(e.ID)
		i.mu.Unlock()
	case "Target.detachedFromTarget":
		var e struct {
			Session string `json:"sessionId"`
		}
		if json.Unmarshal(raw, &e) != nil {
			return
		}
		i.mu.Lock()
		for _, p := range i.pages {
			if p.binding.session == e.Session {
				p.binding = binding{}
			}
		}
		i.mu.Unlock()
	case "Inspector.targetCrashed":
		i.mu.Lock()
		for _, p := range i.pages {
			if p.binding.session == session {
				p.Crashed = true
			}
		}
		i.changedLocked()
		i.mu.Unlock()
	case "Page.javascriptDialogOpening", "Page.javascriptDialogClosed":
		var dialog *Dialog
		if method == "Page.javascriptDialogOpening" {
			dialog = &Dialog{}
			if json.Unmarshal(raw, dialog) != nil {
				return
			}
		}
		i.mu.Lock()
		for _, p := range i.pages {
			if p.binding.session == session {
				p.Dialog = dialog
			}
		}
		// Dialogs are live session state, not a durable entry in tabs.json.
		i.notifyLocked()
		i.mu.Unlock()
	case "Page.screencastFrame":
		var f struct {
			Data     string `json:"data"`
			ID       int64  `json:"sessionId"`
			Metadata struct {
				W float64 `json:"deviceWidth"`
				H float64 `json:"deviceHeight"`
			} `json:"metadata"`
		}
		if json.Unmarshal(raw, &f) != nil {
			return
		}
		jpeg, err := base64.StdEncoding.DecodeString(f.Data)
		i.mu.Lock()
		defer i.mu.Unlock()
		i.ackQueue = append(i.ackQueue, ack{i.conn, session, f.ID})
		if err != nil {
			return
		}
		frame := Frame{int(f.Metadata.W), int(f.Metadata.H), jpeg}
		for _, p := range i.pages {
			if p.binding.session != session {
				continue
			}
			p.last = &frame
			p.framePending = true
			break
		}
	}
}

func latest[T any](ch chan T, v T) {
	select {
	case <-ch:
	default:
	}
	select {
	case ch <- v:
	default:
	}
}
