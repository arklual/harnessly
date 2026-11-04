package agentbrowser

import (
	"testing"
	"time"
)

func TestChromePanelClipboard(t *testing.T) {
	m, i := contractBrowser(t, "headless")
	tab, err := i.AgentNewTab(t.Context(), "copy", "about:blank")
	must(t, err)
	evaluate(t, i, tab.ID, `document.body.innerHTML='<p>Selected words</p><input id="plain" value="partial text"><input id="secret" type="password" value="private password"><textarea id="area">textarea text</textarea>';true`)
	for _, item := range []struct{ script, want string }{
		{`const r=document.createRange();r.selectNodeContents(document.querySelector('p'));getSelection().removeAllRanges();getSelection().addRange(r);true`, "Selected words"},
		{`plain.focus();plain.setSelectionRange(0,7);true`, "partial"},
		{`area.focus();area.setSelectionRange(0,8);true`, "textarea"},
		{`secret.focus();secret.select();true`, ""},
		{`plain.focus();plain.setSelectionRange(0,0);true`, ""},
	} {
		evaluate(t, i, tab.ID, item.script)
		got, err := i.panelClipboard(t.Context(), tab.ID)
		must(t, err)
		if got != item.want {
			t.Fatalf("clipboard = %q, want %q", got, item.want)
		}
	}
	s := openContractSocket(t, m, "copy")
	s.tabs(t, func(msg TabsMessage) bool { return msg.Current == tab.ID })
	evaluate(t, i, tab.ID, `secret.focus();secret.select();window.copyKeys=0;document.onkeydown=e=>{if(e.ctrlKey && e.key==='c') copyKeys++};true`)
	s.send(t, Input{T: "clipboard.read", Key: "c", Code: "KeyC", KeyCode: 67, Ctrl: true})
	// The empty/password path must forward the key, not produce a clipboard payload.
	eventually(t, time.Second, func() bool { return string(evaluate(t, i, tab.ID, "copyKeys")) == "1" })
}
