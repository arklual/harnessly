// Motion presets for a video's board: one vocabulary of movement on top of GSAP. Load it after gsap.min.js and
// call it inside tkVideo.motion. Every call adds to one timeline at a time you give, so the frame stays a function of t.
//   tk.enter(target, {at, from: 'up'|'down'|'left'|'right'|'scale'|'fade'|'mask', dur, stagger, ease})
//   tk.exit(target, {at, to: 'up'|'down'|'left'|'right'|'scale'|'fade', dur, ease})
//   tk.accent(target, {at, kind: 'pulse'|'nudge'|'wiggle', dur})
//   tk.camera(world, {at, dur, x, y, zoom, ease})     world is the wrapper that holds the scene: one move at a time
//   tk.draw(paths, {at, dur, stagger, ease})          needs DrawSVGPlugin.min.js
//   tk.text(target, {at, by: 'chars'|'words'|'lines', dur, stagger, from}) needs SplitText.min.js
//   tk.physical.drop(target, {at, height, bounces})   a fall with squash and stretch
//   tk.physical.hop(target, {at, height, dur})        a jump with anticipation, stretch and landing
//   tk.morph(a, b, {at, dur, ease})                   one shape into another                    needs MorphSVGPlugin.min.js
//   tk.drawHand(paths, {at, dur, jitter, fps, seed})  a line drawn by hand, a little unsteady        needs DrawSVGPlugin.min.js
//   tk.squash(target, {at, amount, dur})              weight: squash, stretch, settle
//   tk.inertia(target, {at, dur, x, y, rotation, scale, overshoot})  a move that overshoots and settles
//   tk.parallax(layers, {at, dur, amount, depths})    layers drift by depth; amount in px, vw, vh, vmin (or drawing units)
//   tk.camera.drift(world, {at, dur, amount, seed})   slow handheld drift of the world            (xPercent, yPercent, rotation)
//   tk.camera.shake(world, {at, dur, amount, seed})   a knock that dies away                      (xPercent, yPercent, rotation)
//   tk.particles(container, {at, kind: 'confetti'|'sparks'|'dust', count, x, y, from, seed, dur, power, colors})
//   tk.sceneLength(seconds)                           the scene's length, for what lasts all of it (breathing, drift)
//   tk.rng(seed)                                      a seeded generator: same seed, same numbers
//   tk.tl is the timeline; add your own tweens to it with tk.tl.to(target, vars, at).
// Every preset is a function of the scene's time: the frame at t is the same played through or sought to.
(function () {
  'use strict';
  if (!window.gsap) throw new Error('tk.js needs gsap.min.js before it');
  var g = window.gsap;
  var line = null;

  function tl() {
    if (!line) line = g.timeline();
    return line;
  }

  var SHIFT = {
    up: { y: '6vmin' }, down: { y: '-6vmin' }, left: { x: '6vmin' }, right: { x: '-6vmin' },
    scale: { scale: 0.85 }, fade: {},
  };
  var REST = { up: { y: 0 }, down: { y: 0 }, left: { x: 0 }, right: { x: 0 }, scale: { scale: 1 }, fade: {} };
  var LEAVE = {
    up: { y: '-6vmin' }, down: { y: '6vmin' }, left: { x: '-6vmin' }, right: { x: '6vmin' }, scale: { scale: 0.9 }, fade: {},
  };

  // start: a value a thing has from the first frame to the move that changes it. Drawn at once, so the frame before the first
  // seek is what the frame reached by seeking back is.
  function startWith(target, vars) {
    var v = { immediateRender: true };
    for (var k in vars) v[k] = vars[k];
    tl().set(target, v, 0);
    return tl();
  }

  function pick(table, name) {
    return Object.prototype.hasOwnProperty.call(table, name) ? table[name] : table.up;
  }

  function merge(a, b) {
    var out = {};
    var k;
    for (k in a) out[k] = a[k];
    for (k in b) out[k] = b[k];
    return out;
  }

  function enter(target, o) {
    o = o || {};
    var dur = o.dur === undefined ? 0.6 : o.dur;
    var ease = o.ease || 'expo.out';
    var stagger = o.stagger || 0;
    if (o.from === 'mask') {
      tl().fromTo(target, { clipPath: 'inset(0 100% 0 0)' }, { clipPath: 'inset(0 0% 0 0)', duration: dur, ease: ease, stagger: stagger }, o.at || 0);
      return tl();
    }
    tl().fromTo(target, merge({ autoAlpha: 0 }, pick(SHIFT, o.from)),
      merge({ autoAlpha: 1, duration: dur, ease: ease, stagger: stagger }, pick(REST, o.from)), o.at || 0);
    return tl();
  }

  function exit(target, o) {
    o = o || {};
    tl().to(target, merge({ autoAlpha: 0, duration: o.dur === undefined ? 0.3 : o.dur, ease: o.ease || 'power2.in', stagger: o.stagger || 0 }, pick(LEAVE, o.to || 'fade')), o.at || 0);
    return tl();
  }

  function accent(target, o) {
    o = o || {};
    var dur = o.dur === undefined ? 0.25 : o.dur;
    var at = o.at || 0;
    if (o.kind === 'nudge') {
      tl().to(target, { x: '1vmin', duration: dur / 2, ease: 'power2.out', yoyo: true, repeat: 1 }, at);
    } else if (o.kind === 'wiggle') {
      tl().to(target, { rotation: 4, duration: dur / 4, ease: 'sine.inOut', yoyo: true, repeat: 3 }, at);
    } else {
      tl().to(target, { scale: 1.08, duration: dur / 2, ease: 'power2.out', yoyo: true, repeat: 1 }, at);
    }
    return tl();
  }

  function camera(world, o) {
    o = o || {};
    var v = { duration: o.dur === undefined ? 1.2 : o.dur, ease: o.ease || 'power2.inOut' };
    if (o.x !== undefined) v.x = o.x;
    if (o.y !== undefined) v.y = o.y;
    if (o.zoom !== undefined) v.scale = o.zoom;
    tl().to(world, v, o.at || 0);
    return tl();
  }

  function draw(paths, o) {
    o = o || {};
    tl().fromTo(paths, { drawSVG: '0%' }, { drawSVG: '100%', duration: o.dur === undefined ? 0.8 : o.dur, ease: o.ease || 'power2.inOut', stagger: o.stagger || 0 }, o.at || 0);
    return tl();
  }

  function text(target, o) {
    o = o || {};
    if (!window.SplitText) throw new Error('tk.text needs SplitText.min.js');
    var by = o.by || 'words';
    var parts = new window.SplitText(target, { type: by === 'chars' ? 'words,chars' : by });
    return enter(parts[by], { at: o.at, from: o.from || 'up', dur: o.dur === undefined ? 0.5 : o.dur, stagger: o.stagger === undefined ? 0.05 : o.stagger, ease: o.ease });
  }

  var physical = {
    drop: function (target, o) {
      o = o || {};
      var at = o.at || 0;
      var height = o.height || '60vh';
      var bounces = o.bounces === undefined ? 2 : o.bounces;
      startWith(target, { transformOrigin: '50% 100%', y: '-' + height });
      tl().to(target, { y: 0, duration: 0.45, ease: 'power2.in' }, at);
      var t = at + 0.45;
      var rise = parseFloat(height) * 0.3;
      var unit = String(height).replace(/[-0-9.]/g, '');
      for (var i = 0; i <= bounces; i++) {
        tl().to(target, { scaleY: 0.75, scaleX: 1.2, duration: 0.07, ease: 'power1.out' }, t);
        tl().to(target, { scaleY: 1, scaleX: 1, duration: 0.14, ease: 'back.out(3)' }, t + 0.07);
        if (i === bounces) break;
        var up = 0.32 * Math.pow(0.7, i);
        tl().to(target, { y: -rise + unit, duration: up, ease: 'power2.out' }, t + 0.07);
        tl().to(target, { y: 0, duration: up, ease: 'power2.in' }, t + 0.07 + up);
        t += 0.07 + up * 2;
        rise *= 0.45;
      }
      return tl();
    },
    hop: function (target, o) {
      o = o || {};
      var at = o.at || 0;
      var dur = o.dur === undefined ? 0.6 : o.dur;
      var height = o.height || '12vh';
      startWith(target, { transformOrigin: '50% 100%' });
      tl().to(target, { scaleY: 0.8, scaleX: 1.12, duration: 0.1, ease: 'power1.out' }, at);
      tl().to(target, { scaleY: 1.1, scaleX: 0.94, duration: 0.08, ease: 'power1.out' }, at + 0.1);
      tl().to(target, { y: '-' + height, duration: dur / 2, ease: 'power2.out' }, at + 0.1);
      tl().to(target, { y: 0, duration: dur / 2, ease: 'power2.in' }, at + 0.1 + dur / 2);
      tl().to(target, { scaleY: 0.82, scaleX: 1.14, duration: 0.07, ease: 'power1.out' }, at + 0.1 + dur);
      tl().to(target, { scaleY: 1, scaleX: 1, duration: 0.2, ease: 'back.out(3)' }, at + 0.17 + dur);
      return tl();
    },
  };

  // ---- HRN-652: living motion ----

  var sceneSeconds = null;

  function sceneLength(seconds) {
    if (seconds !== undefined) {
      if (!(seconds > 0)) throw new Error('tk.sceneLength needs seconds above zero');
      sceneSeconds = seconds;
    }
    return sceneSeconds;
  }

  // What lasts the whole scene needs its length: a tween that ran past it would stretch the motion.
  function lasting(at, dur, what) {
    if (dur !== undefined) return dur;
    if (sceneSeconds === null) throw new Error(what + ' lasts the scene: call tk.sceneLength(<seconds>) once, or pass {dur}');
    return Math.max(0.1, sceneSeconds - at);
  }

  function hash(text) {
    var h = 2166136261;
    var s = String(text);
    for (var i = 0; i < s.length; i++) {
      h ^= s.charCodeAt(i);
      h = Math.imul(h, 16777619);
    }
    return h >>> 0;
  }

  // rng: mulberry32 from a seed (a number or a text). Not Math.random: a preset's chance is the same on every draw.
  function rng(seed) {
    var a = typeof seed === 'number' ? seed >>> 0 : hash(seed === undefined ? 'tk' : seed);
    return function () {
      a = (a + 0x6d2b79f5) | 0;
      var t = Math.imul(a ^ (a >>> 15), 1 | a);
      t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
      return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
    };
  }

  function need(plugin, what) {
    if (!window[plugin]) throw new Error(what + ' needs ' + plugin + '.min.js in the head, after gsap.min.js');
    g.registerPlugin(window[plugin]);
  }

  function els(target) {
    return g.utils.toArray(target);
  }

  function r3(x) {
    return Math.round(x * 1000) / 1000;
  }

  // chain: a property moving through points at the times given (seconds), segment by segment, each with both its ends
  // spelled out, so any frame is the same whether the earlier segments were played or skipped.
  function chain(target, prop, points, times, ease) {
    if (!target || target.length === 0) return tl();
    for (var i = 0; i < points.length - 1; i++) {
      var from = {}, to = { duration: r3(times[i + 1] - times[i]), ease: ease || 'sine.inOut', immediateRender: false };
      from[prop] = points[i];
      to[prop] = points[i + 1];
      tl().fromTo(target, from, to, times[i]);
    }
    return tl();
  }

  // px of a length: a number is px, 'vw', 'vh', 'vmin', 'vmax', '%' of the viewport are what they say.
  function lengthPx(v) {
    if (typeof v === 'number') return v;
    var m = /^(-?[0-9.]+)\s*(px|vw|vh|vmin|vmax)?$/.exec(String(v).trim());
    if (!m) throw new Error('tk: a length like 12, "6vw" or "10vmin", not ' + v);
    var n = parseFloat(m[1]);
    var unit = m[2] || 'px';
    var w = window.innerWidth, h = window.innerHeight;
    var k = unit === 'vw' ? w / 100 : unit === 'vh' ? h / 100 : unit === 'vmin' ? Math.min(w, h) / 100 : unit === 'vmax' ? Math.max(w, h) / 100 : 1;
    return n * k;
  }

  // In a drawing a length is in the drawing's own units: px divided by the size of one unit on the screen.
  function lengthFor(el, v) {
    var px = lengthPx(v);
    if (typeof v === 'number' && !(el instanceof SVGElement && el.ownerSVGElement)) return v;
    if (el instanceof SVGElement && el.ownerSVGElement && typeof v !== 'number') {
      var m = el.ownerSVGElement.getScreenCTM();
      return m && m.a ? px / m.a : px;
    }
    return px;
  }

  function morph(a, b, o) {
    o = o || {};
    need('MorphSVGPlugin', 'tk.morph');
    var shape = { shape: b };
    if (o.shapeIndex !== undefined) shape.shapeIndex = o.shapeIndex;
    if (o.type) shape.type = o.type;
    if (o.origin) shape.origin = o.origin;
    tl().to(a, { morphSVG: shape, duration: o.dur === undefined ? 0.6 : o.dur, ease: o.ease || 'power2.inOut' }, o.at || 0);
    return tl();
  }

  function heldThenJump(p) { return p >= 1 ? 1 : 0; }

  // drawHand: DrawSVG, plus a line "boil": every 1/fps second the line shifts by a seeded fraction of a unit, as a hand does.
  function drawHand(paths, o) {
    o = o || {};
    need('DrawSVGPlugin', 'tk.drawHand');
    var at = o.at || 0;
    var dur = o.dur === undefined ? 1.0 : o.dur;
    var list = els(paths);
    var fps = o.fps || 12;
    var jitter = o.jitter === undefined ? 1.2 : o.jitter;
    var stagger = o.stagger || 0;
    var ease = o.ease || 'power1.inOut';
    list.forEach(function (el, i) {
      var start = at + i * stagger;
      tl().fromTo(el, { drawSVG: '0%' }, { drawSVG: '100%', duration: dur, ease: ease }, start);
      if (!jitter) return;
      var rand = rng((o.seed === undefined ? 'hand' : o.seed) + ':' + i);
      // The line holds a place for 1/fps of a second and then takes the next: every step is a move of its own with both ends
      // spelled out, so a frame on the very edge of a step is the same whether it is reached forwards or backwards.
      var steps = Math.max(1, Math.ceil((dur + 0.2) * fps));
      var last = { x: 0, y: 0, rotation: 0 };
      for (var s = 0; s < steps; s++) {
        var next = s === steps - 1 ? { x: 0, y: 0, rotation: 0 } :
          { x: r3((rand() - 0.5) * 2 * jitter), y: r3((rand() - 0.5) * 2 * jitter), rotation: r3((rand() - 0.5) * 0.8) };
        var t0 = r3(start + s / fps), t1 = r3(start + (s + 1) / fps);
        tl().fromTo(el, last, { x: next.x, y: next.y, rotation: next.rotation, duration: r3(t1 - t0), ease: heldThenJump, immediateRender: false }, t0);
        last = next;
      }
    });
    return tl();
  }

  // squash: the weight of a thing: it presses down, springs up past its size and settles.
  function squash(target, o) {
    o = o || {};
    var at = o.at || 0;
    var dur = o.dur === undefined ? 0.55 : o.dur;
    var a = o.amount === undefined ? 0.18 : o.amount;
    startWith(target, { transformOrigin: o.origin || '50% 100%' });
    var t1 = r3(at + dur * 0.22), t2 = r3(at + dur * 0.46), t3 = r3(at + dur);
    chain(target, 'scaleY', [1, 1 - a, 1 + a * 0.4, 1], [at, t1, t2, t3], 'sine.inOut');
    chain(target, 'scaleX', [1, 1 + a * 0.7, 1 - a * 0.25, 1], [at, t1, t2, t3], 'sine.inOut');
    return tl();
  }

  // inertia: it goes past where it is going and settles back; `overshoot` is how far (1 is a little, 3 a lot).
  function inertia(target, o) {
    o = o || {};
    var v = { duration: o.dur === undefined ? 0.8 : o.dur, ease: 'back.out(' + (o.overshoot === undefined ? 1.7 : o.overshoot) + ')' };
    ['x', 'y', 'rotation', 'scale', 'scaleX', 'scaleY', 'autoAlpha', 'opacity'].forEach(function (k) {
      if (o[k] !== undefined) v[k] = o[k];
    });
    var f = o.from;
    if (f) tl().fromTo(target, f, v, o.at || 0);
    else tl().to(target, v, o.at || 0);
    return tl();
  }

  // parallax: layers far to near; each drifts by its depth (data-depth on the layer, else its place in the list).
  function parallax(layers, o) {
    o = o || {};
    var list = els(layers);
    var at = o.at || 0;
    var dur = o.dur === undefined ? 3 : o.dur;
    var amount = o.amount === undefined ? 60 : o.amount;
    var axis = o.axis === 'y' ? 'y' : 'x';
    var dir = o.dir === -1 ? -1 : 1;
    list.forEach(function (el, i) {
      var depth = o.depths ? o.depths[i] : parseFloat(el.getAttribute && el.getAttribute('data-depth'));
      if (!(depth >= 0)) depth = list.length > 1 ? (i + 1) / list.length : 1;
      var half = lengthFor(el, amount) * depth * dir / 2;
      var from = {}, to = { duration: dur, ease: o.ease || 'none' };
      from[axis] = -half;
      to[axis] = half;
      tl().fromTo(el, from, to, at);
    });
    return tl();
  }

  // rested: the world has its transform from the first frame, so a frame before the move and the same frame reached by
  // seeking back are rasterised alike (Chrome draws a thing with a transform on a layer of its own).
  function rested(world) {
    startWith(world, { xPercent: 0, yPercent: 0, rotation: 0 });
  }

  // camera.drift: a slow wander of the world: a few places picked from the seed, eased between, back home at the end.
  camera.drift = function (world, o) {
    o = o || {};
    var at = o.at || 0;
    var dur = lasting(at, o.dur, 'tk.camera.drift');
    var amount = o.amount === undefined ? 1 : o.amount;
    var rand = rng(o.seed === undefined ? 'drift' : o.seed);
    var n = Math.max(1, Math.round(dur / 3));
    var xs = [0], ys = [0], rs = [0], times = [at];
    for (var i = 1; i <= n; i++) {
      var last = i === n;
      xs.push(last ? 0 : r3((rand() - 0.5) * 1.6 * amount));
      ys.push(last ? 0 : r3((rand() - 0.5) * 1.0 * amount));
      rs.push(last ? 0 : r3((rand() - 0.5) * 0.7 * amount));
      times.push(r3(at + dur * i / n));
    }
    rested(world);
    chain(world, 'xPercent', xs, times, 'sine.inOut');
    chain(world, 'yPercent', ys, times, 'sine.inOut');
    chain(world, 'rotation', rs, times, 'sine.inOut');
    return tl();
  };

  // camera.shake: noise at 30 points a second under a falling envelope, from nothing to nothing.
  camera.shake = function (world, o) {
    o = o || {};
    var at = o.at || 0;
    var dur = o.dur === undefined ? 0.5 : o.dur;
    var amount = o.amount === undefined ? 1.2 : o.amount;
    var rand = rng(o.seed === undefined ? 'shake' : o.seed);
    var n = Math.max(2, Math.round(dur * 30));
    var xs = [0], ys = [0], rs = [0], times = [at];
    for (var i = 1; i <= n; i++) {
      var k = i === n ? 0 : Math.pow(1 - i / n, 1.4) * amount;
      xs.push(r3((rand() * 2 - 1) * k));
      ys.push(r3((rand() * 2 - 1) * k));
      rs.push(r3((rand() * 2 - 1) * k * 0.3));
      times.push(r3(at + dur * i / n));
    }
    rested(world);
    chain(world, 'xPercent', xs, times, 'none');
    chain(world, 'yPercent', ys, times, 'none');
    chain(world, 'rotation', rs, times, 'none');
    return tl();
  };

  var PARTICLE_KINDS = {
    confetti: { count: 70, power: 62, gravity: 70, drag: 1.6, size: 1.5, life: 2.6, spread: 110, shapes: ['rect', 'rect', 'dot', 'strip'] },
    sparks: { count: 28, power: 78, gravity: 22, drag: 3.2, size: 0.55, life: 0.9, spread: 360, shapes: ['spark'] },
    dust: { count: 16, power: 14, gravity: -3, drag: 2.2, size: 2.4, life: 1.5, spread: 140, shapes: ['puff'] },
  };

  var CONFETTI = ['var(--deck-acc,#7b61ff)', '#ffd15c', '#ff9a52', '#33c07f', '#6ec0f2', '#ef5a5a', '#ffffff'];
  var SPARKS = ['#ffd15c', '#ffb347', '#fff3c4', 'var(--deck-acc,#7b61ff)'];
  var DUST = ['rgba(255,255,255,.85)', 'rgba(190,196,210,.7)'];

  // particles: n bits thrown from a point (x, y in % of the container, or `from`: a target's middle), each on a path worked out
  // from the seed: velocity, drag and gravity give its place at any time, so a frame is the same however it is reached.
  function particles(container, o) {
    o = o || {};
    var host = typeof container === 'string' ? document.querySelector(container) : container;
    if (!host) throw new Error('tk.particles: no container ' + container);
    var kind = o.kind || 'confetti';
    var base = PARTICLE_KINDS[kind];
    if (!base) throw new Error('tk.particles: kind is confetti, sparks or dust');
    var at = o.at || 0;
    var count = o.count || base.count;
    var power = o.power || base.power;
    var gravity = o.gravity === undefined ? base.gravity : o.gravity;
    var drag = o.drag === undefined ? base.drag : o.drag;
    var life = o.dur || base.life;
    var size = (o.size || base.size);
    var spread = o.spread === undefined ? base.spread : o.spread;
    var aim = o.angle === undefined ? -90 : o.angle;
    var colors = o.colors || (kind === 'confetti' ? CONFETTI : kind === 'sparks' ? SPARKS : DUST);
    var rand = rng(o.seed === undefined ? kind : o.seed);
    var ox = o.x === undefined ? 50 : parseFloat(o.x);
    var oy = o.y === undefined ? 60 : parseFloat(o.y);
    if (o.from) {
      var src = typeof o.from === 'string' ? document.querySelector(o.from) : o.from;
      var a = src.getBoundingClientRect(), b = host.getBoundingClientRect();
      ox = (a.left + a.width / 2 - b.left) / b.width * 100;
      oy = (a.top + a.height / 2 - b.top) / b.height * 100;
    }
    var unit = Math.min(window.innerWidth, window.innerHeight) / 100; // 1vmin in px: the path is drawn in px for this size
    var made = [];
    function bit() {
      var shape = base.shapes[Math.floor(rand() * base.shapes.length)];
      var p = document.createElement('i');
      p.setAttribute('data-tk-particle', kind);
      var s = size * (0.6 + rand() * 0.8);
      var w = shape === 'strip' ? s * 0.45 : shape === 'spark' ? s * 4 : s;
      var h = shape === 'rect' ? s * 0.6 : shape === 'strip' ? s * 1.8 : shape === 'spark' ? s : s;
      var round = shape === 'dot' || shape === 'puff' || shape === 'spark';
      p.style.cssText = 'position:absolute;left:' + (ox + (rand() - 0.5) * 3).toFixed(2) + '%;top:' + (oy + (rand() - 0.5) * 3).toFixed(2) +
        '%;width:' + w.toFixed(2) + 'vmin;height:' + h.toFixed(2) + 'vmin;margin:-' + (h / 2).toFixed(2) + 'vmin 0 0 -' + (w / 2).toFixed(2) +
        'vmin;background:' + colors[Math.floor(rand() * colors.length)] + ';border-radius:' + (round ? '50%' : '0.2vmin') +
        ';pointer-events:none;opacity:0;visibility:hidden;' + (shape === 'puff' ? 'filter:blur(0.4vmin);' : '');
      host.appendChild(p);
      var ang = (aim + (rand() - 0.5) * spread) * Math.PI / 180;
      var v = power * (0.45 + rand() * 0.55);
      var T = life * (0.7 + rand() * 0.3);
      var late = rand() * (kind === 'dust' ? 0.4 : 0.12);
      var vx = Math.cos(ang) * v, vy = Math.sin(ang) * v;
      var X = function (t) { return vx * (1 - Math.exp(-drag * t)) / drag; };
      var Y = function (t) { return gravity * t / drag + (vy - gravity / drag) * (1 - Math.exp(-drag * t)) / drag; };
      var endX = (X(T) * unit) || 0.001, endY = (Y(T) * unit) || 0.001;
      var start = r3(at + late);
      tl().fromTo(p, { autoAlpha: 0 }, { autoAlpha: 1, duration: 0.001, immediateRender: false }, start);
      startWith(p, { rotation: shape === 'spark' ? r3(ang * 180 / Math.PI) : r3(rand() * 360) });
      tl().fromTo(p, { x: 0 }, { x: r3(endX), duration: T, ease: (function (fx, e) { return function (k) { return fx(k * T) * unit / e; }; })(X, endX), immediateRender: false }, start);
      tl().fromTo(p, { y: 0 }, { y: r3(endY), duration: T, ease: (function (fy, e) { return function (k) { return fy(k * T) * unit / e; }; })(Y, endY), immediateRender: false }, start);
      if (kind === 'confetti') {
        tl().fromTo(p, { rotation: 0 }, { rotation: r3((rand() - 0.5) * 900), duration: T, ease: 'none', immediateRender: false }, start);
        var flips = 2 + Math.floor(rand() * 4);
        tl().fromTo(p, { scaleY: 1 }, { scaleY: -1, duration: r3(T / (flips * 2)), ease: 'sine.inOut', repeat: flips * 2 - 1, yoyo: true, immediateRender: false }, start);
      } else if (kind === 'puff') {
        tl().fromTo(p, { scale: 0.4 }, { scale: 1.6, duration: T, ease: 'power1.out', immediateRender: false }, start);
      } else if (kind === 'sparks') {
        tl().fromTo(p, { scaleX: 1 }, { scaleX: 0.1, duration: T, ease: 'power2.in', immediateRender: false }, start);
      }
      tl().to(p, { opacity: 0, duration: r3(T * 0.3), ease: 'power1.in', immediateRender: false }, r3(start + T * 0.7));
      made.push(p);
    }
    for (var i = 0; i < count; i++) bit();
    return tl();
  }

  window.tk = {
    get tl() { return tl(); },
    enter: enter, exit: exit, accent: accent, camera: camera, draw: draw, text: text, physical: physical,
    morph: morph, drawHand: drawHand, squash: squash, inertia: inertia, overshoot: inertia, parallax: parallax, particles: particles,
    sceneLength: sceneLength, rng: rng, chain: chain, start: startWith,
  };
})();
