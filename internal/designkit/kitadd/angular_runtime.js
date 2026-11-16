// The browser side of an Angular kit: one Angular application (zoneless) starts when the script loads, and every
// component or host-like directive of the package gets a React component of the same name. The React component keeps
// an element for Angular to mount into (createComponent on a host element made here), sets Angular inputs with
// setInput, turns outputs into onXxx props and projects React children through <ng-content>.
import '@angular/compiler';
import * as core from '@angular/core';
import { createApplication } from '@angular/platform-browser';
/*IMPORTS*/
var MODS = [/*MODS*/];
var DATA = /*DATA*/;
var NS = /*NS*/;
var React = window.React, ReactDOM = window.ReactDOM;
var zoneless = core.provideZonelessChangeDetection || core.provideExperimentalZonelessChangeDetection;
var DYN = {};
var providers = [zoneless ? zoneless() : []];
/*PROVIDERS*/
// Some libraries measure the page (a scrollbar probe on document.body) while they start: wait for a body.
var bodyReady = new Promise(function (res) { if (document.body) res(); else document.addEventListener('DOMContentLoaded', function () { res(); }); });
var ready = bodyReady.then(function () { return createApplication({ providers: providers }); });
ready.catch(function (e) { console.error('[kit ' + NS + '] Angular did not start', e); });
var ns = window[NS] || (window[NS] = {});
ns.ready = ready;

function cap(s) { return s.replace(/(^|[-_.:])(\w)/g, function (_, __, c) { return c.toUpperCase(); }); }
function hostClass(c, Dir) {
  var ins = c.inputs.map(function (i) { return i.name; });
  var outs = c.outputs.map(function (o) { return o.name; });
  var H = class {};
  core.Component({ selector: 'hk-host', standalone: true, template: '<ng-content></ng-content>',
    hostDirectives: [{ directive: Dir, inputs: ins, outputs: outs }] })(H);
  return H;
}
var hosts = {};
function classOf(c) {
  var C = MODS[c.mod][c.cls];
  if (c.directive) return hosts[c.id] || (hosts[c.id] = hostClass(c, C));
  return C;
}
function applyHost(el, className, style) {
  if (className !== undefined) {
    (el.__hkc || []).forEach(function (n) { el.classList.remove(n); });
    el.__hkc = String(className || '').split(/\s+/).filter(Boolean);
    el.__hkc.forEach(function (n) { el.classList.add(n); });
  }
  if (style) for (var k in style) { try { el.style[k] = style[k]; } catch (e) {} }
}

var ALL = null;
function allClasses() {
  if (ALL) return ALL;
  var seen = [];
  MODS.forEach(function (m) { Object.keys(m).forEach(function (k) {
    var v = m[k];
    if (typeof v === 'function' && (/Module$/.test(k) || v.ɵmod || (v.ɵcmp && v.ɵcmp.standalone) || (v.ɵdir && v.ɵdir.standalone)) && seen.indexOf(v) < 0) seen.push(v);
  }); });
  // NgModules are flattened here: a module in a JIT standalone import list does not always bring its exports along
  // (PrimeNG's TableModule, SelectModule …), so the exported directives and components are added by themselves.
  var scopes = core.ɵtransitiveScopesFor;
  if (typeof scopes === 'function') seen.slice().forEach(function (m) {
    if (!m.ɵmod) return;
    try {
      var ex = scopes(m).exported;
      [ex.directives, ex.pipes].forEach(function (set) { set.forEach(function (x) { if (seen.indexOf(x) < 0) seen.push(x); }); });
    } catch (e) {}
  });
  return (ALL = seen);
}
var DEFS = null;
function own(c, k) { return Object.prototype.hasOwnProperty.call(c, k) ? c[k] : null; }
function allDefs() {
  if (DEFS) return DEFS;
  var dirs = [], pipes = [];
  allClasses().forEach(function (c) {
    var d = own(c, 'ɵcmp') || own(c, 'ɵdir'), p = own(c, 'ɵpipe');
    if (d && dirs.indexOf(d) < 0) dirs.push(d);
    if (p && pipes.indexOf(p) < 0) pipes.push(p);
  });
  return (DEFS = { dirs: dirs, pipes: pipes });
}
function findCls(name) {
  for (var i = 0; i < MODS.length; i++) if (typeof MODS[i][name] === 'function') return MODS[i][name];
  return null;
}
var VOID = /^(area|base|br|col|embed|hr|img|input|link|meta|source|track|wbr)$/;
function isText(n) { return typeof n === 'string' || typeof n === 'number'; }
function esc(t) { return String(t).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/@/g, '&#64;').replace(/\{/g, '&#123;').replace(/\}/g, '&#125;'); }
function attrEsc(t) { return esc(t).replace(/"/g, '&quot;'); }
function hasKit(n) {
  if (n == null || typeof n !== 'object') return false;
  if (Array.isArray(n)) return n.some(hasKit);
  if (n.type && n.type.__hk) return true;
  return n.props ? hasKit(n.props.children) : false;
}
function tplOf(n, st) {
  if (n == null || n === false || n === true) return '';
  if (Array.isArray(n)) return n.map(function (x) { return tplOf(x, st); }).join('');
  if (typeof n === 'string' || typeof n === 'number') return esc(n);
  var t = n.type, p = n.props || {};
  if (t && t.__hk) return openTpl(t.__hk, p, st);
  if (t === React.Fragment) return tplOf(p.children, st);
  if (typeof t === 'string') return '<' + t + (p.className ? ' class="' + attrEsc(p.className) + '"' : '') + (VOID.test(t) ? '/>' : '>' + tplOf(p.children, st) + '</' + t + '>');
  return '';
}
function openTpl(c, p, st) {
  var a = '<' + c.tag;
  (c.attrs || []).forEach(function (x) { a += ' ' + x[0] + '="' + attrEsc(x[1]) + '"'; });
  c.inputs.forEach(function (i) {
    var v = p[i.name];
    if (v === undefined) return;
    if (/^[A-Za-z_$][\w$]*$/.test(i.name)) { st.vals.push(v); a += ' [' + i.name + ']="v[' + (st.vals.length - 1) + ']"'; }
    else if (typeof v === 'string') a += ' ' + i.name + '="' + attrEsc(v) + '"';
  });
  var known = {};
  c.inputs.forEach(function (i) { known[i.name] = 1; });
  Object.keys(p).forEach(function (k) {
    var v = p[k];
    if (known[k] || k === 'children' || k === 'className' || k === 'style' || k === 'open' || k === 'key' || k === 'ref' || /^on[A-Z]/.test(k)) return;
    if ((typeof v === 'string' || typeof v === 'number') && /^[A-Za-z][\w-]*$/.test(k)) a += ' ' + k + '="' + attrEsc(v) + '"';
  });
  c.outputs.forEach(function (o) {
    var f = p['on' + cap(o.name)];
    if (typeof f === 'function' && /^[A-Za-z_$][\w$]*$/.test(o.name)) { st.fns.push(f); a += ' (' + o.name + ')="f[' + (st.fns.length - 1) + ']($event)"'; }
  });
  if (p.className) a += ' class="' + attrEsc(p.className) + '"';
  if (c.ov && c.ov.attrs) a += ' ' + c.ov.attrs;
  if (VOID.test(c.tag)) return a + '/>';
  return a + '>' + tplOf(p.children, st) + '</' + c.tag + '>';
}
function tplSource(c, p, st) {
  var ov = c.ov;
  if (ov && ov.dyn) return DYN[c.id](p, st);
  if (ov && ov.tpl) return ov.tpl;
  var root = openTpl(c, p, st);
  return ov ? (ov.before || '') + root + (ov.after || '') : root;
}
function tplClass(src, c) {
  var T = class { constructor() { this.v = []; this.f = []; this.d = (c.ov && c.ov.data) || []; this.ca = function (n) { return n.children || []; }; } };
  core.Component({ selector: 'hk-tpl', standalone: true, imports: allClasses(), template: src })(T);
  // The JIT does not always resolve every import of the list into the template's directive table (long lists stop
  // short), so the table is set from the classes' own definitions.
  try {
    var def = T.ɵcmp, defs = allDefs();
    def.directiveDefs = function () { return defs.dirs; };
    def.pipeDefs = function () { return defs.pipes; };
  } catch (e) { console.error('[kit] defs', e); }
  if (c.ov && c.ov.query) {
    var q = c.ov.query, read = q.cls ? findCls(q.cls) : null;
    core.ViewChild(q.name, read ? { read: read } : {})(T.prototype, 'trg');
  }
  return T;
}

function wrap(c) {
  var W = React.forwardRef(function Kit(props, fref) {
    var mount = React.useRef(null), pr = React.useRef(props);
    pr.current = props;
    var st = React.useState(null), proj = st[0], setProj = st[1];
    var kitKids = hasKit(props.children) || !!c.ov || !!c.tpl;
    React.useLayoutEffect(function () {
      var dead = false, S = { ref: null, el: null, kind: null, src: null, text: null, last: {}, opened: false }, container = null, theApp = null;
      function destroy() {
        if (S.ref) { try { S.ref.destroy(); } catch (e) {} }
        if (S.el) S.el.remove();
        S.ref = S.el = S.kind = S.src = null; S.last = {}; S.opened = false;
      }
      function mountClassic() {
        var host = document.createElement(c.tag);
        host.setAttribute('data-hk', c.id);
        (c.attrs || []).forEach(function (a) { host.setAttribute(a[0], a[1]); });
        var kids = pr.current.children;
        if (isText(kids)) { S.text = document.createTextNode(String(kids)); container = S.text; }
        else { S.text = null; container = document.createElement('div'); container.style.display = 'contents'; }
        mount.current.appendChild(host);
        var nodes = (c.slots || ['*']).map(function (s, i) { return i === 0 || s === '*' ? [container] : []; });
        var ref = core.createComponent(classOf(c), { hostElement: host, environmentInjector: theApp.injector, projectableNodes: nodes });
        theApp.attachView(ref.hostView);
        S.ref = ref; S.el = host; S.kind = 'c';
        c.outputs.forEach(function (o) {
          var out = ref.instance[o.prop];
          if (out && out.subscribe) out.subscribe(function (v) {
            var f = pr.current['on' + cap(o.name)];
            if (typeof f === 'function') f(v);
          });
        });
        var inst = ref.instance;
        if (typeof inst.registerOnChange === 'function' && typeof inst.writeValue === 'function') {
          inst.registerOnChange(function (v) { var f = pr.current.onChange; if (typeof f === 'function') f(v); });
          if (typeof inst.registerOnTouched === 'function') inst.registerOnTouched(function () { var f = pr.current.onBlur; if (typeof f === 'function') f(); });
        }
        setProj(S.text ? null : container);
      }
      function mountTpl(src, t) {
        var el = document.createElement('hk-tpl');
        el.style.display = 'contents';
        el.setAttribute('data-hk', c.id);
        mount.current.appendChild(el);
        var ref = core.createComponent(tplClass(src, c), { hostElement: el, environmentInjector: theApp.injector });
        ref.instance.v = t.vals; ref.instance.f = t.fns;
        theApp.attachView(ref.hostView);
        S.ref = ref; S.el = el; S.kind = 't'; S.src = src; S.opened = false;
      }
      function flushTpl(p) {
        var t = { vals: [], fns: [] }, src = tplSource(c, p, t);
        if (S.kind !== 't' || S.src !== src) { destroy(); mountTpl(src, t); }
        else { S.ref.instance.v = t.vals; S.ref.instance.f = t.fns; }
        S.ref.changeDetectorRef.detectChanges();
        var ov = c.ov;
        if (ov && ov.open) {
          var want = !!p.open, target = S.ref.instance.trg;
          if (want && !S.opened && target && typeof target[ov.open] === 'function') {
            try { target[ov.open](); S.opened = true; } catch (e) { console.error('[kit] open', e); }
          } else if (!want && S.opened) {
            try { if (target.close) target.close(); else if (target.closeMenu) target.closeMenu(); } catch (e) {}
            S.opened = false;
          }
        }
      }
      function flushClassic(p) {
        var kidsText = isText(p.children);
        if (S.kind !== 'c' || (kidsText !== !!S.text && p.children != null)) { destroy(); mountClassic(); }
        else if (S.text && String(p.children) !== S.text.data) S.text.data = String(p.children);
        var changed = false, ref = S.ref;
        c.inputs.forEach(function (i) {
          if (p[i.name] !== undefined && p[i.name] !== S.last[i.name]) {
            try { ref.setInput(i.name, p[i.name]); S.last[i.name] = p[i.name]; changed = true; } catch (e) {}
          }
        });
        if (p.value !== undefined && p.value !== S.last.__value && typeof ref.instance.writeValue === 'function') {
          try { ref.instance.writeValue(p.value); S.last.__value = p.value; changed = true; } catch (e) {}
        }
        if (changed) ref.changeDetectorRef.detectChanges();
        applyHost(S.el, p.className, p.style);
      }
      function flush() {
        if (!theApp || dead) return;
        var p = pr.current;
        try { if (hasKit(p.children) || c.ov || c.tpl) flushTpl(p); else flushClassic(p); }
        catch (e) { console.error('[kit ' + c.id + ']', e); }
      }
      ready.then(function (app) { if (dead) return; theApp = app; flush(); });
      mount.current.__hkFlush = flush;
      return function () { dead = true; destroy(); };
    }, []);
    React.useLayoutEffect(function () { if (mount.current && mount.current.__hkFlush) mount.current.__hkFlush(); });
    React.useImperativeHandle(fref, function () { return mount.current && mount.current.firstChild; });
    return React.createElement(React.Fragment, null,
      React.createElement('span', { ref: mount, style: { display: 'contents' } }),
      proj && !kitKids ? ReactDOM.createPortal(props.children, proj) : null);
  });
  W.displayName = c.id;
  W.__hk = c;
  return W;
}
DATA.forEach(function (c) { ns[c.id] = wrap(c); });
/*COMPOSE*/
/*ROOT*/
