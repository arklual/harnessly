// SPDX-License-Identifier: Apache-2.0
// Harnessly's board engine: an independent implementation of the *.dc.html format, written from its observed behaviour.
//
// A board is one HTML file: a template inside <x-dc>, logic in <script type="text/x-dc" data-dc-script>, tweaks in that
// script's data-props. The template is parsed once by the browser's own HTML parser, numbered, turned into a tree of
// small builder functions, and drawn with React (the board's kits are React components, so React is part of the format).
(function () {
  'use strict';

  if (window.__dcRegistry && window.__dcBoot) return; // the file was included twice

  var REACT_URL = 'https://unpkg.com/react@18.3.1/umd/react.production.min.js';
  var REACT_DOM_URL = 'https://unpkg.com/react-dom@18.3.1/umd/react-dom.production.min.js';
  var BABEL_URL = 'https://unpkg.com/@babel/standalone@7.29.0/babel.min.js';
  var INTEGRITY = {};
  INTEGRITY[REACT_URL] = 'sha384-DGyLxAyjq0f9SPpVevD6IgztCFlnMF6oW/XQGmfe+IsZ8TqEiDrcHkMLKI6fiB/Z';
  INTEGRITY[REACT_DOM_URL] = 'sha384-gTGxhz21lVGYNMcdJOyq01Edg0jhn/c22nsx0kyqP0TxaV5WVdsSH1fSDUf5YJj1';
  INTEGRITY[BABEL_URL] = 'sha384-m08KidiNqLdpJqLq95G/LEi8Qvjl/xUYll3QILypMoQ65QorJ9Lvtp2RXYGBFj1y';

  var LOG = '[dc-runtime]';

  // ---------------------------------------------------------------------------------------------------------------
  // Reading a board file.
  // internal/design/deckpatch_chrome_test.go cuts parseDcText, the encode section and the head of compileTemplate out
  // of this file by their exact first lines and section comments, so those stay as they are.
  // ---------------------------------------------------------------------------------------------------------------

  function parseDcText(src) {
    var open = /<x-dc(?:\s[^>]*)?>/.exec(src);
    var close = src.lastIndexOf('</x-dc>');
    if (!open || close < open.index) return null;
    var from = open.index + open[0].length;
    var script = new DOMParser().parseFromString(src, 'text/html').querySelector('script[data-dc-script]');
    return {
      template: src.slice(from, Math.max(from, close)),
      js: script ? script.textContent : '',
      props: script ? script.getAttribute('data-props') : null
    };
  }

  function dcNameFromPath(path) {
    var file = String(path).split(/[?#]/)[0].split('/').pop() || '';
    try { file = decodeURIComponent(file); } catch (e) { /* the name as it is */ }
    return file.replace(/\.dc\.html$/i, '').replace(/\.html?$/i, '') || 'index';
  }

  // src/encode.ts: the template's source is renamed before parsing, so the HTML parser keeps what it would otherwise
  // lose (the case of attribute names) or move (table and select tags, which it foster-parents around the control tags).

  var RAW_TAGS = /(<\/?)(select|table|tbody|thead|tfoot|tr|td|th|caption)(\s|>)/gi;
  var SELF_CLOSED_IMPORT = /<(x-import|dc-import)((?:[^>"']|"[^"]*"|'[^']*')*)\/>/gi;
  var CAMEL_ATTR = /(\s)([a-z]+[A-Z][A-Za-z0-9]*)(\s*=)/g;

  function encodeCase(s) {
    return s
      .replace(SELF_CLOSED_IMPORT, '<$1$2></$1>')
      .replace(/<helmet(\s|>)/gi, '<sc-helmet$1')
      .replace(/<\/helmet\s*>/gi, '</sc-helmet>')
      .replace(CAMEL_ATTR, function (all, before, name, after) {
        return before + 'sc-camel-' + name.replace(/[A-Z]/g, function (c) { return '-' + c.toLowerCase(); }) + after;
      })
      .replace(RAW_TAGS, function (all, bracket, name, after) {
        return bracket + 'sc-raw-' + name.toLowerCase() + after;
      });
  }

  // src/compile.ts: the numbering is a preorder over the template's content. querySelectorAll does not enter the
  // content of a nested <template>, which is what the runtime's own parse (deckpatch.go) expects.

  function compileTemplate(html, host) {
    var tpl = document.createElement('template');
    tpl.innerHTML = encodeCase(html);
    var numbered = tpl.content.querySelectorAll('*');
    for (var i = 0; i < numbered.length; i += 1) numbered[i].setAttribute('data-dc-tpl', String(i));
    const builders = buildChildren(tpl.content, host);
    return { element: tpl, build: builders };
  }

  // ---------------------------------------------------------------------------------------------------------------
  // Expressions: {{ path }}, literals and ! in front of either. Anything else never resolves.
  // ---------------------------------------------------------------------------------------------------------------

  var PATH_HEAD = /^[A-Za-z_$][\w$]*/;
  var PATH_STEP = /^(?:\.([\w$]+)|\[\s*(\d+)\s*\]|\[\s*"([^"]*)"\s*\]|\[\s*'([^']*)'\s*\])/;

  function parsePath(text) {
    var head = PATH_HEAD.exec(text);
    if (!head) return null;
    var keys = [head[0]];
    var rest = text.slice(head[0].length);
    while (rest) {
      var step = PATH_STEP.exec(rest);
      if (!step) return null;
      keys.push(step[1] !== undefined ? step[1] : step[2] !== undefined ? step[2] : step[3] !== undefined ? step[3] : step[4]);
      rest = rest.slice(step[0].length);
    }
    return keys;
  }

  function never() { return undefined; }

  function constant(value) { return function () { return value; }; }

  function parseExpression(source) {
    var text = source.trim();
    if (text.charAt(0) === '!') {
      var inner = parseExpression(text.slice(1));
      return function (scope) { return !inner(scope); };
    }
    if (/^-?\d+(?:\.\d+)?$/.test(text)) return constant(Number(text));
    if (text === 'true') return constant(true);
    if (text === 'false') return constant(false);
    if (text === 'null') return constant(null);
    var quoted = /^"([^"]*)"$/.exec(text) || /^'([^']*)'$/.exec(text);
    if (quoted) return constant(quoted[1]);
    if (text === 'undefined' || text === '') return never;
    var keys = parsePath(text);
    if (!keys) return never;
    return function (scope) {
      var value = scope[keys[0]];
      for (var i = 1; i < keys.length && value !== undefined && value !== null; i += 1) value = value[keys[i]];
      return i < keys.length ? undefined : value;
    };
  }

  var WHOLE_EXPRESSION = /^\{\{([\s\S]*)\}\}$/;
  var ONE_EXPRESSION = /\{\{([\s\S]*?)\}\}/g;

  // An attribute's value: an expression that is the whole value keeps its type, anything mixed becomes a string.
  function compileValue(text) {
    var whole = WHOLE_EXPRESSION.exec(text.trim());
    if (whole) return parseExpression(whole[1]);
    if (text.indexOf('{{') < 0) return constant(text);
    var parts = splitExpressions(text);
    return function (scope) {
      var out = '';
      for (var i = 0; i < parts.length; i += 1) {
        var part = parts[i];
        if (typeof part === 'string') { out += part; continue; }
        var value = part.read(scope);
        if (value !== undefined && value !== null) out += String(value);
      }
      return out;
    };
  }

  function splitExpressions(text) {
    var parts = [];
    var at = 0;
    ONE_EXPRESSION.lastIndex = 0;
    for (var m = ONE_EXPRESSION.exec(text); m; m = ONE_EXPRESSION.exec(text)) {
      if (m.index > at) parts.push(text.slice(at, m.index));
      parts.push({ source: m[1], read: parseExpression(m[1]) });
      at = m.index + m[0].length;
    }
    if (at < text.length) parts.push(text.slice(at));
    return parts;
  }

  // ---------------------------------------------------------------------------------------------------------------
  // Attributes.
  // ---------------------------------------------------------------------------------------------------------------

  // React's event names, for an attribute that reached the template in lower case (a template read from the DOM).
  var EVENTS = {};
  ('Abort AnimationEnd AnimationIteration AnimationStart AuxClick BeforeInput Blur CanPlay CanPlayThrough Change Click ' +
    'Close CompositionEnd CompositionStart CompositionUpdate ContextMenu Copy Cut DoubleClick Drag DragEnd DragEnter ' +
    'DragExit DragLeave DragOver DragStart Drop DurationChange Emptied Encrypted Ended Error Focus GotPointerCapture ' +
    'Input Invalid KeyDown KeyPress KeyUp Load LoadedData LoadedMetadata LoadStart LostPointerCapture MouseDown ' +
    'MouseEnter MouseLeave MouseMove MouseOut MouseOver MouseUp Paste Pause Play Playing PointerCancel PointerDown ' +
    'PointerEnter PointerLeave PointerMove PointerOut PointerOver PointerUp Progress RateChange Reset Resize Scroll ' +
    'Seeked Seeking Select Stalled Submit Suspend TimeUpdate Toggle TouchCancel TouchEnd TouchMove TouchStart ' +
    'TransitionEnd VolumeChange Waiting Wheel').split(' ').forEach(function (name) {
    EVENTS['on' + name.toLowerCase()] = 'on' + name;
  });
  EVENTS.ondblclick = 'onDoubleClick';

  // A bare attribute of these names means on, whatever React would make of an empty string.
  var BOOLEAN_ATTRS = { async: 1, checked: 1, controls: 1, default: 1, defer: 1, disabled: 1, hidden: 1, loop: 1, multiple: 1, muted: 1,
    open: 1, required: 1, reversed: 1, scoped: 1, seamless: 1, selected: 1 };

  var DROPPED_ON_ELEMENTS = { 'data-dc-tpl': 1, 'hint-size': 1, 'hint-placeholder-val': 1, 'hint-placeholder-count': 1 };

  function kebabToCamel(name) {
    return name.replace(/-([a-z0-9])/g, function (all, c) { return c.toUpperCase(); });
  }

  // A name as the board's author wrote it: encodeCase's sc-camel-foo-bar is fooBar.
  function authoredName(name) {
    return name.indexOf('sc-camel-') === 0 ? kebabToCamel(name.slice(9)) : name;
  }

  // A style attribute's declarations: split at the semicolons that are not inside quotes or parentheses.
  function parseDeclarations(text) {
    var out = [];
    var start = 0;
    var depth = 0;
    var quote = '';
    function take(end) {
      var declaration = text.slice(start, end);
      var colon = declaration.indexOf(':');
      if (colon <= 0) return;
      var name = declaration.slice(0, colon).trim();
      var value = declaration.slice(colon + 1).replace(/!\s*important\s*$/i, '').trim();
      if (name && value) out.push({ name: name.indexOf('--') === 0 ? name : name.toLowerCase(), value: value });
    }
    for (var i = 0; i < text.length; i += 1) {
      var c = text.charAt(i);
      if (quote) { if (c === quote) quote = ''; continue; }
      if (c === '"' || c === "'") quote = c;
      else if (c === '(') depth += 1;
      else if (c === ')') depth = Math.max(0, depth - 1);
      else if (c === ';' && depth === 0) { take(i); start = i + 1; }
    }
    take(text.length);
    return out;
  }

  var styleCache = new Map();
  var styleProbe = null;

  // The browser decides what is a valid declaration; a property written twice keeps its first place and its last value.
  function cssToObject(input) {
    var text = String(input);
    if (styleCache.has(text)) return styleCache.get(text);
    styleProbe = styleProbe || document.createElement('div').style;
    var kept = new Map();
    parseDeclarations(text).forEach(function (d) {
      styleProbe.cssText = '';
      styleProbe.setProperty(d.name, d.value);
      if (!styleProbe.length) return;
      kept.set(d.name, d.value);
    });
    var style = {};
    kept.forEach(function (value, name) {
      if (name.indexOf('--') === 0) style[name] = value;
      else style[name.replace(/^-ms-/, 'ms-').replace(/-([a-z])/g, function (all, c) { return c.toUpperCase(); })] = value;
    });
    if (styleCache.size > 5000) styleCache.clear();
    styleCache.set(text, style);
    return style;
  }

  // style-hover="…", style-focus="…" and the like: a generated class with one rule, per distinct pseudo and source text.
  // The text is taken as written: a {{ }} in it makes that declaration invalid, so it has no effect.
  var PSEUDO_ELEMENTS = { before: 1, after: 1, 'first-line': 1, 'first-letter': 1, placeholder: 1, selection: 1, marker: 1 };
  var pseudoSheet = null;
  var pseudoClasses = new Map();

  function pseudoClass(pseudo, text) {
    var key = pseudo + '\n' + text;
    if (pseudoClasses.has(key)) return pseudoClasses.get(key);
    if (!pseudoSheet) {
      pseudoSheet = document.createElement('style');
      document.head.appendChild(pseudoSheet);
    }
    var name = 'scp' + pseudoClasses.size.toString(36);
    pseudoClasses.set(key, name);
    var element = PSEUDO_ELEMENTS[pseudo];
    var body = parseDeclarations(text).map(function (d) {
      return d.name + ':' + d.value + (element ? '' : ' !important');
    }).join(';');
    try {
      pseudoSheet.sheet.insertRule('.' + name + (element ? '::' : ':') + pseudo + '{' + body + '}', pseudoSheet.sheet.cssRules.length);
    } catch (e) { /* a pseudo this browser does not know */ }
    return name;
  }

  function styleObject(value) {
    if (value === undefined || value === null || value === false) return undefined;
    return typeof value === 'object' ? value : cssToObject(value);
  }

  // The props of a plain element: its number first and its style last, as a board's DOM reads.
  function compileElementProps(el) {
    var readers = [];
    var style = null;
    var generated = [];
    var names = el.getAttributeNames();
    for (var i = 0; i < names.length; i += 1) {
      var raw = names[i];
      if (DROPPED_ON_ELEMENTS[raw]) continue;
      if (raw.indexOf('style-') === 0) { generated.push(pseudoClass(raw.slice(6), el.getAttribute(raw))); continue; }
      var name = authoredName(raw);
      var read = BOOLEAN_ATTRS[raw] && el.getAttribute(raw) === '' ? constant(true) : compileValue(el.getAttribute(raw));
      if (name === 'style') { style = read; continue; }
      if (name === 'class') name = 'className';
      if (EVENTS[name]) name = EVENTS[name];
      readers.push({ name: name, read: read, event: /^on[A-Z]/.test(name), ref: name === 'ref' });
    }
    var tplNumber = el.getAttribute('data-dc-tpl');
    var extraClasses = generated.join(' ');
    return function (scope) {
      var props = { 'data-dc-tpl': tplNumber };
      for (var k = 0; k < readers.length; k += 1) {
        var r = readers[k];
        var value = r.read(scope);
        if (value === undefined) continue;
        if ((r.event || r.ref) && typeof value !== 'function' && (r.event || typeof value !== 'object' || value === null)) continue;
        props[r.name] = value;
      }
      if (extraClasses) props.className = props.className ? props.className + ' ' + extraClasses : extraClasses;
      if (style) {
        var object = styleObject(style(scope));
        if (object) props.style = object;
      }
      return props;
    };
  }

  // An import's props: what the component gets. Kebab-case names are camelCased except data-* and aria-*, which the DOM
  // reads as they are; class stays class, style belongs to the host box.
  function compileComponentProps(el, skip) {
    var readers = [];
    el.getAttributeNames().forEach(function (raw) {
      if (raw === 'data-dc-tpl' || raw === 'hint-size' || raw === 'style' || skip[raw]) return;
      if (raw.indexOf('style-') === 0) { pseudoClass(raw.slice(6), el.getAttribute(raw)); return; }
      var name = authoredName(raw);
      if (!/^(data|aria)-/.test(name)) name = kebabToCamel(name);
      readers.push({ name: name, read: compileValue(el.getAttribute(raw)) });
    });
    return function (scope) {
      var props = {};
      for (var i = 0; i < readers.length; i += 1) {
        var value = readers[i].read(scope);
        if (value !== undefined) props[readers[i].name] = value;
      }
      return props;
    };
  }

  // A component's box carries only what places the box; every other style is the component's own business.
  var HOST_STYLES = { width: 1, height: 1, position: 1, top: 1, left: 1, right: 1, bottom: 1, inset: 1, zIndex: 1, transform: 1 };

  function hostBoxStyle(value) {
    var style = styleObject(value);
    var kept = null;
    if (style) {
      Object.keys(style).forEach(function (name) {
        if (!HOST_STYLES[name]) return;
        kept = kept || {};
        kept[name] = style[name];
      });
    }
    return kept || { display: 'contents' };
  }

  function placeholderStyle(hint) {
    if (typeof hint !== 'string' || !hint) return { width: '100%', height: '60px' };
    var sizes = hint.split(',');
    var style = {};
    if (sizes[0] && sizes[0].trim()) style.width = sizes[0].trim();
    if (sizes[1] && sizes[1].trim()) style.height = sizes[1].trim();
    return style;
  }

  // ---------------------------------------------------------------------------------------------------------------
  // Template to builders. A builder takes (scope, run) and returns React children; run is the drawing host's context.
  // ---------------------------------------------------------------------------------------------------------------

  function h() { return window.React.createElement.apply(window.React, arguments); }

  function buildChildren(parent, host) {
    var builders = [];
    for (var node = parent.firstChild; node; node = node.nextSibling) {
      var builder = null;
      if (node.nodeType === 3) builder = buildText(node.nodeValue);
      else if (node.nodeType === 1) builder = buildElement(node, host);
      if (builder) builders.push(builder);
    }
    return function (scope, run) {
      var out = [];
      for (var i = 0; i < builders.length; i += 1) {
        // An empty result keeps its place, so that a sc-if turning off does not make React remount the siblings after it.
        var made = builders[i](scope, run);
        out.push(made === undefined ? null : made);
      }
      return out;
    };
  }

  // White space with no plain space in it is formatting: the board's line breaks do not reach the DOM.
  function buildText(text) {
    if (/^\s*$/.test(text) && text.indexOf(' ') < 0) return null;
    if (text.indexOf('{{') < 0) return constant(text);
    var parts = splitExpressions(text);
    var drawn = parts.map(function (part) {
      if (typeof part === 'string') return constant(part);
      return function (scope, run) { return interpolation(part, scope, run); };
    });
    return function (scope, run) {
      return fragment(drawn.map(function (draw) { return draw(scope, run); }));
    };
  }

  function interpolation(part, scope, run) {
    var value = part.read(scope);
    if (value === undefined) {
      if (run.streaming) return h('span', { className: 'sc-interp sc-missing' }, part.source.trim());
      run.warnUnresolved(part.source);
      return null;
    }
    if (value === null || typeof value === 'boolean') return null;
    if (Array.isArray(value) || window.React.isValidElement(value)) return value;
    return h('span', { className: 'sc-interp' }, String(value));
  }

  // Children with stable positions: a child's key is its place, so a list that changes reuses its nodes in order.
  function fragment(children, key) {
    var props = key === undefined ? null : { key: key };
    return window.React.createElement.apply(window.React, [window.React.Fragment, props].concat(children));
  }

  function buildElement(el, host) {
    var tag = el.localName;
    if (tag === 'sc-helmet') return null;
    if (tag === 'sc-if') return buildIf(el, host);
    if (tag === 'sc-for') return buildFor(el, host);
    if (tag === 'x-import') return buildComponentImport(el, host);
    if (tag === 'dc-import') return buildBoardImport(el, host);
    var type = tag.indexOf('sc-raw-') === 0 ? tag.slice(7) : tag;
    var props = compileElementProps(el);
    var children = el.localName === 'template' ? constant([]) : buildChildren(el, host);
    return function (scope, run) {
      return h.apply(null, [type, props(scope)].concat(children(scope, run)));
    };
  }

  function buildIf(el, host) {
    var read = el.hasAttribute('value') ? compileValue(el.getAttribute('value')) : never;
    var hint = el.hasAttribute('hint-placeholder-val') ? compileValue(el.getAttribute('hint-placeholder-val')) : never;
    var children = buildChildren(el, host);
    return function (scope, run) {
      var value = read(scope);
      if (value === undefined && run.streaming) value = hint(scope);
      return value ? fragment(children(scope, run)) : null;
    };
  }

  function buildFor(el, host) {
    var listSource = el.getAttribute('list') || '';
    var read = compileValue(listSource);
    var as = el.getAttribute('as');
    var count = Number(el.getAttribute('hint-placeholder-count')) || 0;
    var children = buildChildren(el, host);
    return function (scope, run) {
      var list = read(scope);
      if (list === undefined && run.streaming) {
        list = [];
        for (var i = 0; i < count; i += 1) list.push(undefined);
      }
      if (!Array.isArray(list)) {
        if (list !== undefined && list !== null) run.warnOnce('sc-for list="' + listSource + '" is not an array (' + typeof list + ')');
        return null;
      }
      return fragment(list.map(function (item, index) {
        var inner = Object.create(scope);
        if (as) inner[as] = item;
        inner.$index = index;
        return fragment(children(inner, run), index);
      }));
    };
  }

  // x-import: a React component named by a dotted path on window, optionally from files that define it.
  function buildComponentImport(el, host) {
    var name = el.hasAttribute('component-from-global-scope') ? compileValue(el.getAttribute('component-from-global-scope')) : constant('');
    var from = el.getAttribute('from');
    var hint = compileValue(el.getAttribute('hint-size') || '');
    var boxStyle = el.hasAttribute('style') ? compileValue(el.getAttribute('style')) : never;
    var props = compileComponentProps(el, { 'component-from-global-scope': 1, from: 1 });
    var children = buildChildren(el, host);
    var tplNumber = el.getAttribute('data-dc-tpl');
    // A component's files load with the template, drawn or not: they may bring styles and globals the board relies on.
    if (from) from.split(/\s+/).filter(Boolean).forEach(loadComponentFile);
    return function (scope, run) {
      var path = name(scope);
      var component = typeof path === 'string' ? globalAt(path) : undefined;
      var inner;
      if (component) {
        inner = h.apply(null, [component, props(scope)].concat(children(scope, run)));
      } else {
        if (typeof path === 'string' && path) awaitGlobal(path);
        inner = h('div', { className: 'sc-placeholder', title: path === undefined || path === null ? '' : String(path), style: placeholderStyle(hint(scope)) });
      }
      return h('div', { className: 'sc-host-x', 'data-dc-tpl': tplNumber, style: hostBoxStyle(boxStyle(scope)) }, inner);
    };
  }

  // dc-import: another board of the project, drawn in a host of its own, with its own numbers.
  function buildBoardImport(el, host) {
    var name = compileValue(el.getAttribute('name') || '');
    var hint = compileValue(el.getAttribute('hint-size') || '');
    var boxStyle = el.hasAttribute('style') ? compileValue(el.getAttribute('style')) : never;
    var props = compileComponentProps(el, { name: 1 });
    var children = buildChildren(el, host);
    var tplNumber = el.getAttribute('data-dc-tpl');
    return function (scope, run) {
      var board = name(scope);
      var given = props(scope);
      given.__name = String(board);
      given.__box = { tpl: tplNumber, style: styleObject(boxStyle(scope)), hint: hint(scope) };
      return h.apply(null, [getDC(given.__name), given].concat(children(scope, run)));
    };
  }

  function globalAt(path) {
    var value = window;
    var keys = path.split('.');
    for (var i = 0; i < keys.length; i += 1) {
      if (value === undefined || value === null) return undefined;
      value = value[keys[i]];
    }
    var usable = typeof value === 'function' || (value && typeof value === 'object' && value.$$typeof);
    return usable ? value : undefined;
  }

  // A kit may define itself later than the first draw: the names still missing are looked up until they appear.
  var missingGlobals = Object.create(null);
  var globalsTimer = null;
  var globalsWaited = 0;

  function awaitGlobal(path) {
    if (missingGlobals[path]) return;
    missingGlobals[path] = true;
    if (globalsTimer) return;
    globalsWaited = 0;
    scheduleGlobalsCheck();
  }

  function scheduleGlobalsCheck() {
    globalsTimer = setTimeout(function () {
      globalsTimer = null;
      globalsWaited += 1;
      var found = false;
      var left = 0;
      Object.keys(missingGlobals).forEach(function (path) {
        if (globalAt(path)) { delete missingGlobals[path]; found = true; } else left += 1;
      });
      if (found) redrawAll();
      if (left) scheduleGlobalsCheck();
    }, globalsWaited < 150 ? 100 : 2000);
  }

  // ---------------------------------------------------------------------------------------------------------------
  // Files an x-import pulls in.
  // ---------------------------------------------------------------------------------------------------------------

  var resources = function () { return window.__resources || null; };

  function resourceUrl(url) {
    var map = resources();
    return (map && map[url]) || url;
  }

  var scriptsLoading = Object.create(null);

  function loadScript(url) {
    if (scriptsLoading[url]) return scriptsLoading[url];
    scriptsLoading[url] = new Promise(function (resolve, reject) {
      var script = document.createElement('script');
      var resolved = resourceUrl(url);
      script.src = resolved;
      if (resolved === url && INTEGRITY[url]) {
        script.integrity = INTEGRITY[url];
        script.crossOrigin = 'anonymous';
      }
      script.onload = function () { resolve(); };
      script.onerror = function () { reject(new Error('could not load ' + resolved)); };
      document.head.appendChild(script);
    });
    return scriptsLoading[url];
  }

  function ensureBabel() {
    if (window.Babel) return Promise.resolve();
    return loadScript(BABEL_URL);
  }

  var filesLoading = Object.create(null);

  function loadComponentFile(file) {
    if (filesLoading[file]) return filesLoading[file];
    var jsx = /\.(jsx|tsx)$/i.test(file);
    var kind = jsx ? 'jsx' : 'js';
    console.info(LOG + ' x-import: loading ' + file + ' (' + kind + ')');
    filesLoading[file] = fetch(resourceUrl(file)).then(function (response) {
      if (!response.ok) throw new Error('HTTP ' + response.status);
      return response.text();
    }).then(function (source) {
      return (jsx || /^\s*export\s/m.test(source) ? ensureBabel() : Promise.resolve()).then(function () {
        var before = new Set(Object.getOwnPropertyNames(window));
        var exported = runComponentFile(source, file);
        var added = Object.getOwnPropertyNames(window).filter(function (key) { return !before.has(key); });
        console.info(LOG + ' x-import: loaded ' + file + ' — exports: [' + exported.join(', ') + '] window globals: [' + added.join(', ') + ']');
        redrawAll();
      });
    }).catch(function (error) {
      console.error(LOG + ' x-import: FAILED to load ' + file + ' (' + kind + ')', error);
    });
    return filesLoading[file];
  }

  // A component file runs in the page's global scope, as a classic script would; its exports, if any, land on window.
  function runComponentFile(source, file) {
    var usesExports = /^\s*export\s/m.test(source);
    var code = source;
    if (window.Babel && (/\.(jsx|tsx)$/i.test(file) || usesExports)) {
      var options = { presets: ['react'], filename: file, sourceType: 'module', compact: false };
      if (/\.tsx$/i.test(file)) options.presets.push('typescript');
      if (usesExports) options.plugins = ['transform-modules-commonjs'];
      else options.sourceType = 'script';
      code = window.Babel.transform(source, options).code;
    }
    if (!usesExports) {
      (0, eval)(code + '\n//# sourceURL=' + encodeURI(file));
      return [];
    }
    var moduleObject = { exports: {} };
    new Function('exports', 'module', code + '\n//# sourceURL=' + encodeURI(file))(moduleObject.exports, moduleObject);
    var names = Object.keys(moduleObject.exports).filter(function (key) { return key !== '__esModule'; });
    names.forEach(function (key) {
      if (key !== 'default') window[key] = moduleObject.exports[key];
    });
    return names;
  }

  // ---------------------------------------------------------------------------------------------------------------
  // The registry: what is known about each board, by name.
  // ---------------------------------------------------------------------------------------------------------------

  var registry = Object.create(null);
  var rootName = null;
  var atomicsDone = false;

  function entryOf(name) {
    if (!registry[name]) {
      registry[name] = {
        html: null, tpl: null, compiled: null, Logic: DCLogic, js: '', jsStreaming: false, htmlStreaming: false, ver: 0,
        subs: new Set(), fetched: false, propsMeta: {}, preview: null, jsSeq: 0, logicError: null,
        helmetNodes: [], helmetSource: null, warned: new Set(), failed: false
      };
    }
    return registry[name];
  }

  function notify(entry) {
    entry.ver += 1;
    entry.subs.forEach(function (host) { host.redraw(); });
  }

  function redrawAll() {
    Object.keys(registry).forEach(function (name) {
      registry[name].subs.forEach(function (host) { host.redraw(); });
    });
  }

  function setTemplate(entry, html, name) {
    entry.html = html;
    var sheetBefore = pseudoSheet;
    entry.compiled = compileTemplate(html, name);
    entry.tpl = entry.compiled.build;
    entry.warned = new Set();
    // The order in <head>: canvas, atomics, the sheet of style-hover and the like, fill, then the helmet's own elements.
    var pseudoAdded = pseudoSheet !== sheetBefore ? pseudoSheet : null;
    var element = entry.compiled.element;
    if (name === rootName) applyCanvasSheet(element, pseudoAdded);
    syncAtomics(element, pseudoAdded);
    if (name === rootName) toggleSheet('fill', !entry.preview, 'html,body{height:100%;margin:0}#dc-root,#dc-root>.sc-host{height:100%}', {});
    syncHelmet(entry, element);
  }

  // Compiling a logic script: it runs once, per compile, and whatever it calls Component is the board's class.
  function compileLogic(js, name) {
    if (!js || !js.trim()) return DCLogic;
    var factory = new Function('DCLogic', 'StreamableLogic', 'React', 'ReactDOM',
      js + '\n;return typeof Component === "undefined" ? null : Component;\n//# sourceURL=dc-logic/' + encodeURIComponent(name) + '.js');
    return factory(DCLogic, DCLogic, window.React, window.ReactDOM) || DCLogic;
  }

  function setLogic(entry, js, name, streaming) {
    entry.js = js;
    entry.jsSeq += 1;
    try {
      entry.Logic = compileLogic(js, name);
      entry.logicError = null;
    } catch (error) {
      if (!streaming) {
        entry.logicError = name + ': ' + (error && error.message);
        console.error(LOG + ' logic script of ' + name + ' failed:', error);
      }
      return false;
    }
    entry.subs.forEach(function (host) { host.replaceLogic(); });
    return true;
  }

  function setPropsMeta(entry, text) {
    var meta = {};
    var preview = null;
    if (text) {
      try {
        var parsed = JSON.parse(text);
        Object.keys(parsed || {}).forEach(function (key) {
          if (key === '$preview') preview = parsed[key];
          else meta[key] = parsed[key];
        });
      } catch (error) {
        console.warn(LOG + ' data-props is not JSON:', error && error.message);
      }
    }
    entry.propsMeta = meta;
    entry.preview = preview;
  }

  // Two sheets depend on what the root board says about itself: a board with no fixed preview size fills the window (see
  // setTemplate), and a canvas board (<meta name="design_doc_mode" content="canvas"> in its helmet) has the canvas's background.
  function applyCanvasSheet(tpl, before) {
    var canvas = !!tpl.content.querySelector('sc-helmet meta[name="design_doc_mode"][content="canvas"]');
    var ground = (themeAtStart || document.documentElement.getAttribute('data-theme')) === 'dark' ? '#2e2c26' : '#f0eee6';
    toggleSheet('canvas', canvas, 'html,body{background:' + ground + '}#dc-root>.sc-host{position:relative}', { 'data-dc-canvas': '' }, before);
    if (canvas) document.documentElement.setAttribute('data-dc-canvas', '');
    else document.documentElement.removeAttribute('data-dc-canvas');
  }

  var rootSheets = {};
  // A page that brought React itself is ready before its helmet's scripts ran: its theme is what <html> says at the start.
  var themeAtStart = window.React && window.ReactDOM ? document.documentElement.getAttribute('data-theme') || 'light' : null;

  function toggleSheet(key, on, css, attrs, before) {
    if (on && !rootSheets[key]) {
      var sheet = document.createElement('style');
      Object.keys(attrs).forEach(function (name) { sheet.setAttribute(name, attrs[name]); });
      sheet.textContent = css;
      document.head.insertBefore(sheet, before || null);
      rootSheets[key] = sheet;
    } else if (!on && rootSheets[key]) {
      rootSheets[key].remove();
      rootSheets[key] = null;
    }
  }

  function syncAtomics(tpl, before) {
    if (atomicsDone || !tpl.content.querySelector('sc-helmet[data-dc-atomics]')) return;
    atomicsDone = true;
    var atomics = document.createElement('style');
    atomics.id = '__dc-atomics';
    atomics.textContent = ATOMICS;
    document.head.insertBefore(atomics, before || null);
  }

  // The same href or src as written, already placed by a helmet: boards sharing a font or a kit load it once.
  function sameResourceInHead(child) {
    var attr = child.localName === 'link' ? 'href' : child.localName === 'script' ? 'src' : null;
    var written = attr && child.getAttribute(attr);
    if (!written) return false;
    return Array.prototype.some.call(document.head.children, function (node) {
      return node.localName === child.localName && node.__dcWritten === written;
    });
  }

  // Helmet: its elements go to the end of <head> once per board, a script being made anew so that it runs.
  function syncHelmet(entry, tpl) {
    var helmet = tpl.content.querySelector('sc-helmet');
    var source = helmet ? helmet.innerHTML : '';
    if (source === entry.helmetSource) return;
    entry.helmetSource = source;
    entry.helmetNodes.forEach(function (node) { if (node.parentNode) node.parentNode.removeChild(node); });
    entry.helmetNodes = [];
    if (!helmet) return;
    Array.prototype.forEach.call(helmet.children, function (child) {
      if (sameResourceInHead(child)) return;
      var node;
      if (child.localName === 'script') {
        node = document.createElement('script');
        child.getAttributeNames().forEach(function (attr) { node.setAttribute(attr, child.getAttribute(attr)); });
        node.textContent = child.textContent;
        node.__dcWritten = child.getAttribute('src');
      } else {
        node = document.importNode(child, true);
        if (node.localName === 'link' && node.getAttribute('href')) {
          node.__dcWritten = node.getAttribute('href');
          node.setAttribute('href', resourceUrl(node.__dcWritten));
        }
      }
      document.head.appendChild(node);
      entry.helmetNodes.push(node);
    });
  }

  // ---------------------------------------------------------------------------------------------------------------
  // The logic class a board extends.
  // ---------------------------------------------------------------------------------------------------------------

  function DCLogic(props) {
    this.props = props || {};
    this.state = this.state || {};
  }
  DCLogic.prototype.setState = function (update, done) {
    var host = this.__host;
    var logic = this;
    function apply() {
      var next = typeof update === 'function' ? update(logic.state, logic.props) : update;
      if (next) logic.state = Object.assign({}, logic.state, next);
    }
    if (!host || !host.mounted) {
      apply();
      if (done) done.call(this);
      return;
    }
    host.setState(function () {
      apply();
      return { tick: host.nextTick() };
    }, done ? function () { done.call(logic); } : undefined);
  };
  DCLogic.prototype.forceUpdate = function () {
    if (this.__host && this.__host.mounted) this.__host.redraw();
  };
  DCLogic.prototype.componentDidMount = function () {};
  DCLogic.prototype.componentDidUpdate = function () {};
  DCLogic.prototype.componentWillUnmount = function () {};
  DCLogic.prototype.renderVals = function () { return {}; };

  // ---------------------------------------------------------------------------------------------------------------
  // The host: one React component per drawn board, built when React is on the page.
  // ---------------------------------------------------------------------------------------------------------------

  var HostClass = null;
  var dispatchers = Object.create(null);

  function hostClass() {
    if (HostClass) return HostClass;
    var React = window.React;

    function BoardHost(props) {
      React.Component.call(this, props);
      this.state = { tick: 0, error: null };
      this.mounted = false;
      this.ticks = 0;
      this.logic = null;
      this.ctorError = null;
      this.seenVer = -1;
      this.makeLogic();
    }
    BoardHost.prototype = Object.create(React.Component.prototype);
    BoardHost.prototype.constructor = BoardHost;
    BoardHost.getDerivedStateFromError = function (error) { return { error: error }; };

    BoardHost.prototype.nextTick = function () { this.ticks += 1; return this.ticks; };
    BoardHost.prototype.entry = function () { return entryOf(this.props.__name); };

    BoardHost.prototype.userProps = function () {
      var name = this.props.__name;
      var entry = this.entry();
      var out = {};
      if (name === rootName) {
        Object.keys(entry.propsMeta).forEach(function (key) {
          if (entry.propsMeta[key] && 'default' in entry.propsMeta[key]) out[key] = entry.propsMeta[key].default;
        });
      }
      Object.assign(out, entry.propOverrides);
      var given = this.props;
      Object.keys(given).forEach(function (key) {
        if (key !== '__name' && key !== '__box') out[key] = given[key];
      });
      return out;
    };

    BoardHost.prototype.makeLogic = function () {
      var entry = this.entry();
      var props = this.userProps();
      this.ctorError = null;
      try {
        this.logic = new entry.Logic(props);
      } catch (error) {
        this.ctorError = error;
        console.error(LOG + ' ' + this.props.__name + ': constructor failed:', error);
        this.logic = new DCLogic(props);
      }
      this.logic.props = props;
      if (!this.logic.state) this.logic.state = {};
      this.logic.__host = this;
    };

    BoardHost.prototype.replaceLogic = function () {
      if (!this.mounted) { this.makeLogic(); return; }
      this.callLogic('componentWillUnmount');
      this.makeLogic();
      this.pendingMount = true;
      this.redraw();
    };

    BoardHost.prototype.callLogic = function (method, args) {
      try {
        this.logic[method].apply(this.logic, args || []);
      } catch (error) {
        console.error(LOG + ' ' + this.props.__name + '.' + method + '():', error);
        this.lifecycleError = this.props.__name + '.' + method + '(): ' + (error && error.message);
        this.redraw();
      }
    };

    BoardHost.prototype.redraw = function () {
      if (!this.mounted) return;
      this.setState({ tick: this.nextTick() });
    };

    BoardHost.prototype.componentDidMount = function () {
      var entry = this.entry();
      this.mounted = true;
      this.drawnProps = this.logic.props;
      entry.subs.add(this);
      this.callLogic('componentDidMount');
    };

    BoardHost.prototype.componentDidUpdate = function (previous) {
      if (this.pendingMount) {
        this.pendingMount = false;
        this.callLogic('componentDidMount');
        return;
      }
      // What the board last drew with, under the name its host goes by; there is no previous state argument.
      this.callLogic('componentDidUpdate', [Object.assign({}, this.drawnProps, { __name: previous.__name })]);
      this.drawnProps = this.logic.props;
    };

    BoardHost.prototype.componentWillUnmount = function () {
      this.mounted = false;
      this.entry().subs.delete(this);
      this.callLogic('componentWillUnmount');
    };

    BoardHost.prototype.componentDidCatch = function (error) {
      console.error(LOG + ' render error in <' + this.props.__name + '>:', error);
    };

    BoardHost.prototype.render = function () {
      var name = this.props.__name;
      var entry = this.entry();
      var box = this.props.__box;
      var classes = 'sc-host';
      if (entry.htmlStreaming) classes += ' sc-streaming-html';
      if (entry.jsStreaming) classes += ' sc-streaming-js';
      var hostProps = { className: classes, 'data-sc-name': name };
      if (box) hostProps['data-dc-tpl'] = box.tpl;
      if (box && box.style) hostProps.style = box.style;

      // A new version of the template gets a new chance after a render error.
      if (this.seenVer !== entry.ver) {
        this.seenVer = entry.ver;
        if (this.state.error) this.state.error = null;
      }

      if (!entry.tpl) {
        return h('div', hostProps, h('div', { className: 'sc-placeholder', title: name, style: placeholderStyle(box && box.hint) }));
      }

      if (this.state.error) {
        var message = name + ': ' + (this.state.error && this.state.error.message);
        hostProps.className = classes + ' sc-has-error';
        return h('div', hostProps, errorBanner(message),
          h('div', { className: 'sc-placeholder', title: name, style: { width: '100%', height: '60px' } },
            h('div', { className: 'sc-placeholder-error' }, message)));
      }

      var logic = this.logic;
      logic.props = this.userProps();
      var problem = this.lifecycleError || (this.ctorError ? name + ': ' + this.ctorError.message : null);
      var values = null;
      try {
        values = logic.renderVals();
      } catch (error) {
        problem = name + '.renderVals(): ' + (error && error.message);
      }
      var scope = Object.assign(Object.create(null), logic.props, values);
      var run = {
        streaming: entry.htmlStreaming,
        warnUnresolved: function (source) {
          this.warnOnce('{{ ' + source.trim() + ' }} never resolved — rendered as empty');
        },
        warnOnce: function (text) {
          if (entry.warned.has(text)) return;
          entry.warned.add(text);
          console.warn(LOG + ' ' + name + ': ' + text);
        }
      };
      hostProps.className = classes + (problem ? ' sc-has-error' : '');
      // Two fixed slots, so a banner that comes and goes does not remount the board under it.
      return h('div', hostProps, problem ? errorBanner(problem) : null, fragment(entry.tpl(scope, run)));
    };

    HostClass = BoardHost;
    return HostClass;
  }

  function errorBanner(text) {
    return h('div', { className: 'sc-logic-error', 'data-omelette-chrome': '' }, text);
  }

  // The React component of a board by name: a sibling board's file is fetched the first time it is asked for.
  function getDC(name) {
    if (dispatchers[name]) return dispatchers[name];
    function Dispatcher(props) {
      if (!registry[name]) loadSibling(name);
      return window.React.createElement(hostClass(), props);
    }
    Dispatcher.displayName = 'DC(' + name + ')';
    dispatchers[name] = Dispatcher;
    return Dispatcher;
  }

  function loadSibling(name) {
    var entry = entryOf(name);
    var file = './' + name + '.dc.html';
    fetch(resourceUrl(file)).then(function (response) {
      if (!response.ok) {
        console.error(LOG + ' sibling fetch for "' + name + '" failed: ' + file + ' returned ' + response.status + ' — the reference renders as an empty placeholder.');
        return;
      }
      return response.text().then(function (text) {
        var parts = parseDcText(text);
        if (!parts) {
          console.error(LOG + ' sibling fetch for "' + name + '" failed: ' + file + ' has no <x-dc> — the reference renders as an empty placeholder.');
          return;
        }
        entry.fetched = true;
        setPropsMeta(entry, parts.props);
        setTemplate(entry, parts.template, name);
        setLogic(entry, parts.js, name, false);
        notify(entry);
      });
    }).catch(function (error) {
      console.error(LOG + ' sibling fetch for "' + name + '" failed:', error);
    });
  }

  // ---------------------------------------------------------------------------------------------------------------
  // What the runtime around a board calls.
  // ---------------------------------------------------------------------------------------------------------------

  // kind: 'html' the template, 'js' the logic script, 'props' the data-props text. streaming: more of it is coming, so
  // a partial script is not an error and what does not resolve yet is shown as a place held.
  window.__dcUpdate = function (name, kind, value, streaming) {
    var entry = entryOf(name);
    if (kind === 'html') {
      entry.htmlStreaming = !!streaming;
      setTemplate(entry, String(value), name);
      notify(entry);
    } else if (kind === 'js') {
      entry.jsStreaming = !!streaming;
      if (setLogic(entry, String(value), name, !!streaming)) notify(entry);
    } else if (kind === 'props') {
      setPropsMeta(entry, value);
      notify(entry);
    }
  };

  window.__dcStreaming = function (on) {
    document.documentElement.classList.toggle('sc-dc-streaming', !!on);
  };

  window.__dcSetProps = function (name, overrides) {
    var entry = entryOf(name);
    entry.propOverrides = overrides || {};
    notify(entry);
  };

  // CSS keyframes held at one moment, for a player that drives time itself; -1 or null lets them run again.
  var playhead = null;
  window.__dcSeekKeyframes = function (ms) {
    var animations = document.getAnimations();
    if (ms === null || ms === undefined || ms < 0) {
      playhead = null;
      animations.forEach(function (animation) { animation.play(); });
      return;
    }
    playhead = ms;
    animations.forEach(function (animation) {
      animation.pause();
      animation.currentTime = ms;
    });
  };
  window.__dcKeyframesPlayhead = function () { return playhead; };

  window.__dcRootName = function () { return rootName; };
  window.__dcAnnotatedTemplate = function (name) {
    var entry = registry[name];
    return entry && entry.compiled ? entry.compiled.element.innerHTML : null;
  };
  window.__dcTemplateSource = function (name) {
    var entry = registry[name];
    return entry ? entry.html : null;
  };
  window.__dcRegistry = registry;
  window.__dcContentKeyed = true;
  window.getDC = getDC;
  window.DCLogic = DCLogic;
  window.StreamableLogic = DCLogic;

  // ---------------------------------------------------------------------------------------------------------------
  // Styles the engine brings: the boxes that hold a place, the error banner and what printing needs.
  // ---------------------------------------------------------------------------------------------------------------

  var BASE_CSS = [
    '.sc-placeholder{box-sizing:border-box;overflow:hidden;border-radius:2px;background:color-mix(in srgb,currentColor 8%,transparent);border:1px solid color-mix(in srgb,currentColor 50%,transparent)}',
    '.sc-placeholder-error{padding:4px 8px;font:11px/1.4 ui-monospace,monospace;word-break:break-word;color:color-mix(in srgb,currentColor 70%,transparent)}',
    '.sc-interp.sc-missing{display:inline-block;box-sizing:border-box;overflow:hidden;width:2em;height:1em;vertical-align:text-bottom;color:transparent;user-select:none;border-radius:2px;background:rgba(255,255,255,.3);border:1px solid rgba(0,0,0,.5)}',
    '.sc-interp.sc-unresolved{padding:0 3px;border-radius:3px;font-family:ui-monospace,monospace;font-size:.85em;color:color-mix(in srgb,currentColor 50%,transparent);background:color-mix(in srgb,currentColor 10%,transparent)}',
    '@keyframes sc-held{from{opacity:.45}to{opacity:1}}',
    'html.sc-dc-streaming .sc-placeholder,html.sc-dc-streaming .sc-interp.sc-missing{border-color:transparent;background:color-mix(in srgb,currentColor 6%,transparent);animation:sc-held .9s ease-in-out infinite alternate}',
    '.sc-host.sc-has-error{position:relative}',
    '.sc-logic-error{position:absolute;z-index:2147483647;top:8px;left:8px;max-width:60ch;padding:6px 10px;border-radius:4px;pointer-events:none;white-space:pre-wrap;background:#b00020;color:#fff;font:12px/1.4 ui-monospace,monospace}',
    '@media print{@page{margin:.5cm}figure,table{break-inside:avoid}#dc-root,#dc-root>.sc-host{height:auto}' +
      '*,*::before,*::after{print-color-adjust:exact;-webkit-print-color-adjust:exact;backdrop-filter:none!important;-webkit-backdrop-filter:none!important;' +
      'transition-duration:0s!important;animation-play-state:running!important;animation-duration:.001s!important;animation-delay:-99s!important;animation-iteration-count:1!important;animation-fill-mode:both!important}}'
  ].join('\n');

  // Helmet's data-dc-atomics: one-property utility classes a board may use by name.
  var ATOMICS = [
    ['fx', 'display:flex'], ['col', 'display:flex;flex-direction:column'], ['grid', 'display:grid'], ['ac', 'align-items:center'],
    ['jc', 'justify-content:center'], ['jb', 'justify-content:space-between'], ['f1', 'flex:1'], ['noshrink', 'flex-shrink:0'],
    ['wrap', 'flex-wrap:wrap'], ['fw5', 'font-weight:500'], ['fw6', 'font-weight:600'], ['fw7', 'font-weight:700'], ['fw8', 'font-weight:800'],
    ['fs11', 'font-size:11px'], ['fs12', 'font-size:12px'], ['fs13', 'font-size:13px'], ['fs14', 'font-size:14px'], ['fs15', 'font-size:15px'],
    ['fs16', 'font-size:16px'], ['fs20', 'font-size:20px'], ['fs22', 'font-size:22px'], ['upper', 'text-transform:uppercase'], ['tc', 'text-align:center'],
    ['nowrap', 'white-space:nowrap'], ['gap8', 'gap:8px'], ['gap10', 'gap:10px'], ['gap12', 'gap:12px'], ['gap16', 'gap:16px'], ['gap24', 'gap:24px'],
    ['m0', 'margin:0'], ['mt8', 'margin-top:8px'], ['mt12', 'margin-top:12px'], ['mt16', 'margin-top:16px'], ['mb8', 'margin-bottom:8px'],
    ['mb12', 'margin-bottom:12px'], ['mb16', 'margin-bottom:16px'], ['posrel', 'position:relative'], ['posabs', 'position:absolute'], ['round', 'border-radius:50%'],
    ['ohide', 'overflow:hidden'], ['bbox', 'box-sizing:border-box'], ['pointer', 'cursor:pointer'], ['w100', 'width:100%'], ['b0', 'border:none']
  ].map(function (rule) { return '.' + rule[0] + '{' + rule[1] + '}'; }).join('');

  // ---------------------------------------------------------------------------------------------------------------
  // Start.
  // ---------------------------------------------------------------------------------------------------------------

  // React is written into the document while the parser is here, so the scripts after this one already find it.
  function bringReact() {
    if (window.React && window.ReactDOM) return;
    var urls = [];
    if (!window.React) urls.push(REACT_URL);
    if (!window.ReactDOM) urls.push(REACT_DOM_URL);
    if (document.readyState === 'loading' && document.currentScript && !document.currentScript.async && !document.currentScript.defer) {
      urls.forEach(function (url) {
        var resolved = resourceUrl(url);
        var extra = resolved === url ? ' integrity="' + INTEGRITY[url] + '" crossorigin="anonymous"' : '';
        document.write('<script src="' + resolved + '"' + extra + '><\/script>');
      });
      return;
    }
    urls.forEach(function (url) { loadScript(url); });
  }

  function whenReact(then) {
    if (window.React && window.ReactDOM) { then(); return; }
    setTimeout(function () { whenReact(then); }, 10);
  }

  function installStyles() {
    var base = document.createElement('style');
    base.textContent = BASE_CSS;
    document.head.insertBefore(base, document.head.firstChild);
    var hide = document.createElement('style');
    hide.textContent = 'x-dc{display:none!important}';
    document.head.appendChild(hide);
  }

  function firstBoardText(standalone) {
    if (standalone) return Promise.resolve(null);
    return fetch(location.pathname + location.search, { cache: 'no-store' }).then(function (response) {
      return response.ok ? response.text() : null;
    }).catch(function () { return null; });
  }

  // The template as the DOM holds it: only for a page that cannot read its own source (a standalone export).
  function partsFromDom(holder) {
    var script = document.querySelector('script[data-dc-script]');
    return {
      template: holder ? holder.innerHTML : '',
      js: script ? script.textContent : '',
      props: script ? script.getAttribute('data-props') : null
    };
  }

  function boot() {
    var holder = document.querySelector('x-dc');
    if (!holder) return;
    rootName = dcNameFromPath(location.pathname);
    // The holder is emptied now: what its raw copy started to load (an iframe, an image) has no reason to finish. It leaves
    // the page with the first draw, so that an empty #dc-root with no <x-dc> always means a drawn board with nothing in it.
    var root = document.createElement('div');
    root.id = 'dc-root';
    holder.parentNode.insertBefore(root, holder);
    var domParts = partsFromDom(holder);
    holder.textContent = '';
    // window.__resources present here means a standalone export. vendor.go's getter answers undefined to this one read
    // on a page served by the runtime, which then reads its own source.
    var standalone = !!window.__resources;
    firstBoardText(standalone).then(function (text) {
      var parts = text ? parseDcText(text) : null;
      var entry = entryOf(rootName);
      entry.fetched = !!parts;
      parts = parts || domParts;
      whenReact(function () {
        setPropsMeta(entry, parts.props);
        entry.ver = 1;
        setTemplate(entry, parts.template, rootName);
        setLogic(entry, parts.js, rootName, false);
        var react = window.ReactDOM.createRoot(root);
        var element = window.React.createElement(getDC(rootName), { __name: rootName });
        window.ReactDOM.flushSync(function () { react.render(element); });
        holder.remove();
      });
    });
  }

  installStyles();
  bringReact();
  window.__dcBoot = boot;
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', boot);
  else boot();
})();
