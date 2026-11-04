package agentbrowser

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestChromeSelect(t *testing.T) {
	m, i := contractBrowser(t, "headless")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<select id="plain"><option value="same">First</option><option value="same">Second</option>
<option disabled>Disabled</option><optgroup disabled><option>Group disabled</option></optgroup></select>
<div id="shadow"></div><iframe id="frame" srcdoc="<select><option>A</option><option>B</option></select>"></iframe>
<script>document.querySelector('#shadow').attachShadow({mode:'open'}).innerHTML='<select><option>A</option><option>B</option></select>';
window.events=[];plain.oninput=()=>events.push('input');plain.onchange=()=>events.push('change');</script>`)
	}))
	defer server.Close()
	tab, err := i.NewTab(t.Context(), "select", "agent", server.URL)
	must(t, err)
	eventually(t, 3*time.Second, func() bool { return string(evaluate(t, i, tab.ID, "!!document.querySelector('#plain')")) == "true" })
	s := openContractSocket(t, m, "select")
	s.tabs(t, func(msg TabsMessage) bool { return msg.Current == tab.ID })
	for _, expr := range []string{"document.querySelector('#plain')", "document.querySelector('#shadow').shadowRoot.querySelector('select')", "document.querySelector('#frame')"} {
		var p struct{ X, Y float64 }
		raw := evaluate(t, i, tab.ID, "(()=>{let r=("+expr+").getBoundingClientRect();return {X:r.x+15,Y:r.y+15}})()")
		must(t, json.Unmarshal(raw, &p))
		s.send(t, Input{T: "mouse", Type: "mousePressed", Button: "left", X: p.X, Y: p.Y})
		s.until(t, func(raw []byte) bool {
			var msg struct{ T string }
			must(t, json.Unmarshal(raw, &msg))
			return msg.T == "select"
		})
		s.send(t, Input{T: "mouse", Type: "mouseReleased", Button: "left", X: p.X, Y: p.Y})
		s.send(t, Input{T: "select.choose", Index: 1})
	}
	eventually(t, 3*time.Second, func() bool {
		return string(evaluate(t, i, tab.ID, `document.querySelector('#shadow').shadowRoot.querySelector('select').selectedIndex`)) == "1" &&
			string(evaluate(t, i, tab.ID, `document.querySelector('#frame').contentDocument.querySelector('select').selectedIndex`)) == "1"
	})
	eventually(t, 3*time.Second, func() bool {
		return string(evaluate(t, i, tab.ID, "JSON.stringify([plain.selectedIndex,events])")) == `"[1,[\"input\",\"change\"]]"`
	})
}

func TestChromeSelectFallbackAndStale(t *testing.T) {
	_, i := contractBrowser(t, "headless")
	tab, err := i.NewTab(t.Context(), "select", "person", "about:blank")
	must(t, err)
	evaluate(t, i, tab.ID, `document.body.innerHTML='<select id="choice"><option>A</option><option disabled>B</option></select><button id="button">Click</button>';window.clicks=0;button.onclick=()=>clicks++`)
	var selection panelSelect
	defer selection.clear(t.Context(), i)
	point := func(expr string) Input {
		var p struct{ X, Y float64 }
		must(t, json.Unmarshal(evaluate(t, i, tab.ID, "(()=>{const r=("+expr+").getBoundingClientRect();return {X:r.x+5,Y:r.y+5}})()"), &p))
		return Input{T: "mouse", Type: "mousePressed", Button: "left", X: p.X, Y: p.Y, ClickCount: 1}
	}
	for _, attribute := range []string{"multiple", "disabled", "size"} {
		evaluate(t, i, tab.ID, `choice.setAttribute("`+attribute+`","2")`)
		if selection.hit(t.Context(), i, tab.ID, point("choice")) != nil {
			t.Fatal("intercepted", attribute)
		}
		evaluate(t, i, tab.ID, `choice.removeAttribute("`+attribute+`")`)
	}
	in := point("button")
	if selection.hit(t.Context(), i, tab.ID, in) != nil {
		t.Fatal("intercepted button")
	}
	v := newPanelView()
	must(t, v.input(t.Context(), i, "select", in))
	in.Type = "mouseReleased"
	must(t, v.input(t.Context(), i, "select", in))
	eventually(t, time.Second, func() bool { return string(evaluate(t, i, tab.ID, "clicks")) == "1" })
	if selection.hit(t.Context(), i, tab.ID, point("choice")) == nil {
		t.Fatal("no menu")
	}
	must(t, selection.choose(t.Context(), i, tab.ID, 1))
	if string(evaluate(t, i, tab.ID, "choice.selectedIndex")) != "0" {
		t.Fatal("chose disabled option")
	}
	if selection.hit(t.Context(), i, tab.ID, point("choice")) == nil {
		t.Fatal("no menu")
	}
	if selection.choose(t.Context(), i, "another-tab", 0) == nil {
		t.Fatal("accepted stale selection")
	}
}
