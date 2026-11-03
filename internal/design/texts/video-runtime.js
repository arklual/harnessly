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
//   - The scene comes in with a fade from --deck-bg (data-transition on <main>, or what the host sets).
//
// Player (window.parent) ↔ scene, by postMessage:
//   host → scene  {type:'tk:video-seek', t}  {type:'tk:video-play', t, rate}  {type:'tk:video-pause'}
//                 {type:'tk:video-hit', id, x, y}  {type:'tk:video-scene', transition:{kind, ms}|null}
//   scene → host  {type:'tk:video-ready', moments:[{block, at, out|null}]}  {type:'tk:video-time', t} (≤ 4 a second)
//                 {type:'tk:video-hit', id, block|null, rect:[x, y, w, h]|null}  {type:'tk:video-error', msg}
// A message is taken only from window.parent.
//
// Headless (the render and the check, through the DevTools protocol):
//   window.tkVideo = {v: 2, ready(): Promise, seek(ms): Promise<{same}>, audit(opts?): Promise<issue[]>, moments(),
//                     scene({transition}), problems(): problem[], info(): {engines, motionDur}}
//
// The board's motion engines. All of them hang on one time:
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
    v: 2,
    ready: ready,
    seek: function (ms) { return ready().then(function () { playing = null; return seekTo(ms); }); },
    audit: function (opts) { return ready().then(function () { return audit(opts); }); },
    moments: moments,
    scene: setScene,
    motion: motion,
    register: register,
    problems: allProblems,
    info: function () {
      return { engines: engines.map(function (e) { return e.name; }), motionDur: Math.round(motionEnd() * 1000) };
    },
  };

  // Once the board is drawn: hide what waits for its moment, then say the scene is ready.
  ready().then(function () {
    apply(vt);
    tell({ type: 'tk:video-ready', moments: moments() });
  });
})();
