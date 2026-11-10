// The scene script of a video. The library server puts it into every board of a video project, before the
// board's engine, so the board runs on the scene's clock instead of the wall clock and the same t always draws the same
// frame: in the live player, in the render and in the check.
//
// Time t is ms from the scene's start.
//   - data-build-in="fade|rise|pop" with data-tk-at / data-tk-out (seconds from the scene's start) is drawn as a pure
//     function of t: inline opacity, translate and scale on every seek, new nodes caught by a MutationObserver.
//   - Every other animation (the board's @keyframes, transitions, element.animate) is paused and set to currentTime = t,
//     on the document and in open shadow roots; a CSS transition is finished at once.
//   - performance.now, Date.now / new Date(), requestAnimationFrame give the scene's time; Math.random is seeded.
//     The library's own client.js keeps the wall clock.
//   - The scene's overlay layer (titles, callouts, highlights, key caps, counters, badges: scene({overlays}), from
//     meta/scenes/<id>.json) is drawn over the board as a pure function of t, in the live player and in the render.
//   - The scene comes in with a fade from --deck-bg (data-transition on <main>, or what the host sets).
//
// Player (window.parent) ↔ scene, by postMessage:
//   host → scene  {type:'tk:video-seek', t}  {type:'tk:video-play', t, rate}  {type:'tk:video-pause'}
//                 {type:'tk:video-hit', id, x, y}  {type:'tk:video-scene', transition:{kind, ms}|null, overlays:[...]}
//   scene → host  {type:'tk:video-ready', moments:[{block, at, out|null}]}  {type:'tk:video-time', t} (≤ 4 a second)
//                 {type:'tk:video-hit', id, block|null, rect:[x, y, w, h]|null}  {type:'tk:video-error', msg}
// A message is taken only from window.parent.
//
// Headless (the render and the check, through the DevTools protocol):
//   window.tkVideo = {v: 3, ready(): Promise, seek(ms): Promise<{same}>, audit(opts?): Promise<issue[]>, moments(),
//                     scene({transition, overlays}), problems(): problem[], info(): {engines, motionDur}}
//
// The board's motion engines (HRN-576). All of them hang on one time:
//   tkVideo.motion(build)  build(root) runs once the board is drawn and the fonts and images are in. With GSAP on the page
//                          every tween and timeline it makes is gathered into one paused timeline and sought with t.
//   tkVideo.register({name, ready?(), seek(ms)})  any other engine (a canvas, three.js, lottie): ready may return a promise
//                          and the scene waits for it; seek draws time ms and may return a promise. seek returning false
//                          says that nothing changed. It must draw from the time alone, not from the previous frame.
(function () {
  'use strict';
  if (window.tkVideo) return;

  var BUILD_MS = 500;
  var OUT_MS = 300;
  var TRANSITION_MS = 600;
  var PERF_BASE = 10000;
  var EPOCH = Date.UTC(2026, 0, 1);
  var TIME_EVERY = 250;
  var LIB = '/__lib/client.js';

  var RealDate = Date;
  var realNow = Date.now.bind(Date);
  var realPerf = performance.now.bind(performance);
  var realRaf = window.requestAnimationFrame.bind(window);
  var realCaf = window.cancelAnimationFrame.bind(window);

  // The deck's fonts: nothing is installed on a render machine. A @font-face costs nothing until text uses it.
  (function () {
    var link = document.createElement('link');
    link.rel = 'stylesheet';
    link.href = '/__lib/motion/fonts.css';
    (document.head || document.documentElement).appendChild(link);
  })();

  var vt = 0;
  var playing = null; // {t0, r0, rate}
  var transition = null; // {kind, ms}; null: the board's own
  var engaged = false;

  // ---- the clock ----

  // fromLib: the library's own script asks (its timers and marks keep the wall clock).
  function fromLib() {
    var s = new Error().stack;
    return typeof s === 'string' && s.indexOf(LIB) >= 0;
  }

  performance.now = function () { return PERF_BASE + vt; };

  function FakeDate(a, b, c, d, e, f, g) {
    var n = arguments.length;
    if (!(this instanceof FakeDate)) return new RealDate(fromLib() ? realNow() : EPOCH + vt).toString();
    if (n === 0) return new RealDate(fromLib() ? realNow() : EPOCH + vt);
    if (n === 1) return new RealDate(a);
    return new RealDate(a, b, c === undefined ? 1 : c, d || 0, e || 0, f || 0, g || 0);
  }
  FakeDate.prototype = RealDate.prototype;
  FakeDate.now = function () { return fromLib() ? realNow() : EPOCH + vt; };
  FakeDate.parse = RealDate.parse;
  FakeDate.UTC = RealDate.UTC;
  window.Date = FakeDate;

  // Math.random: mulberry32 from a fixed seed.
  var seed = 0x7c3a91d5;
  Math.random = function () {
    seed = (seed + 0x6d2b79f5) | 0;
    var r = Math.imul(seed ^ (seed >>> 15), 1 | seed);
    r = (r + Math.imul(r ^ (r >>> 7), 61 | r)) ^ r;
    return ((r ^ (r >>> 14)) >>> 0) / 4294967296;
  };

  // requestAnimationFrame: the scene's callbacks wait for the next seek (one round each) or the next played frame.
  var rafQueue = [];
  var rafNext = 1e9;
  window.requestAnimationFrame = function (cb) {
    if (fromLib()) return realRaf(cb);
    rafNext += 1;
    rafQueue.push({ id: rafNext, cb: cb });
    return rafNext;
  };
  window.cancelAnimationFrame = function (id) {
    if (id > 1e9) {
      rafQueue = rafQueue.filter(function (r) { return r.id !== id; });
      return;
    }
    realCaf(id);
  };
  function flushRaf() {
    var round = rafQueue;
    rafQueue = [];
    var ts = PERF_BASE + vt;
    for (var i = 0; i < round.length; i++) {
      try { round[i].cb(ts); } catch (err) { tellError(err); }
    }
    return round.length;
  }

  // ---- the board's engines ----

  var engines = []; // {name, ready, seek}
  var builders = [];
  var problems = []; // what went wrong: the check reports it
  var settled = false; // the board is drawn and its assets are in: builders run now
  var gsapMaster = null; // {tl, t0}: the paused timeline holding the board's tweens; t0 is the root's time at the build

  function problem(code, msg) {
    if (problems.length >= 50) return;
    problems.push({ code: code, msg: String(msg).slice(0, 400) });
  }

  function register(engine) {
    if (!engine || typeof engine.seek !== 'function') {
      problem('engine', 'tkVideo.register needs {name, seek(ms)}');
      return;
    }
    engines.push({ name: String(engine.name || 'engine'), ready: engine.ready, seek: engine.seek, started: null });
    dirty = true;
  }

  function motion(build) {
    if (typeof build !== 'function') {
      problem('engine', 'tkVideo.motion needs a function');
      return;
    }
    builders.push(build);
    if (settled) runBuilders();
  }

  function runBuilders() {
    if (!builders.length) return;
    var list = builders;
    builders = [];
    var g = window.gsap;
    var t0 = 0;
    if (g) {
      g.defaults({ lazy: false });
      g.ticker.lagSmoothing(0);
      t0 = g.globalTimeline.time();
    }
    var root = document.querySelector('main') || document.body;
    for (var i = 0; i < list.length; i++) {
      try { list[i](root); } catch (err) { problem('engine', 'tkVideo.motion: ' + (err && err.message || err)); tellError(err); }
    }
    if (!g) return;
    // exportRoot takes in every tween made so far, also those made outside a timeline, so a frame depends on t alone.
    var tl = g.exportRoot({ paused: true }, true);
    g.ticker.sleep();
    if (gsapMaster) {
      gsapMaster.more.push(tl);
      return;
    }
    gsapMaster = { tl: tl, t0: t0, more: [], last: -1 };
    engines.push({ name: 'gsap', ready: null, started: null, seek: seekGsap });
  }

  function seekGsap(ms) {
    var m = gsapMaster;
    var at = ms / 1000;
    var end = motionEnd();
    var clamped = Math.min(at, end);
    if (clamped === m.last) return false;
    m.last = clamped;
    m.tl.time(m.t0 + clamped, false);
    for (var i = 0; i < m.more.length; i++) m.more[i].time(m.t0 + clamped, false);
    window.gsap.ticker.sleep(); // rendering wakes it: its frames are not the scene's
    return true;
  }

  // motionEnd: where the board's GSAP ends, in seconds from the scene's start.
  function motionEnd() {
    var m = gsapMaster;
    if (!m) return 0;
    var end = m.tl.duration() - m.t0;
    for (var i = 0; i < m.more.length; i++) end = Math.max(end, m.more[i].duration() - m.t0);
    return Math.max(0, end);
  }

  // Every engine draws ms; true when one of them says it changed something. Each answer is awaited through waits.
  function seekEngines(ms, waits) {
    var changed = false;
    for (var i = 0; i < engines.length; i++) {
      try {
        var r = engines[i].seek(ms);
        if (r && typeof r.then === 'function') {
          changed = true;
          if (waits) waits.push(r.then(null, engineFailed(engines[i])));
        } else if (r !== false) {
          changed = true;
        }
      } catch (err) { engineFailed(engines[i])(err); }
    }
    return changed;
  }

  function engineFailed(engine) {
    return function (err) {
      problem('engine', engine.name + ': ' + (err && err.message || err));
      tellError(err);
    };
  }

  function readyEngines() {
    var waits = [];
    for (var i = 0; i < engines.length; i++) {
      var e = engines[i];
      if (e.started || typeof e.ready !== 'function') continue;
      try { e.started = Promise.resolve(e.ready()).then(null, engineFailed(e)); } catch (err) { engineFailed(e)(err); e.started = Promise.resolve(); }
      waits.push(e.started);
    }
    return Promise.all(waits);
  }

  // ---- build-ins ----

  var BUILD_INS = { fade: true, rise: true, pop: true };
  var base = new WeakMap(); // element → its own inline opacity, translate, scale before ours
  var written = new WeakMap(); // element → what we last wrote

  function secondsAttr(el, name) {
    var raw = el.getAttribute(name);
    if (raw === null || raw.trim() === '') return null;
    var n = Number(raw);
    return isFinite(n) && n >= 0 ? Math.round(n * 1000) : null;
  }

  var TIMED = '[data-build-in],[data-tk-at],[data-tk-out]';

  // source: the element is the engine's template (inside <x-dc>, not drawn yet); the engine compiles it from the DOM, so
  // nothing of ours may land there.
  function source(el) {
    return !!(el.closest('x-dc') && !el.closest('#dc-root'));
  }

  function timed() {
    var all = document.querySelectorAll(TIMED);
    var out = [];
    for (var i = 0; i < all.length; i++) {
      if (!source(all[i])) out.push(all[i]);
    }
    return out;
  }

  function ease(p) { return 1 - Math.pow(1 - p, 3); }

  // look is what an element shows at t: opacity 0..1, a lift in px and a scale.
  function look(el, t) {
    var at = secondsAttr(el, 'data-tk-at');
    var out = secondsAttr(el, 'data-tk-out');
    var kind = el.getAttribute('data-build-in');
    if (!BUILD_INS[kind]) kind = 'fade';
    var start = at === null ? 0 : at;
    var p = t < start ? 0 : Math.min(1, (t - start) / BUILD_MS);
    if (at === null && !el.hasAttribute('data-build-in')) p = 1;
    if (out !== null && t >= out) p = Math.min(p, Math.max(0, 1 - (t - out) / OUT_MS));
    var e = ease(p);
    return {
      opacity: e,
      lift: kind === 'rise' ? (1 - e) * 24 : 0,
      scale: kind === 'pop' ? 0.92 + 0.08 * e : 1,
    };
  }

  function round3(n) { return Math.round(n * 1000) / 1000; }

  // draw writes an element's look; true when anything changed.
  function draw(el, t) {
    var lk = look(el, t);
    if (!base.has(el)) {
      var own = Number(el.style.opacity);
      base.set(el, { opacity: el.style.opacity === '' || !isFinite(own) ? 1 : own });
    }
    var b = base.get(el);
    var opacity = String(round3(b.opacity * lk.opacity));
    var translate = lk.lift ? '0px ' + round3(lk.lift) + 'px' : '';
    var scale = lk.scale !== 1 ? String(round3(lk.scale)) : '';
    var key = opacity + '|' + translate + '|' + scale;
    if (written.get(el) === key && el.style.opacity === opacity) return false;
    written.set(el, key);
    el.style.opacity = opacity;
    el.style.translate = translate;
    el.style.scale = scale;
    return true;
  }

  // ---- the scene's way in ----

  var veil = null;
  function transitionNow() {
    if (transition) return transition;
    var main = document.querySelector('main');
    var kind = main && main.getAttribute('data-transition');
    if (kind === 'fade' || kind === 'push' || kind === 'magic') return { kind: 'fade', ms: TRANSITION_MS };
    return { kind: 'none', ms: 0 };
  }
  function drawVeil(t) {
    var tr = transitionNow();
    var o = tr.kind === 'fade' && tr.ms > 0 ? Math.max(0, 1 - t / tr.ms) : 0;
    if (o <= 0) {
      if (veil && veil.style.opacity !== '0') { veil.style.opacity = '0'; return true; }
      return false;
    }
    if (!veil) {
      veil = document.createElement('div');
      veil.setAttribute('data-tk-video-veil', '');
      veil.setAttribute('aria-hidden', 'true');
      veil.style.cssText = 'position:fixed;inset:0;z-index:2147483646;pointer-events:none;background:var(--deck-bg,#fff)';
      document.documentElement.appendChild(veil);
    }
    var v = String(round3(o));
    if (veil.style.opacity === v) return false;
    veil.style.opacity = v;
    return true;
  }

  // ---- animations ----

  function roots() {
    var out = [document];
    var walker = document.createTreeWalker(document.documentElement, NodeFilter.SHOW_ELEMENT);
    for (var n = walker.currentNode; n; n = walker.nextNode()) {
      if (n.shadowRoot) out.push(n.shadowRoot);
    }
    return out;
  }

  function animations() {
    var seen = new Set();
    var out = [];
    var rs = roots();
    for (var i = 0; i < rs.length; i++) {
      var list = rs[i].getAnimations ? rs[i].getAnimations() : [];
      for (var j = 0; j < list.length; j++) {
        if (!seen.has(list[j])) { seen.add(list[j]); out.push(list[j]); }
      }
    }
    return out;
  }

  function progressOf(a) {
    try {
      var c = a.effect && a.effect.getComputedTiming();
      return c ? c.progress + ':' + c.currentIteration : '';
    } catch (err) { return ''; }
  }

  // seekAnimations sets every animation to t; true when any of them shows something else now.
  function seekAnimations(t) {
    var changed = false;
    var list = animations();
    for (var i = 0; i < list.length; i++) {
      var a = list[i];
      var before = progressOf(a);
      try {
        if (typeof CSSTransition !== 'undefined' && a instanceof CSSTransition) {
          a.finish();
        } else {
          if (a.playState !== 'paused') a.pause();
          a.currentTime = t;
        }
      } catch (err) { tellError(err); }
      if (progressOf(a) !== before) changed = true;
    }
    return changed;
  }

  // ---- the overlay layer (HRN-653) ----
  //
  // Titles, lower thirds, callouts, highlights, key caps, counters and badges that the host or the render hands over in
  // the scene's config ({overlays: [...]}, internal/design/video_overlays.go). They are drawn over the board as a pure
  // function of t: nothing remembers the previous frame, the hash for the scramble is of (char, time step), so a seek
  // draws the same frame as playing through. Positions are fractions of the frame; the 9:16 frame uses an overlay's own
  // portrait place, else the base place moved into the safe zone (placeFor, the same rule as VideoOverlayPlaceIn in Go).

  var OV_OUT_MS = 320;
  var OV_COUNT_MS = 1400;
  var OV_STYLE = [
    '[data-tk-overlays]{position:fixed;inset:0;z-index:2147483000;pointer-events:none;overflow:hidden;font-family:var(--deck-font,system-ui,-apple-system,"Segoe UI",sans-serif);}',
    '[data-tk-overlays] *{box-sizing:border-box;}',
    '.tko-g{position:absolute;inset:0;opacity:0;visibility:hidden;}',
    '.tko{position:absolute;}',
    '.tko[data-theme=auto]{--fg:var(--deck-ink,#101114);--bg:var(--deck-panel,#fff);--ac:var(--deck-acc,#3b6cff);}',
    '.tko[data-theme=accent]{--fg:var(--deck-on-acc,#fff);--bg:var(--deck-acc,#3b6cff);--ac:var(--deck-on-acc,#fff);}',
    '.tko[data-theme=dark]{--fg:#f5f6f8;--bg:#15171c;--ac:var(--deck-acc,#3b6cff);}',
    '.tko[data-theme=light]{--fg:#101114;--bg:#fff;--ac:var(--deck-acc,#3b6cff);}',
    '.tko-in{position:absolute;inset:0;display:flex;align-items:center;color:var(--fg);}',
    '.tko[data-anchor=left] .tko-in{justify-content:flex-start;text-align:left;}',
    '.tko[data-anchor=center] .tko-in{justify-content:center;text-align:center;}',
    '.tko[data-anchor=right] .tko-in{justify-content:flex-end;text-align:right;}',
    '.tko-t{display:block;max-width:100%;line-height:1.14;white-space:pre-wrap;overflow-wrap:normal;}',
    '.tko-w,.tko-wd{white-space:nowrap;}',
    '.tko-wd{display:inline-block;}',
    '.tko-c{display:inline-block;}',
    '.tko-c[data-cur]{box-shadow:0.09em 0 0 0 currentColor;}',
    '.tko-plate .tko-in{background:var(--bg);border-radius:0.26em;padding:0 0.55em;box-shadow:0 0.12em 0.5em rgba(0,0,0,.28);}',
    '.tko-title .tko-t{font-weight:800;letter-spacing:-0.02em;}',
    '.tko-title[data-theme=auto] .tko-in{background:none;box-shadow:none;padding:0;text-shadow:0 0.04em 0.4em rgba(0,0,0,.18);}',
    '.tko-caption .tko-in{background:var(--bg);border-radius:0.2em;padding:0 0.7em 0 0.9em;box-shadow:0 0.12em 0.5em rgba(0,0,0,.28);overflow:hidden;}',
    '.tko-caption .tko-in::before{content:"";position:absolute;left:0;top:0;bottom:0;width:0.28em;background:var(--ac);}',
    '.tko-caption .tko-t{font-weight:650;}',
    '.tko-callout .tko-in{background:var(--bg);border-radius:0.3em;padding:0 0.6em;box-shadow:0 0.12em 0.5em rgba(0,0,0,.3);}',
    '.tko-callout .tko-t{font-weight:600;}',
    '.tko-svg{position:absolute;left:0;top:0;overflow:visible;}',
    '.tko-dim{position:absolute;inset:0;border-radius:0.9vmin;box-shadow:0 0 0 200vmax rgba(8,10,14,.45);filter:blur(1.1vmin);}',
    '.tko-ring{position:absolute;inset:0;border-style:solid;border-color:var(--ac);}',
    '.tko-label{position:absolute;left:0;background:var(--bg);color:var(--fg);font-weight:650;padding:0.2em 0.6em;border-radius:0.3em;white-space:nowrap;box-shadow:0 0.1em 0.4em rgba(0,0,0,.3);}',
    '.tko-keys{display:flex;align-items:center;justify-content:center;gap:0.35em;}',
    '.tko-k{background:var(--bg);color:var(--fg);border:0.06em solid rgba(128,128,128,.45);border-bottom-width:0.14em;border-radius:0.28em;padding:0.12em 0.6em;font-weight:650;white-space:nowrap;box-shadow:0 0.1em 0.4em rgba(0,0,0,.25);}',
    '.tko-p{opacity:.7;font-weight:600;}',
    '.tko-counter .tko-t{font-weight:800;font-variant-numeric:tabular-nums;letter-spacing:-0.02em;}',
    '.tko-counter[data-theme=auto] .tko-num{color:var(--ac);}',
    '.tko-lab{font-weight:600;opacity:.82;font-size:0.5em;}',
    '.tko-badge .tko-in{background:var(--bg);border-radius:0.4em;padding:0 0.6em;box-shadow:0 0.14em 0.5em rgba(0,0,0,.35);transform:rotate(-3deg);}',
    '.tko-badge .tko-t{font-weight:800;}',
  ].join('\n');

  var ov = { list: [], items: [], root: null, key: '', styleEl: null, W: 0, H: 0 };

  function clamp01(v) { return v < 0 ? 0 : v > 1 ? 1 : v; }
  function easeOut(p) { return 1 - Math.pow(1 - p, 3); }
  function easeBack(p) { var c = 1.70158; var q = p - 1; return 1 + (c + 1) * q * q * q + c * q * q; }

  function ovHash(i, k) {
    var x = (Math.imul(i + 1, 374761393) + Math.imul(k + 1, 668265263)) | 0;
    x = Math.imul(x ^ (x >>> 13), 1274126177);
    return ((x ^ (x >>> 16)) >>> 0);
  }

  // The safe zone of a format: margins as fractions. 9:16 keeps its bottom clear of the players' own buttons.
  function safeZone(portrait) {
    return portrait ? { l: 0.06, t: 0.08, r: 0.06, b: 0.17 } : { l: 0.04, t: 0.06, r: 0.04, b: 0.06 };
  }

  // placeFor: where the overlay stands in this frame. Equals VideoOverlayPlaceIn in Go.
  function placeFor(o, portrait) {
    var base = { x: o.x, y: o.y, w: o.w, h: o.h, tx: o.tx, ty: o.ty };
    if (!portrait) return base;
    if (o.portrait) return o.portrait;
    if (o.kind === 'highlight') return base;
    var z = safeZone(true);
    var w = Math.min(base.w, 1 - z.l - z.r);
    var h = Math.min(base.h, 1 - z.t - z.b);
    base.w = w;
    base.h = h;
    base.x = Math.min(Math.max(base.x, z.l), 1 - z.r - w);
    base.y = Math.min(Math.max(base.y, z.t), 1 - z.b - h);
    return base;
  }

  function fmtNum(n, decimals) {
    var neg = n < 0;
    var s = Math.abs(n).toFixed(decimals);
    var parts = s.split('.');
    var ip = parts[0].replace(/\B(?=(\d{3})+(?!\d))/g, ' ');
    return (neg ? '−' : '') + ip + (parts.length > 1 ? ',' + parts[1] : '');
  }

  function el(tag, cls, text) {
    var e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text !== undefined) e.textContent = text;
    return e;
  }

  function wordsOf(text) { return text.split(/(\s+)/); }

  function enterMs(o) {
    var len = Array.from(o.text || '').length;
    switch (o.enter) {
      case 'rise': return 620;
      case 'pop': return 460;
      case 'mask': return 720;
      case 'typewriter': return Math.min(2000, Math.max(500, len * 45));
      case 'words': return Math.min(1800, Math.max(600, (o.text || '').split(/\s+/).length * 110 + 350));
      case 'scramble': return Math.min(1600, Math.max(700, len * 35 + 400));
    }
    return 460;
  }

  function leaveMs(o) { return o.leave === 'fall' ? 420 : o.leave === 'shrink' ? 360 : o.leave === 'fade' ? OV_OUT_MS : 0; }

  // The text of an overlay as nodes inside `t`: plain, or split into words / characters for the styles that act on them.
  function fillText(t, o, item) {
    var text = o.text || '';
    item.chars = [];
    item.words = [];
    if (o.kind === 'counter') return;
    if (o.enter === 'typewriter' || o.enter === 'scramble') {
      var parts = wordsOf(text);
      for (var i = 0; i < parts.length; i++) {
        if (parts[i] === '') continue;
        if (/^\s+$/.test(parts[i])) { t.appendChild(document.createTextNode(parts[i])); continue; }
        var w = el('span', 'tko-w');
        var cs = Array.from(parts[i]);
        for (var j = 0; j < cs.length; j++) {
          var c = el('span', 'tko-c', cs[j]);
          c.setAttribute('data-ch', cs[j]);
          w.appendChild(c);
          item.chars.push(c);
        }
        t.appendChild(w);
      }
      return;
    }
    if (o.enter === 'words') {
      var ps = wordsOf(text);
      for (var k = 0; k < ps.length; k++) {
        if (ps[k] === '') continue;
        if (/^\s+$/.test(ps[k])) { t.appendChild(document.createTextNode(ps[k])); continue; }
        var wd = el('span', 'tko-wd', ps[k]);
        t.appendChild(wd);
        item.words.push(wd);
      }
      return;
    }
    t.textContent = text;
  }

  function buildItem(o) {
    var item = { o: o, g: el('div', 'tko-g'), last: '', chars: [], words: [] };
    var box = el('div', 'tko tko-' + o.kind);
    box.setAttribute('data-theme', o.theme || 'auto');
    box.setAttribute('data-anchor', o.anchor || 'center');
    if (o.kind === 'badge' || o.kind === 'counter' && o.theme !== 'auto') box.classList.add('tko-plate');
    var inner = el('div', 'tko-in');
    box.appendChild(inner);
    item.box = box;
    item.inner = inner;
    item.g.appendChild(box);
    if (o.kind === 'highlight') {
      inner.style.display = 'block';
      var ring = el('div', 'tko-ring');
      item.ring = ring;
      inner.appendChild(el('div', 'tko-dim'));
      inner.appendChild(ring);
      if ((o.text || '').trim() !== '') {
        item.label = el('div', 'tko-label', o.text);
        box.appendChild(item.label);
      }
      return item;
    }
    if (o.kind === 'key') {
      var keys = el('div', 'tko-keys');
      var bits = (o.text || '').split('+');
      item.keys = [];
      for (var i = 0; i < bits.length; i++) {
        if (i > 0) keys.appendChild(el('span', 'tko-p', '+'));
        var k = el('span', 'tko-k', bits[i].trim());
        keys.appendChild(k);
        item.keys.push(k);
      }
      inner.appendChild(keys);
      item.text = keys;
      return item;
    }
    var t = el('span', 'tko-t');
    if (o.kind === 'counter') {
      item.num = el('span', 'tko-num', '');
      item.lab = null;
      var tpl = o.text || '';
      var at = tpl.indexOf('{n}');
      if (at >= 0) {
        if (at > 0) t.appendChild(el('span', 'tko-lab', tpl.slice(0, at)));
        t.appendChild(item.num);
        if (at + 3 < tpl.length) t.appendChild(el('span', 'tko-lab', tpl.slice(at + 3)));
      } else {
        t.appendChild(item.num);
        if (tpl.trim() !== '') {
          t.appendChild(document.createTextNode(' '));
          t.appendChild(el('span', 'tko-lab', tpl));
        }
      }
    } else {
      fillText(t, o, item);
    }
    inner.appendChild(t);
    item.text = t;
    if (o.kind === 'callout' && o.tx !== undefined && o.tx !== null) {
      var ns = 'http://www.w3.org/2000/svg';
      var svg = document.createElementNS(ns, 'svg');
      svg.setAttribute('class', 'tko-svg');
      var path = document.createElementNS(ns, 'path');
      path.setAttribute('fill', 'none');
      path.setAttribute('stroke-linecap', 'round');
      var head = document.createElementNS(ns, 'path');
      var ring2 = document.createElementNS(ns, 'circle');
      ring2.setAttribute('fill', 'none');
      var dot = document.createElementNS(ns, 'circle');
      svg.appendChild(path); svg.appendChild(head); svg.appendChild(ring2); svg.appendChild(dot);
      item.svg = svg; item.path = path; item.head = head; item.ring2 = ring2; item.dot = dot;
      item.g.appendChild(svg);
    }
    return item;
  }

  // fitText: the largest font (px) the text takes inside its box, starting from a size of the box's height. offsetWidth and
  // offsetHeight are layout sizes: a transform on the way (a pop in progress) does not change them.
  function fitText(item, boxH) {
    var o = item.o;
    var f = { title: 0.62, caption: 0.5, callout: 0.4, key: 0.5, counter: 0.72, badge: 0.52 }[o.kind] || 0.5;
    var size = Math.max(14, boxH * f);
    var target = item.text;
    if (!target) return size;
    var inn = item.inner;
    var guard = 0;
    item.box.style.fontSize = size + 'px';
    while (guard < 40 && size > 14) {
      var padL = parseFloat(getComputedStyle(inn).paddingLeft) + parseFloat(getComputedStyle(inn).paddingRight);
      var over = target.offsetHeight > inn.clientHeight + 1 || target.scrollWidth > inn.clientWidth - padL + 1;
      if (!over) break;
      size = size * 0.93;
      item.box.style.fontSize = size + 'px';
      guard++;
    }
    return size;
  }

  // Text on a plate keeps a contrast of at least 4.5:1: when the theme's text color fails on its plate, the text takes
  // white or near-black, whichever reads better. Reads computed colors only, so a frame stays a function of t.
  function rgbOf(c) {
    var m = /rgba?\(([^)]+)\)/.exec(c || '');
    if (!m) return null;
    var v = m[1].split(/[ ,\/]+/).map(parseFloat);
    return v.length >= 3 && !isNaN(v[0]) ? v : null;
  }
  function lum(v) {
    var f = function (x) { x /= 255; return x <= 0.03928 ? x / 12.92 : Math.pow((x + 0.055) / 1.055, 2.4); };
    return 0.2126 * f(v[0]) + 0.7152 * f(v[1]) + 0.0722 * f(v[2]);
  }
  function ratio(a, b) { var x = lum(a), y = lum(b); return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05); }
  function ensureContrast(item) {
    var o = item.o;
    if (o.kind === 'highlight' || o.kind === 'title' && o.theme === 'auto') return;
    var cs = getComputedStyle(item.inner);
    var bg = rgbOf(cs.backgroundColor);
    var fg = rgbOf(cs.color);
    if (!bg || !fg || (bg.length > 3 && bg[3] === 0)) return;
    if (ratio(bg, fg) >= 4.5) return;
    var white = [255, 255, 255], ink = [11, 13, 18];
    item.box.style.setProperty('--fg', ratio(bg, white) >= ratio(bg, ink) ? '#fff' : '#0b0d12');
  }

  function layoutItem(item, W, H, portrait) {
    var o = item.o;
    var p = placeFor(o, portrait);
    var bx = p.x * W, by = p.y * H, bw = p.w * W, bh = p.h * H;
    item.rect = [bx, by, bw, bh];
    var s = item.box.style;
    s.left = bx + 'px'; s.top = by + 'px'; s.width = bw + 'px'; s.height = bh + 'px';
    item.g.style.transformOrigin = (bx + bw / 2) + 'px ' + (by + bh / 2) + 'px';
    item.px = Math.max(2, H * 0.0045);
    if (o.kind === 'highlight') {
      var r = Math.max(8, H * 0.012);
      item.ring.style.borderWidth = Math.max(3, H * 0.0055) + 'px';
      item.ring.style.borderRadius = r + 'px';
      if (item.label) {
        var fs = Math.max(18, H * 0.026);
        item.label.style.fontSize = fs + 'px';
        if (by > H * 0.1) item.label.style.top = (-fs * 1.9) + 'px';
        else item.label.style.top = (bh + fs * 0.5) + 'px';
      }
    } else {
      if (o.kind === 'counter') {
        var tmp = fmtNum(o.value === undefined || o.value === null ? 0 : o.value, o.decimals || 0);
        var from = fmtNum(o.from || 0, o.decimals || 0);
        item.num.textContent = tmp.length >= from.length ? tmp : from;
      }
      ensureContrast(item);
      var size = fitText(item, bh);
      item.size = size;
      if (item.chars.length && o.enter === 'scramble') {
        for (var i = 0; i < item.chars.length; i++) {
          var c = item.chars[i];
          c.style.width = '';
          var cw = c.getBoundingClientRect().width;
          c.style.width = cw + 'px';
          c.style.textAlign = 'center';
        }
      }
    }
    if (item.svg) {
      var tx = p.tx === undefined || p.tx === null ? 0.5 : p.tx;
      var ty = p.ty === undefined || p.ty === null ? 0.5 : p.ty;
      item.svg.setAttribute('width', W);
      item.svg.setAttribute('height', H);
      item.svg.setAttribute('viewBox', '0 0 ' + W + ' ' + H);
      var cx = bx + bw / 2, cy = by + bh / 2;
      var ex = tx * W, ey = ty * H;
      // The arrow leaves the box at the point of its edge that faces the target.
      var dx = ex - cx, dy = ey - cy;
      var sx = Math.abs(dx) / (bw / 2 || 1), sy = Math.abs(dy) / (bh / 2 || 1);
      var k = Math.max(sx, sy) || 1;
      var ax = cx + dx / k, ay = cy + dy / k;
      var len = Math.hypot(ex - ax, ey - ay);
      var bend = Math.min(len * 0.18, H * 0.08);
      var mx = (ax + ex) / 2 + (-(ey - ay) / (len || 1)) * bend;
      var my = (ay + ey) / 2 + ((ex - ax) / (len || 1)) * bend;
      // The head sits at the end of the curve, turned along its last stretch.
      var ang = Math.atan2(ey - my, ex - mx);
      var hs = Math.max(14, H * 0.028);
      item.stroke = Math.max(3, H * 0.0055);
      item.cur = { ax: ax, ay: ay, mx: mx, my: my, ex: ex, ey: ey, hs: hs, ang: ang };
      item.path.setAttribute('d', 'M' + ax + ' ' + ay + ' Q' + mx + ' ' + my + ' ' + ex + ' ' + ey);
      item.path.setAttribute('stroke-width', item.stroke);
      item.pathLen = 0;
      try { item.pathLen = item.path.getTotalLength(); } catch (err) { item.pathLen = len * 1.05; }
      var a1 = ang + Math.PI * 0.86, a2 = ang - Math.PI * 0.86;
      item.head.setAttribute('d', 'M' + ex + ' ' + ey + ' L' + (ex + Math.cos(a1) * hs) + ' ' + (ey + Math.sin(a1) * hs) + ' L' + (ex + Math.cos(a2) * hs) + ' ' + (ey + Math.sin(a2) * hs) + ' Z');
      item.ring2.setAttribute('cx', ex); item.ring2.setAttribute('cy', ey);
      item.ring2.setAttribute('stroke-width', Math.max(2, H * 0.0035));
      item.dot.setAttribute('cx', ex); item.dot.setAttribute('cy', ey);
      item.dot.setAttribute('r', Math.max(5, H * 0.009));
      // The arrow is the plate's color on an accent callout, else the accent.
      var cs = getComputedStyle(item.box);
      var sc = (o.theme === 'accent' ? cs.getPropertyValue('--bg') : cs.getPropertyValue('--ac')).trim() || '#3b6cff';
      item.path.setAttribute('stroke', sc);
      item.head.setAttribute('fill', sc);
      item.ring2.setAttribute('stroke', sc);
      item.dot.setAttribute('fill', sc);
    }
    item.last = '';
  }

  function ovPortrait() { return window.innerHeight > window.innerWidth; }

  function layoutOverlays() {
    if (!ov.root) return;
    var W = window.innerWidth, H = window.innerHeight;
    ov.W = W; ov.H = H;
    var portrait = ovPortrait();
    for (var i = 0; i < ov.items.length; i++) layoutItem(ov.items[i], W, H, portrait);
    dirty = true;
  }

  function setOverlays(list) {
    var key = JSON.stringify(list);
    if (key === ov.key) return;
    ov.key = key;
    if (!ov.root) {
      if (!list.length) return;
      ov.root = el('div', '');
      ov.root.setAttribute('data-tk-overlays', '');
      ov.root.setAttribute('aria-hidden', 'true');
      ov.styleEl = el('style', '', OV_STYLE);
      ov.root.appendChild(ov.styleEl);
      document.documentElement.appendChild(ov.root);
      window.addEventListener('resize', layoutOverlays);
      if (document.fonts && document.fonts.addEventListener) document.fonts.addEventListener('loadingdone', layoutOverlays);
    }
    for (var j = ov.items.length - 1; j >= 0; j--) ov.root.removeChild(ov.items[j].g);
    ov.items = [];
    ov.list = list;
    // A highlight dims what lies under it: it goes to the bottom of the layer, so no other overlay is dimmed.
    var order = list.filter(function (o) { return o && o.kind === 'highlight'; })
      .concat(list.filter(function (o) { return o && typeof o === 'object' && typeof o.kind === 'string' && o.kind !== 'highlight'; }));
    for (var i = 0; i < order.length; i++) {
      var item = buildItem(order[i]);
      ov.items.push(item);
      ov.root.appendChild(item.g);
    }
    layoutOverlays();
  }

  // style writes: only what changed since the last frame of this item.
  function drawItem(item, t) {
    var o = item.o;
    var at = o.at || 0;
    var inMs = enterMs(o);
    var outMs = o.out === null || o.out === undefined ? null : o.out;
    var lv = leaveMs(o);
    var H = ov.H || window.innerHeight;
    var visible = t >= at && (outMs === null || t < outMs + lv && (lv > 0 || t < outMs));
    if (!visible) {
      var hidden = 'hidden';
      if (item.last === hidden) return false;
      item.last = hidden;
      item.g.style.visibility = 'hidden';
      item.g.style.opacity = '0';
      return true;
    }
    var p = clamp01((t - at) / inMs);
    var q = outMs !== null && t >= outMs && lv > 0 ? clamp01((t - outMs) / lv) : 0;
    var opacity = 1, ty = 0, scale = 1, clip = '';
    var textEffect = o.enter === 'typewriter' || o.enter === 'words' || o.enter === 'scramble';
    var e = easeOut(p);
    switch (o.enter) {
      case 'rise': opacity = e; ty = (1 - e) * H * 0.045; break;
      case 'pop': opacity = clamp01(p * 3); scale = 0.82 + 0.18 * easeBack(p); break;
      case 'mask': opacity = p > 0 ? 1 : 0; clip = 'inset(0 ' + ((1 - e) * 100).toFixed(2) + '% 0 0)'; break;
      case 'typewriter': case 'words': case 'scramble': opacity = clamp01(p * 8); break;
      default: opacity = e;
    }
    if (o.kind === 'highlight' && o.enter !== 'fade') {
      // The area is not text: the text styles play as a fade.
      if (textEffect || o.enter === 'mask') { opacity = e; clip = ''; }
    }
    if (q > 0) {
      var eq = q * q;
      switch (o.leave) {
        case 'fall': opacity *= 1 - eq; ty += eq * H * 0.05; break;
        case 'shrink': opacity *= 1 - eq; scale *= 1 - 0.18 * eq; break;
        default: opacity *= 1 - q;
      }
    }
    // text and number state
    var detail = '';
    if (item.chars.length && o.enter === 'typewriter') {
      var shown = Math.floor(p * item.chars.length + (p >= 1 ? 1 : 0));
      var live = p < 1 || t - at - inMs < 700;
      var cur = live && Math.floor(t / 400) % 2 === 0 ? Math.min(shown, item.chars.length) - 1 : -2;
      detail = shown + ':' + cur;
      if (detail !== item.detail) {
        for (var i = 0; i < item.chars.length; i++) {
          item.chars[i].style.opacity = i < shown ? '1' : '0';
          if (i === cur) item.chars[i].setAttribute('data-cur', ''); else item.chars[i].removeAttribute('data-cur');
        }
      }
    } else if (item.chars.length && o.enter === 'scramble') {
      var n = item.chars.length;
      var step = Math.floor(t / 55);
      var done = p >= 1 ? n : Math.floor(p * (n + 4)) - 3;
      detail = done + ':' + (done < n ? step : 0);
      if (detail !== item.detail) {
        for (var s = 0; s < n; s++) {
          var c = item.chars[s];
          var real = c.getAttribute('data-ch');
          if (s < done || p >= 1) { if (c.textContent !== real) c.textContent = real; c.style.opacity = '1'; continue; }
          if (p <= 0) { c.style.opacity = '0'; continue; }
          var pool = /[Ѐ-ӿ]/.test(real) ? 'АБВГДЕЖЗИКЛМНОПРСТУФХЦЧШЭЮЯ' : 'ABCDEFGHJKLMNPQRSTUVWXYZ0123456789#%&*+=?';
          c.textContent = pool.charAt(ovHash(s, step) % pool.length);
          c.style.opacity = '0.85';
        }
      }
    } else if (item.words.length && o.enter === 'words') {
      var nw = item.words.length;
      var wDur = Math.min(0.6, 2 / Math.max(2, nw));
      var gap = nw > 1 ? (1 - wDur) / (nw - 1) : 0;
      detail = String(Math.round(p * 1000));
      if (detail !== item.detail) {
        for (var w = 0; w < nw; w++) {
          var wp = easeOut(clamp01((p - w * gap) / wDur));
          item.words[w].style.opacity = wp.toFixed(3);
          item.words[w].style.transform = 'translateY(' + ((1 - wp) * 0.5).toFixed(3) + 'em)';
        }
      }
    } else if (item.num) {
      var cp = easeOut(clamp01((t - at) / OV_COUNT_MS));
      var from = o.from || 0;
      var val = o.value === undefined || o.value === null ? 0 : o.value;
      var now = from + (val - from) * cp;
      var d = o.decimals || 0;
      var txt = fmtNum(Math.round(now * Math.pow(10, d)) / Math.pow(10, d), d);
      detail = txt;
      if (txt !== item.num.textContent) item.num.textContent = txt;
    } else if (item.keys) {
      // The caps are pressed once they are in: a short dip.
      var pt = t - at - inMs - 250;
      var press = pt > 0 && pt < 220 ? Math.sin((pt / 220) * Math.PI) : 0;
      detail = press.toFixed(2);
      if (detail !== item.detail) {
        for (var kk = 0; kk < item.keys.length; kk++) item.keys[kk].style.transform = press > 0 ? 'translateY(' + (press * 0.07).toFixed(3) + 'em)' : '';
      }
    }
    var arrow = '';
    if (item.svg) {
      var ap = clamp01((p - 0.2) / 0.7);
      var phase = ((t - at) % 1300) / 1300;
      if (t - at < 0) phase = 0;
      var ringR = Math.max(10, H * 0.02) * (1 + phase * 1.1);
      var ringA = (1 - phase) * 0.8 * clamp01(p * 2);
      arrow = ap.toFixed(3) + ':' + phase.toFixed(3);
      if (arrow !== item.arrow) {
        var L = item.pathLen || 1;
        item.path.setAttribute('stroke-dasharray', L);
        item.path.setAttribute('stroke-dashoffset', (L * (1 - easeOut(ap))).toFixed(2));
        item.head.setAttribute('opacity', ap > 0.92 ? '1' : '0');
        item.dot.setAttribute('opacity', clamp01(p * 3).toFixed(3));
        item.ring2.setAttribute('r', ringR.toFixed(2));
        item.ring2.setAttribute('opacity', ap > 0.5 ? ringA.toFixed(3) : '0');
      }
    }
    var key = [opacity.toFixed(3), ty.toFixed(1), scale.toFixed(3), clip, detail, arrow].join('|');
    if (key === item.last) return false;
    item.last = key;
    item.detail = detail;
    item.arrow = arrow;
    var g = item.g.style;
    g.visibility = opacity > 0.001 ? 'visible' : 'hidden';
    g.opacity = opacity.toFixed(3);
    g.transform = ty !== 0 || scale !== 1 ? 'translateY(' + ty.toFixed(1) + 'px) scale(' + scale.toFixed(4) + ')' : '';
    item.inner.style.clipPath = clip;
    return true;
  }

  function drawOverlays(t) {
    var changed = false;
    for (var i = 0; i < ov.items.length; i++) {
      if (drawItem(ov.items[i], t)) changed = true;
    }
    return changed;
  }

  // ---- drawing at t ----

  var observer = null;
  var dirty = true;

  function apply(t) {
    if (observer && observer.takeRecords().length) dirty = true;
    var changed = dirty;
    dirty = false;
    var els = timed();
    for (var i = 0; i < els.length; i++) {
      if (draw(els[i], t)) changed = true;
    }
    if (drawVeil(t)) changed = true;
    if (drawOverlays(t)) changed = true;
    if (seekAnimations(t)) changed = true;
    if (observer) observer.takeRecords(); // our own writes
    return changed;
  }

  function watch() {
    if (observer || !window.MutationObserver) return;
    observer = new MutationObserver(function (records) {
      dirty = true;
      for (var i = 0; i < records.length; i++) {
        var added = records[i].addedNodes;
        for (var j = 0; j < added.length; j++) {
          var n = added[j];
          if (n.nodeType !== 1) continue;
          if (n.matches && n.matches(TIMED) && !source(n)) draw(n, vt);
          var inner = n.querySelectorAll ? n.querySelectorAll(TIMED) : [];
          for (var k = 0; k < inner.length; k++) {
            if (!source(inner[k])) draw(inner[k], vt);
          }
        }
      }
      observer.takeRecords();
    });
    observer.observe(document.documentElement, { subtree: true, childList: true, characterData: true, attributes: true });
  }

  // A macrotask that a hidden tab does not slow down.
  function yieldTask() {
    return new Promise(function (resolve) {
      var ch = new MessageChannel();
      ch.port1.onmessage = function () { resolve(); };
      ch.port2.postMessage(0);
    });
  }

  function engage() {
    if (engaged) return;
    engaged = true;
    watch();
  }

  function seekTo(t) {
    engage();
    vt = Math.max(0, Math.round(t));
    var ran = flushRaf();
    var waits = [];
    var drew = seekEngines(vt, waits);
    var changed = apply(vt) || drew;
    // What the callbacks and the engine's renders do lands a task later.
    return Promise.all(waits).then(yieldTask).then(yieldTask).then(function () {
      if (apply(vt)) changed = true;
      return { same: !changed && ran === 0 };
    });
  }

  // ---- moments, hit, audit ----

  function blockOf(el) {
    var b = el && el.closest ? el.closest('[data-block]') : null;
    return b ? b.getAttribute('data-block') : null;
  }

  function moments() {
    var out = [];
    var els = document.querySelectorAll('[data-tk-at],[data-tk-mark]');
    for (var i = 0; i < els.length && out.length < 200; i++) {
      if (source(els[i])) continue;
      var at = secondsAttr(els[i], 'data-tk-at');
      if (at === null) at = secondsAttr(els[i], 'data-tk-mark');
      if (at === null) continue;
      out.push({ block: blockOf(els[i]) || '', at: at, out: secondsAttr(els[i], 'data-tk-out') });
    }
    return out;
  }

  function rectOf(el) {
    var r = el.getBoundingClientRect();
    return [Math.round(r.left), Math.round(r.top), Math.round(r.width), Math.round(r.height)];
  }

  function hit(x, y) {
    var el = document.elementFromPoint(x, y);
    var b = el && el.closest ? el.closest('[data-block]') : null;
    if (!b) return { block: null, rect: null };
    return { block: b.getAttribute('data-block'), rect: rectOf(b) };
  }

  function visible(el) {
    var cs = getComputedStyle(el);
    if (cs.display === 'none' || cs.visibility === 'hidden') return false;
    for (var n = el; n && n.nodeType === 1; n = n.parentElement) {
      if (Number(getComputedStyle(n).opacity) === 0) return false;
    }
    var r = el.getBoundingClientRect();
    return r.width > 0 && r.height > 0;
  }

  function ownText(el) {
    for (var c = el.firstChild; c; c = c.nextSibling) {
      if (c.nodeType === 3 && c.nodeValue.trim() !== '') return true;
    }
    return false;
  }

  // audit lists what is wrong at the current t: text that overflows its box, a block off the screen and, with
  // opts.minFont, text smaller than that (px).
  function audit(opts) {
    var minFont = opts && typeof opts.minFont === 'number' ? opts.minFont : 0;
    var out = [];
    var seen = {};
    function add(code, el, px, detail) {
      var block = blockOf(el);
      var key = code + '|' + block + '|' + (detail || '');
      if (seen[key] || out.length >= 100) return;
      seen[key] = true;
      out.push({ code: code, block: block, px: px, detail: detail || null });
    }
    var W = window.innerWidth;
    var H = window.innerHeight;
    var all = document.body ? document.body.querySelectorAll('*') : [];
    for (var i = 0; i < all.length; i++) {
      var el = all[i];
      if (el.hasAttribute('data-tk-library-bar') || el.closest('[data-tk-library-bar]')) continue;
      var isBlock = el.hasAttribute('data-block');
      var text = ownText(el);
      if (!isBlock && !text) continue;
      if (!visible(el)) continue;
      if (text) {
        if (el.scrollWidth > el.clientWidth + 2 && el.clientWidth > 0 || el.scrollHeight > el.clientHeight + 2 && el.clientHeight > 0) {
          var cs = getComputedStyle(el);
          if (cs.display !== 'inline') add('overflow', el, null);
        }
        var fs = parseFloat(getComputedStyle(el).fontSize);
        if (minFont > 0 && fs < minFont) add('small-text', el, Math.round(fs));
      }
      if (isBlock) {
        var r = el.getBoundingClientRect();
        if (r.left < -2 || r.top < -2 || r.right > W + 2 || r.bottom > H + 2) add('offscreen', el, null);
      }
    }
    motionAudit(add, out);
    return out;
  }

  // motionAudit: two tweens driving one property of one target at once. The later one takes its start value on the first
  // pass, so a frame reached by seeking differs from the same frame played through.
  function motionAudit(add, out) {
    if (!gsapMaster) return;
    var tl = gsapMaster.tl;
    var tweens = tl.getChildren(true, true, false);
    var byKey = new Map();
    var skip = { ease: 1, duration: 1, delay: 1, stagger: 1, overwrite: 1, repeat: 1, repeatDelay: 1, yoyo: 1, onComplete: 1, onUpdate: 1, onStart: 1, immediateRender: 1, lazy: 1, id: 1, startAt: 1, runBackwards: 1, data: 1, paused: 1, transformOrigin: 1, svgOrigin: 1, callbackScope: 1, onUpdateParams: 1, onCompleteParams: 1, onStartParams: 1, parent: 1 };
    for (var i = 0; i < tweens.length && out.length < 100; i++) {
      var tw = tweens[i];
      if (!tw.targets || !tw.duration() || !tw.vars) continue;
      var from = absoluteStart(tw);
      var to = from + tw.totalDuration();
      var targets = tw.targets();
      for (var j = 0; j < targets.length; j++) {
        var el = targets[j];
        if (!el || el.nodeType !== 1) continue;
        var seen = byKey.get(el);
        if (!seen) { seen = {}; byKey.set(el, seen); }
        for (var prop in tw.vars) {
          if (skip[prop]) continue;
          var prior = seen[prop];
          if (prior && from < prior - 0.001) add('motion-overlap', el, null, nameOf(el) + ' ' + prop + ' at ' + (from - gsapMaster.t0).toFixed(2) + 's');
          seen[prop] = Math.max(prior || 0, to);
        }
      }
    }
  }

  function nameOf(el) {
    var cls = typeof el.className === 'string' ? el.className.trim().split(/\s+/)[0] : '';
    if (el.id) return '#' + el.id;
    return el.tagName.toLowerCase() + (cls ? '.' + cls : '');
  }

  function absoluteStart(tw) {
    var at = tw.startTime();
    for (var p = tw.parent; p && p !== gsapMaster.tl; p = p.parent) at = p.startTime() + at;
    return at;
  }

  // ---- ready ----

  function drawn() {
    var x = document.querySelector('x-dc');
    if (!x) return true;
    var root = document.getElementById('dc-root');
    return !!(root && root.firstElementChild);
  }

  var readyPromise = null;
  function ready() {
    if (readyPromise) return readyPromise;
    readyPromise = new Promise(function (resolve) {
      var tries = 0;
      function poll() {
        tries += 1;
        if (document.readyState !== 'loading' && drawn() || tries > 600) {
          var bar = document.querySelectorAll('[data-tk-library-bar]');
          for (var i = 0; i < bar.length; i++) bar[i].remove();
          // Layout first: a font starts loading only once text asks for it.
          void document.documentElement.offsetHeight;
          var fonts = document.fonts && document.fonts.ready ? document.fonts.ready : Promise.resolve();
          fonts.then(images).then(function () {
            settled = true;
            runBuilders();
            return readyEngines();
          }).then(function () {
            engage();
            seekEngines(vt, null);
            resolve(true);
          });
          return;
        }
        setTimeout(poll, 25);
      }
      poll();
    });
    return readyPromise;
  }
  function images() {
    var imgs = document.images;
    var waits = [];
    for (var i = 0; i < imgs.length; i++) {
      if (!imgs[i].complete && imgs[i].decode) waits.push(imgs[i].decode().catch(function () {}));
    }
    return Promise.all(waits);
  }

  // ---- the player ----

  var lastTime = 0;
  function frame() {
    if (!playing) return;
    vt = Math.max(0, Math.round(playing.t0 + (realPerf() - playing.r0) * playing.rate));
    flushRaf();
    seekEngines(vt, null);
    apply(vt);
    var now = realPerf();
    if (now - lastTime >= TIME_EVERY) {
      lastTime = now;
      tell({ type: 'tk:video-time', t: vt });
    }
    realRaf(frame);
  }

  function tell(msg) {
    if (window.parent === window) return;
    var origins = location.ancestorOrigins;
    var to = origins && origins.length ? origins[0] : '*';
    try { window.parent.postMessage(msg, to); } catch (err) { /* the host is gone */ }
  }

  function tellError(err) {
    tell({ type: 'tk:video-error', msg: String(err && err.message || err).slice(0, 400) });
  }

  function num(v) { return typeof v === 'number' && isFinite(v) ? v : null; }

  function setScene(cfg) {
    var tr = cfg && cfg.transition;
    if (tr && (tr.kind === 'fade' || tr.kind === 'none') && num(tr.ms) !== null) {
      transition = { kind: tr.kind, ms: Math.max(0, Math.min(2000, tr.ms)) };
    } else {
      transition = null;
    }
    if (cfg && Array.isArray(cfg.overlays)) setOverlays(cfg.overlays);
    dirty = true;
  }

  window.addEventListener('message', function (event) {
    if (event.source !== window.parent || window.parent === window) return;
    var d = event.data;
    if (!d || typeof d !== 'object') return;
    switch (d.type) {
      case 'tk:video-seek': {
        var t = num(d.t);
        if (t === null) return;
        playing = null;
        seekTo(t);
        return;
      }
      case 'tk:video-play': {
        var t0 = num(d.t);
        var rate = num(d.rate);
        if (t0 === null) return;
        engage();
        var was = playing;
        playing = { t0: Math.max(0, t0), r0: realPerf(), rate: rate !== null && rate > 0 && rate <= 4 ? rate : 1 };
        if (!was) realRaf(frame);
        return;
      }
      case 'tk:video-pause':
        playing = null;
        tell({ type: 'tk:video-time', t: vt });
        return;
      case 'tk:video-hit': {
        var id = num(d.id);
        var x = num(d.x);
        var y = num(d.y);
        if (id === null || x === null || y === null) return;
        var h = hit(x, y);
        tell({ type: 'tk:video-hit', id: id, block: h.block, rect: h.rect });
        return;
      }
      case 'tk:video-scene':
        setScene(d);
        apply(vt);
        return;
    }
  });

  window.addEventListener('error', function (event) {
    var t = event.target;
    if (t && t !== window && t.nodeType === 1) {
      problem('asset', (t.src || t.href || t.tagName) + ' did not load');
      return;
    }
    problem('script', event.message || event.error);
    tellError(event.error || event.message);
  }, true);
  window.addEventListener('unhandledrejection', function (event) {
    problem('script', event.reason && event.reason.message || event.reason);
    tellError(event.reason);
  });

  // external: what the board fetched from another origin. A video draws from the project and the kit alone.
  function external() {
    var out = [];
    var list = performance.getEntriesByType ? performance.getEntriesByType('resource') : [];
    for (var i = 0; i < list.length; i++) {
      var u = list[i].name;
      if (u.indexOf('data:') === 0 || u.indexOf('blob:') === 0) continue;
      try { if (new URL(u).origin !== location.origin) out.push(u); } catch (err) { /* not an address */ }
    }
    return out;
  }

  function allProblems() {
    var out = problems.slice();
    var ext = external();
    for (var i = 0; i < ext.length && i < 10; i++) out.push({ code: 'external', msg: ext[i] });
    return out;
  }

  window.tkVideo = {
    v: 3,
    ready: ready,
    seek: function (ms) { return ready().then(function () { playing = null; return seekTo(ms); }); },
    audit: function (opts) { return ready().then(function () { return audit(opts); }); },
    moments: moments,
    scene: setScene,
    motion: motion,
    register: register,
    problems: allProblems,
    info: function () {
      return { engines: engines.map(function (e) { return e.name; }), motionDur: Math.round(motionEnd() * 1000), overlays: ov.items.length };
    },
  };

  // Once the board is drawn: hide what waits for its moment, then say the scene is ready.
  ready().then(function () {
    apply(vt);
    tell({ type: 'tk:video-ready', moments: moments() });
  });
})();
