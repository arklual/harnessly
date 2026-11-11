package docs

import (
	"bytes"
	"net/http"
	"strings"
)

// frameScript runs inside the document the viewer frames and speaks the viewer's side of the frame protocol
// (TDocFrameMessage, TDocParentMessage in web/app/types/doc-threads.ts): the selection a comment is anchored to, the
// places of the thread marks, and the viewer's requests to show a thread, a draft or a passage. Editing in place is not
// offered by this hub, so the edit messages are neither sent nor read.
//
// The document is served with a sandbox without allow-same-origin: the script has no cookies and no access to the hub.
const frameScript = `<script data-hub-doc-frame>(function(){
var parentWin=window.parent;if(!parentWin||parentWin===window)return;
var me=document.currentScript;
var threads=[],draft=null,focusId=null,raf=0;
function post(m){try{parentWin.postMessage(m,'*')}catch(e){}}
function bodyText(){var r=document.createRange();r.setStart(document.body,0);r.setEndBefore(me);return r.toString()}
function offsetOf(node,off){var r=document.createRange();r.setStart(document.body,0);r.setEnd(node,off);return r.toString().length}
function rangeAt(from,to){
  var w=document.createTreeWalker(document.body,NodeFilter.SHOW_TEXT),n,pos=0,sn=null,so=0,en=null,eo=0;
  while((n=w.nextNode())){
    if(me&&(me.compareDocumentPosition(n)&Node.DOCUMENT_POSITION_FOLLOWING||me.contains(n)))break;
    var len=n.data.length;
    if(!sn&&from<=pos+len&&from>=pos&&(from<pos+len||to<=pos+len)){sn=n;so=from-pos}
    if(sn&&to<=pos+len){en=n;eo=to-pos;break}
    pos+=len;
  }
  if(!sn||!en)return null;
  var r=document.createRange();r.setStart(sn,so);r.setEnd(en,eo);return r;
}
function locate(a){
  if(!a||!a.quote)return null;
  var t=bodyText(),q=a.quote,i=-1;
  if(a.prefix||a.suffix){i=t.indexOf((a.prefix||'')+q+(a.suffix||''));if(i>=0)i+=(a.prefix||'').length}
  if(i<0)i=t.indexOf(q);
  if(i<0)return null;
  return rangeAt(i,i+q.length);
}
function rectOf(r){var b=r.getBoundingClientRect();return{top:b.top,left:b.left,width:b.width,height:b.height}}
function headingBefore(node){
  var el=node.nodeType===1?node:node.parentElement,best=null;
  var hs=document.querySelectorAll('h1,h2,h3,h4,h5,h6');
  for(var i=0;i<hs.length;i++){if(hs[i].compareDocumentPosition(el)&Node.DOCUMENT_POSITION_FOLLOWING||hs[i].contains(el))best=hs[i];else break}
  return best;
}
function anchorOf(sel){
  var r=sel.getRangeAt(0),q=r.toString().replace(/\s+/g,' ').trim();
  if(!q)return null;
  var raw=r.toString(),start=offsetOf(r.startContainer,r.startOffset),t=bodyText();
  var lead=raw.length-raw.replace(/^\s+/,'').length;
  start+=lead;
  var quote=raw.trim().slice(0,500),end=start+quote.length;
  var a={quote:quote,prefix:t.slice(Math.max(0,start-40),start),suffix:t.slice(end,end+40)};
  var h=headingBefore(r.startContainer);
  if(h){a.headingText=(h.textContent||'').trim().slice(0,200);if(h.id)a.headingId=h.id}
  return a;
}
function selectionChanged(){
  var sel=window.getSelection();
  if(!sel||sel.isCollapsed||!sel.rangeCount){post({type:'doc:selection-cleared'});return}
  var a=anchorOf(sel);
  if(!a){post({type:'doc:selection-cleared'});return}
  var r=sel.getRangeAt(0),rects=r.getClientRects(),last=rects.length?rects[rects.length-1]:r.getBoundingClientRect();
  post({type:'doc:selection',anchor:a,rect:rectOf(r),end:{top:last.top,left:last.left,width:last.width,height:last.height}});
}
function paint(){
  if(!window.CSS||!CSS.highlights||typeof Highlight==='undefined')return;
  var open=[],shut=[],dr=[],fo=[];
  threads.forEach(function(t){var r=locate(t.anchor);if(!r)return;(t.id===focusId?fo:(t.status==='resolved'?shut:open)).push(r)});
  if(draft){var d=locate(draft);if(d)dr.push(d)}
  CSS.highlights.set('hub-doc-open',new Highlight(...open));
  CSS.highlights.set('hub-doc-resolved',new Highlight(...shut));
  CSS.highlights.set('hub-doc-focus',new Highlight(...fo));
  CSS.highlights.set('hub-doc-draft',new Highlight(...dr));
}
function sendAnchors(){
  raf=0;paint();
  var pos={};
  threads.forEach(function(t){var r=locate(t.anchor);pos[t.id]=r?rectOf(r):null});
  var col=document.body.getBoundingClientRect().right;
  var kids=document.body.children;for(var i=0;i<kids.length;i++){var b=kids[i].getBoundingClientRect();if(b.width>0&&b.right>col*0.5&&b.right<col)col=b.right}
  post({type:'doc:anchors',positions:pos,columnRight:Math.min(col,window.innerWidth)});
}
function schedule(){if(!raf)raf=requestAnimationFrame(sendAnchors)}
var css=document.createElement('style');
css.textContent='::highlight(hub-doc-open){background:rgba(250,204,21,.35)}::highlight(hub-doc-resolved){background:rgba(148,163,184,.25)}::highlight(hub-doc-focus){background:rgba(250,204,21,.6)}::highlight(hub-doc-draft){background:rgba(96,165,250,.4)}';
document.head.appendChild(css);
document.addEventListener('mouseup',function(){setTimeout(selectionChanged,0)});
document.addEventListener('keyup',function(e){if(e.key==='Shift'||e.key.indexOf('Arrow')===0)selectionChanged()});
document.addEventListener('mousedown',function(){post({type:'doc:dismiss'})});
document.addEventListener('keydown',function(e){
  if(e.key==='Escape')post({type:'doc:dismiss'});
  if((e.ctrlKey||e.metaKey)&&e.altKey&&(e.key==='m'||e.key==='M'||e.code==='KeyM')){e.preventDefault();post({type:'doc:comment-key'})}
});
document.addEventListener('click',function(e){
  var sel=window.getSelection();if(sel&&!sel.isCollapsed)return;
  for(var i=0;i<threads.length;i++){var r=locate(threads[i].anchor);if(!r)continue;
    var rs=r.getClientRects();for(var j=0;j<rs.length;j++){var b=rs[j];
      if(e.clientX>=b.left&&e.clientX<=b.right&&e.clientY>=b.top&&e.clientY<=b.bottom){post({type:'doc:open',threadId:threads[i].id});return}}}
});
window.addEventListener('scroll',schedule,{passive:true});
window.addEventListener('resize',schedule);
window.addEventListener('message',function(e){
  if(e.source!==parentWin)return;
  var m=e.data;if(!m||typeof m.type!=='string')return;
  if(m.type==='doc:threads'){threads=Array.isArray(m.threads)?m.threads:[];schedule()}
  else if(m.type==='doc:focus'){focusId=m.threadId||null;var t=threads.filter(function(x){return x.id===focusId})[0];
    if(t){var r=locate(t.anchor);if(r){var el=r.startContainer.parentElement;if(el)el.scrollIntoView({block:'center',behavior:'smooth'})}}schedule()}
  else if(m.type==='doc:draft'){draft=m.anchor;schedule()}
  else if(m.type==='doc:draft-clear'){draft=null;schedule()}
  else if(m.type==='doc:find'&&typeof m.text==='string'){
    var t2=bodyText(),i=t2.indexOf(m.text.slice(0,200));if(i>=0){var r2=rangeAt(i,i+Math.min(m.text.length,200));if(r2){var el2=r2.startContainer.parentElement;if(el2)el2.scrollIntoView({block:'center',behavior:'smooth'});draft={quote:m.text.slice(0,200),prefix:'',suffix:''};focusId=null;paint();setTimeout(function(){draft=null;paint()},1800)}}}
});
if(document.readyState==='complete')schedule();else window.addEventListener('load',schedule);
})();</script>`

// withFrame puts the frame script at the end of the document's body (or of the document when it has none).
func withFrame(page []byte) []byte {
	if i := bytes.LastIndex(bytes.ToLower(page), []byte("</body>")); i >= 0 {
		out := make([]byte, 0, len(page)+len(frameScript))
		out = append(out, page[:i]...)
		out = append(out, frameScript...)
		return append(out, page[i:]...)
	}
	return append(page, frameScript...)
}

// themeOf reads the viewer's theme request.
func themeOf(v string) string {
	switch strings.ToLower(v) {
	case "light", "dark":
		return strings.ToLower(v)
	}
	return ""
}

// frameable sets the terms of a document page the viewer puts in a frame: a sandbox, and the right to be framed by the
// origin it is served from (the hub's, or the one of a gateway that proxies it under its own address). The hub's blanket
// X-Frame-Options: DENY would refuse even that, and the browser shows «refused to connect» in the frame.
func frameable(h http.Header) {
	h.Del("X-Frame-Options")
	h.Set("Content-Security-Policy", "sandbox allow-scripts allow-popups; frame-ancestors 'self'")
}
