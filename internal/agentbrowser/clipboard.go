package agentbrowser

import (
	"context"
	"encoding/json"
)

// Only the authenticated panel's explicit copy shortcut uses this path. Never
// fall back to an input's value: password selections must not leave Chrome.
func (i *Instance) panelClipboard(ctx context.Context, id string) (string, error) {
	raw, err := i.Call(ctx, id, "Runtime.evaluate", map[string]any{
		"returnByValue": true,
		"expression": `(()=>{
   let doc=document, root=doc, el;
   for(let n=0;n<32;n++) {
    el=root.activeElement;
    if(el?.shadowRoot?.activeElement) { root=el.shadowRoot; continue; }
    if(el?.tagName==='IFRAME') {
     try { if(el.contentDocument) { doc=el.contentDocument; root=doc; continue; } } catch {}
    }
    break;
   }
   if(el?.tagName==='INPUT' && el.type==='password') return '';
   if(el?.tagName==='INPUT' || el?.tagName==='TEXTAREA') {
    const a=el.selectionStart,b=el.selectionEnd;
    return typeof a==='number' && typeof b==='number' ? el.value.slice(a,b) : '';
   }
   return (root.getSelection?.() ?? doc.getSelection())?.toString() ?? '';
  })()`,
	})
	if err != nil {
		return "", err
	}
	var r struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return "", err
	}
	return r.Result.Value, nil
}
