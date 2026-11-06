package agentbrowser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Native select popups are absent from both screencasts and screenshots. Keep a
// remote reference per socket so duplicate values and nested documents retain identity.
const selectHit = `(x,y) => {
 let root=document, el, ox=0, oy=0;
 for(let depth=0;depth<32;depth++) {
  el=root.elementFromPoint(x,y);
  if(!el) return null;
  if(el.shadowRoot) { root=el.shadowRoot; continue; }
  if(el.tagName==='IFRAME') {
   const r=el.getBoundingClientRect();
   try { if(!el.contentDocument) return null; root=el.contentDocument; }
   catch { return null; }
   const dx=r.left+el.clientLeft, dy=r.top+el.clientTop;
   x-=dx; y-=dy; ox+=dx; oy+=dy; continue;
  }
  break;
 }
 if(!el || el.tagName!=='SELECT' || el.multiple || el.size>1 || el.matches(':disabled')) return null;
 const r=el.getBoundingClientRect();
 return {element:el, menu:{t:'select',x:r.left+ox,y:r.top+oy,w:r.width,h:r.height,
  options:Array.from(el.options,o=>({value:o.value,label:o.label,
   disabled:o.disabled || (o.parentElement.tagName==='OPTGROUP' && o.parentElement.disabled)})),
  selected:el.selectedIndex}};
}`

type panelSelect struct {
	tab, object    string
	swallowRelease bool
}

func (s *panelSelect) clear(ctx context.Context, i *Instance) {
	if s.object != "" {
		_, _ = i.Call(ctx, s.tab, "Runtime.releaseObject", map[string]any{"objectId": s.object})
	}
	s.object, s.tab = "", ""
	s.swallowRelease = false
}

func (s *panelSelect) hit(ctx context.Context, i *Instance, tab string, in Input) json.RawMessage {
	s.clear(ctx, i)
	ctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	raw, err := i.Call(ctx, tab, "Runtime.evaluate", map[string]any{
		"expression": fmt.Sprintf("(%s)(%g,%g)", selectHit, in.X, in.Y),
	})
	if err != nil {
		return nil
	}
	var result struct {
		Result struct {
			ObjectID string `json:"objectId"`
		} `json:"result"`
	}
	if json.Unmarshal(raw, &result) != nil || result.Result.ObjectID == "" {
		return nil
	}
	s.tab, s.object = tab, result.Result.ObjectID
	raw, err = i.Call(ctx, tab, "Runtime.callFunctionOn", map[string]any{
		"objectId": s.object, "functionDeclaration": "function(){return this.menu}", "returnByValue": true,
	})
	var menu struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
	}
	if err != nil || json.Unmarshal(raw, &menu) != nil || len(menu.Result.Value) == 0 {
		s.clear(ctx, i)
		return nil
	}
	s.swallowRelease = true
	return menu.Result.Value
}

func (s *panelSelect) choose(ctx context.Context, i *Instance, tab string, index int) error {
	defer s.clear(ctx, i)
	if s.object == "" || s.tab != tab {
		return errors.New("select is no longer open")
	}
	_, err := i.panelCall(ctx, tab, "Runtime.callFunctionOn", map[string]any{
		"objectId": s.object, "arguments": []map[string]any{{"value": index}}, "userGesture": true,
		"functionDeclaration": `function(index){
   const el=this.element, o=el.options[index];
   if(!el.isConnected || el.matches(':disabled') || !o || o.disabled ||
     (o.parentElement.tagName==='OPTGROUP' && o.parentElement.disabled)) return;
   el.focus(); el.selectedIndex=index;
   const E=el.ownerDocument.defaultView.Event;
   el.dispatchEvent(new E('input',{bubbles:true})); el.dispatchEvent(new E('change',{bubbles:true}));
  }`,
	})
	return err
}
