package agentbrowser

import (
	"encoding/json"
	"testing"
	"time"
)

func TestChromePanelInputOpeningDialog(t *testing.T) {
	m, i := contractBrowser(t, "headless")
	tab, err := i.NewTab(t.Context(), "dialog-input", "person", "about:blank")
	must(t, err)
	evaluate(t, i, tab.ID, `document.body.innerHTML='<button onclick="window.answer=confirm(\'Continue?\')">Confirm</button><select onchange="window.answer=confirm(\'Select?\')"><option>A</option><option>B</option></select>'`)
	s := openContractSocket(t, m, "dialog-input")
	s.tabs(t, func(msg TabsMessage) bool { return msg.Current == tab.ID })
	for _, selector := range []string{"button", "select"} {
		var p struct{ X, Y float64 }
		must(t, json.Unmarshal(evaluate(t, i, tab.ID, "(()=>{const r=document.querySelector('"+selector+"').getBoundingClientRect();return {X:r.x+5,Y:r.y+5}})()"), &p))
		start := time.Now()
		s.send(t, Input{T: "mouse", Type: "mousePressed", Button: "left", X: p.X, Y: p.Y, ClickCount: 1})
		s.send(t, Input{T: "mouse", Type: "mouseReleased", Button: "left", X: p.X, Y: p.Y, ClickCount: 1})
		if selector == "select" {
			s.until(t, func(raw []byte) bool {
				var msg struct{ T string }
				must(t, json.Unmarshal(raw, &msg))
				return msg.T == "select"
			})
			s.send(t, Input{T: "select.choose", Index: 1})
		}
		s.tabs(t, func(msg TabsMessage) bool { return len(msg.Tabs) > 0 && msg.Tabs[0].Dialog != nil })
		s.send(t, Input{T: "dialog", Accept: true})
		s.tabs(t, func(msg TabsMessage) bool { return len(msg.Tabs) > 0 && msg.Tabs[0].Dialog == nil })
		if time.Since(start) > 3*time.Second {
			t.Fatal("dialog answer waited behind input", time.Since(start))
		}
		eventually(t, time.Second, func() bool { return string(evaluate(t, i, tab.ID, "answer")) == "true" })
	}
}
