// The art library of a video's board: heroes, things and backdrops drawn in code, flat and rounded, in the colours of
// the deck's theme. Load it after tk.js, then the packs you use (heroes.js, props.js, backdrops.js).
//   <svg data-tk-art="hero-round"></svg>     a place for a drawing: the runtime fills it before your motion builds
//   tk.art('prop-chart', container)          or draw one from the code; returns the <svg>
//   tk.art.list()                            the names that are drawn
// Colours come from --deck-* (acc, acc-soft, ink, panel, line, bg, on-acc) so a drawing is in the deck's theme; skin, hair and the
// few signal colours are fixed. A drawing has named parts (data-part="head", "armR", "bar"...) with pivots (data-pivot="x y").
(function () {
  'use strict';
  if (!window.tk) throw new Error('art.js needs tk.js before it');
  var tk = window.tk;
  var STYLE_ID = 'tk-art-style';

  // One palette. Fills are classes (f-*), lines are s-* with round ends: an SVG attribute cannot hold var().
  var CSS = [
    '.tk-art{display:block;overflow:visible;',
    '--a-acc:var(--deck-acc,#7b61ff);--a-on:var(--deck-on-acc,#fff);--a-ink:var(--deck-ink,#16161a);--a-bg:var(--deck-bg,#fff);',
    '--a-panel:var(--deck-panel,#f4f2fc);--a-line:var(--deck-line,#cfc8f5);',
    '--a-acc2:color-mix(in srgb,var(--a-acc) 76%,#10102a);--a-acc3:color-mix(in srgb,var(--a-acc) 45%,#fff);',
    '--a-soft:var(--deck-acc-soft,color-mix(in srgb,var(--a-acc) 16%,#fff));',
    '--a-pants:color-mix(in srgb,var(--a-ink) 84%,var(--a-acc));',
    '--a-skin:#f3bd94;--a-skin2:#e29d72;--a-hair:#34283c;--a-hair2:#4d3b57;--a-warm:#ff9a52;--a-warm2:#e97a2e;',
    '--a-good:#33c07f;--a-bad:#ef5a5a;--a-sun:#ffd15c;--a-sun2:#f5b73a;--a-sky:#9ad8ff;--a-sky2:#6ec0f2;',
    '--a-white:#fff;--a-dark:#262432;--a-dark2:#3a3748;--a-steel:#d5d9e2;--a-steel2:#a3abbb;--a-steel3:#7d8698;--a-leaf:#46b987;--a-leaf2:#2f9a6c;--a-skyp:color-mix(in srgb,var(--a-sky) 36%,#fff);--a-steelp:color-mix(in srgb,var(--a-steel) 50%,#fff);--a-accp:color-mix(in srgb,var(--a-acc) 22%,#fff);--a-tongue:#ee7a87;--a-wood:#d9a46c;--a-wood2:#b9824c}',
    '.tk-art .st{fill:none;stroke-linecap:round;stroke-linejoin:round}',
    '.tk-art .sh{fill:#16142e;fill-opacity:.15}.tk-art .hl{fill:#fff;fill-opacity:.42}.tk-art .none{fill:none}',
  ].join('') + ['acc', 'acc2', 'acc3', 'soft', 'ink', 'panel', 'line', 'bg', 'on', 'pants', 'skin', 'skin2', 'hair', 'hair2', 'warm', 'warm2', 'good', 'bad',
    'sun', 'sun2', 'sky', 'sky2', 'white', 'dark', 'dark2', 'steel', 'steel2', 'steel3', 'leaf', 'leaf2', 'skyp', 'steelp', 'accp', 'wood', 'wood2', 'tongue'].map(function (c) {
    return '.tk-art .f-' + c + '{fill:var(--a-' + c + ')}.tk-art .s-' + c + '{stroke:var(--a-' + c + ')}';
  }).join('');

  var defs = {};
  var count = 0;

  function style() {
    if (document.getElementById(STYLE_ID)) return;
    var el = document.createElement('style');
    el.id = STYLE_ID;
    el.textContent = CSS;
    (document.head || document.documentElement).appendChild(el);
  }

  // define(name, {w, h, body, rig?, fit?}): body is the SVG inside; `@id@` in it becomes an id of its own to this drawing.
  function define(name, o) {
    if (!o || !o.body || !o.w || !o.h) throw new Error('tk.art.define(' + name + ') needs {w, h, body}');
    defs[name] = o;
  }

  function fill(el, name) {
    var d = defs[name];
    if (!d) throw new Error('tk.art: no drawing "' + name + '" (load the pack: heroes.js, props.js or backdrops.js). Known: ' + Object.keys(defs).join(', '));
    style();
    count += 1;
    var svg = el;
    if (String(el.tagName).toLowerCase() !== 'svg') {
      svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
      el.appendChild(svg);
    }
    svg.setAttribute('viewBox', '0 0 ' + d.w + ' ' + d.h);
    svg.setAttribute('preserveAspectRatio', d.fit || 'xMidYMax meet');
    svg.setAttribute('aria-hidden', 'true');
    svg.setAttribute('focusable', 'false');
    svg.setAttribute('data-tk-art', name);
    if (d.rig) svg.setAttribute('data-rig', JSON.stringify(d.rig));
    var cls = (svg.getAttribute('class') || '').split(/\s+/).filter(Boolean);
    if (cls.indexOf('tk-art') < 0) cls.push('tk-art');
    svg.setAttribute('class', cls.join(' '));
    svg.innerHTML = d.body.replace(/@id@/g, 'a' + count);
    svg.setAttribute('data-tk-mounted', '1');
    return svg;
  }

  function art(name, into, o) {
    var host = typeof into === 'string' ? document.querySelector(into) : into;
    if (!host) throw new Error('tk.art: no container ' + into);
    var svg = fill(host, name);
    if (o && o.class) svg.setAttribute('class', svg.getAttribute('class') + ' ' + o.class);
    return svg;
  }

  // mount: fill every [data-tk-art] under root that is still empty.
  function mount(root) {
    var scope = root || document;
    var list = scope.querySelectorAll('[data-tk-art]:not([data-tk-mounted])');
    for (var i = 0; i < list.length; i++) fill(list[i], list[i].getAttribute('data-tk-art'));
    return list.length;
  }

  art.define = define;
  art.mount = mount;
  art.list = function () { return Object.keys(defs); };
  art.has = function (name) { return !!defs[name]; };
  art.size = function (name) { return defs[name] ? { w: defs[name].w, h: defs[name].h } : null; };
  tk.art = art;

  // The runtime builds first of all builders what was put on the board as <svg data-tk-art>.
  if (window.tkVideo && window.tkVideo.motion) window.tkVideo.motion(function (root) { mount(root); });
})();
