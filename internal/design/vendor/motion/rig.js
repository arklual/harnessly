// A rigged hero for a video's board: a drawing from art/heroes.js with named parts, and the actions of a character as
// calls on the scene's timeline. Load after gsap.min.js, MorphSVGPlugin.min.js, tk.js, art/art.js and art/heroes.js.
//
//   <svg class="hero" data-tk-art="hero-person"></svg>
//   var hero = tk.rig('.hero');                    wraps the drawing; sets the pivots of its parts
//   tk.sceneLength(8);                             once: what lasts the scene (breathing, blinking) lasts this long
//   hero.alive();                                  breathing and blinking for all of the scene: do this for every hero
//   hero.walk('-60vw', '0vw', 2, {at: 0.3});       positional arguments first, then {at, ...}; every action takes `at` (seconds)
//   hero.talk(3, {at: 2.4, text: 'Смотрите, что получилось'});
//   hero.wave({at: 1});  hero.point('.chart', {at: 3});  hero.look('.chart', {at: 3});  hero.nod({at: 5});
//   hero.shrug();  hero.jump({at: 6});  hero.blink();  hero.emotion('happy' | 'surprised' | 'sad' | 'neutral', {at, hold});
//
// Parts it moves (data-part): root (jump, bob) > body (breath, lean) > head (nod) > face (look) > eyeL eyeR (blink) pupilL pupilR
// (look) browL browR cheekL cheekR mouth (morph); armL armR (swing, wave, point) > foreL foreR; legL legR (step); shadow; tuft.
// One property of one part is moved by one action at a time: the check calls two at once motion-overlap. Actions end at rest.
(function () {
  'use strict';
  if (!window.tk || !window.gsap) throw new Error('rig.js needs gsap.min.js and tk.js before it');
  var g = window.gsap;
  var tk = window.tk;
  var tl = function () { return tk.tl; };

  function r3(x) { return Math.round(x * 1000) / 1000; }

  // pivots: gsap turns a part about the point its drawing names (data-pivot="x y", in the drawing's units).
  function pivots(root) {
    var host = typeof root === 'string' ? document.querySelector(root) : root;
    if (!host) throw new Error('tk.pivots: no element ' + root);
    var list = host.querySelectorAll('[data-pivot]');
    for (var i = 0; i < list.length; i++) {
      if (list[i].__tkPivot) continue;
      list[i].__tkPivot = true;
      tk.start(list[i], { svgOrigin: list[i].getAttribute('data-pivot') });
    }
    return host;
  }

  // part: the elements of a drawing called name (data-part), as an array: tk.part('.chart', 'bar').
  function part(root, name) {
    var host = typeof root === 'string' ? document.querySelector(root) : root;
    if (!host) throw new Error('tk.part: no element ' + root);
    return Array.prototype.slice.call(host.querySelectorAll('[data-part="' + name + '"]'));
  }

  // ft: a move with both its ends spelled out, drawn only once its time has come.
  function ft(target, from, to, at) {
    if (!target || (target.length === 0)) return;
    var v = {};
    for (var k in to) v[k] = to[k];
    v.immediateRender = false;
    tl().fromTo(target, from, v, r3(at));
  }

  function pxOf(v) {
    if (typeof v === 'number') return v;
    var m = /^(-?[0-9.]+)\s*(px|vw|vh|vmin|vmax)?$/.exec(String(v).trim());
    if (!m) throw new Error('tk.rig: a position like 0, "10vw" or "12vmin", not ' + v);
    var n = parseFloat(m[1]);
    var u = m[2];
    var w = window.innerWidth, h = window.innerHeight;
    return n * (u === 'vw' ? w / 100 : u === 'vh' ? h / 100 : u === 'vmin' ? Math.min(w, h) / 100 : u === 'vmax' ? Math.max(w, h) / 100 : 1);
  }

  // A point of a drawing on the screen.
  function screenPoint(svg, x, y) {
    var m = svg.getScreenCTM();
    if (!m) throw new Error('tk.rig: the hero is not on the page (display: none?)');
    return { x: m.a * x + m.c * y + m.e, y: m.b * x + m.d * y + m.f };
  }

  function centerOf(target) {
    if (target && typeof target === 'object' && !target.nodeType) {
      if (typeof target.x === 'number' && typeof target.y === 'number') return { x: target.x, y: target.y };
    }
    var el = typeof target === 'string' ? document.querySelector(target) : target;
    if (!el || !el.getBoundingClientRect) throw new Error('tk.rig: no target ' + target);
    var r = el.getBoundingClientRect();
    return { x: r.left + r.width / 2, y: r.top + r.height / 2 };
  }

  var MOUTH_FOR_LETTER = [
    [/[аяaA]/i, 'open'], [/[оёoOуюu]/i, 'o'], [/[иыэеiyeEЕ]/i, 'wide'], [/[мпбmpb]/i, 'closed'], [/\s/, 'closed'],
  ];

  function visemes(text, dur, rand) {
    var list = [];
    if (text) {
      String(text).toLowerCase().split('').forEach(function (ch) {
        if (!/[a-zа-яё\s]/i.test(ch)) return;
        var shape = 'mid';
        for (var i = 0; i < MOUTH_FOR_LETTER.length; i++) if (MOUTH_FOR_LETTER[i][0].test(ch)) { shape = MOUTH_FOR_LETTER[i][1]; break; }
        if (list.length && list[list.length - 1] === shape) return;
        list.push(shape);
      });
    }
    var step = 0.1;
    var most = Math.max(2, Math.floor((dur - 0.12) / step));
    if (!list.length) {
      var palette = ['open', 'mid', 'wide', 'o', 'mid', 'open'];
      for (var n = 0; n < most; n++) list.push(n % 2 ? 'closed' : palette[Math.floor(rand() * palette.length)]);
    }
    while (list.length > most) {
      var drop = 1 + Math.floor(rand() * (list.length - 1));
      list.splice(drop, 1);
    }
    return list;
  }

  var BROWS = {
    neutral: { y: 0, l: 0, r: 0 }, happy: { y: -5, l: -5, r: 5 }, surprised: { y: -13, l: 0, r: 0 }, sad: { y: 3, l: 12, r: -12 },
  };
  var EMOTIONS = {
    neutral: { mouth: null, cheek: 0 }, happy: { mouth: 'grin', cheek: 0.8 }, surprised: { mouth: 'o', cheek: 0 }, sad: { mouth: 'sad', cheek: 0 },
  };
  var BLINK_GAPS = [2.7, 3.6, 2.3, 4.2, 3.1, 2.9, 3.8];

  function Hero(host) {
    var el = typeof host === 'string' ? document.querySelector(host) : host;
    if (!el) throw new Error('tk.rig: no element ' + host);
    var svg = el;
    if (String(el.tagName).toLowerCase() !== 'svg') svg = el.querySelector('svg[data-tk-art]');
    if (!svg || !svg.getAttribute('data-tk-mounted')) {
      if (!tk.art) throw new Error('tk.rig needs art/art.js and art/heroes.js');
      tk.art.mount(el.parentNode || document);
      svg = String(el.tagName).toLowerCase() === 'svg' ? el : el.querySelector('svg[data-tk-art]');
    }
    if (!svg || !svg.querySelector('[data-part="body"]')) throw new Error('tk.rig: ' + host + ' is not a hero drawing (data-tk-art="hero-person", "hero-round" or "hero-robot")');
    this.el = svg;
    this.cfg = JSON.parse(svg.getAttribute('data-rig') || '{}');
    this.state = { look: { x: 0, y: 0 }, emotion: 'neutral', x0: null, x1: null };
    pivots(svg);
    if (!window.MorphSVGPlugin) throw new Error('tk.rig needs MorphSVGPlugin.min.js in the head, after gsap.min.js');
    g.registerPlugin(window.MorphSVGPlugin);
    // The mouth is first drawn in the plugin's own form of its path, so that a frame before the first morph and the same frame
    // reached by seeking back are the same pixels.
    var mouths = part(svg, 'mouth');
    for (var i = 0; i < mouths.length; i++) tk.start(mouths[i], { morphSVG: { shape: mouths[i].getAttribute('d') } });
  }

  var H = Hero.prototype;

  // P: the parts called by these names, all in one list.
  H.P = function () {
    var out = [];
    for (var i = 0; i < arguments.length; i++) out = out.concat(part(this.el, arguments[i]));
    return out;
  };

  H.rest = function () {
    var m = this.P('mouth')[0];
    return m ? m.getAttribute('data-rest') : null;
  };

  // Every path of the mouth (the mouth and its tongue) goes to its own shape of that name.
  function morphMouth(hero, name, at, dur, ease) {
    var m = hero.P('mouth');
    if (!m.length) return;
    for (var i = 0; i < m.length; i++) {
      var d = m[i].getAttribute('data-m-' + name);
      if (!d) throw new Error('tk.rig: this hero has no mouth "' + name + '"');
      tk.morph(m[i], d, { at: r3(at), dur: dur, ease: ease || 'sine.inOut' });
    }
  }

  // ---- the life that lasts the scene ----

  H.breathe = function (o) {
    o = o || {};
    var at = o.at || 0;
    var dur = o.dur !== undefined ? o.dur : lasting(at, 'hero.breathe');
    var period = o.period || 3.4;
    var amount = o.amount || this.cfg.breath || 0.016;
    var n = Math.max(2, 2 * Math.round(dur / period));
    var body = this.P('body');
    if (body.length) {
      tl().fromTo(body, { scaleY: 1, scaleX: 1 }, { scaleY: 1 + amount, scaleX: 1 + amount * 0.3, duration: r3(dur / n), ease: 'sine.inOut', repeat: n - 1, yoyo: true, immediateRender: false }, r3(at));
    }
    var tuft = this.P('tuft');
    if (tuft.length) {
      var m = Math.max(2, 2 * Math.round(dur / (period * 1.3)));
      tl().fromTo(tuft, { rotation: -3 }, { rotation: 3, duration: r3(dur / m), ease: 'sine.inOut', repeat: m - 1, yoyo: true, immediateRender: false }, r3(at));
    }
    return this;
  };

  function lasting(at, what) {
    var len = tk.sceneLength();
    if (len === null || len === undefined) throw new Error(what + ' lasts the scene: call tk.sceneLength(<seconds>) once, or pass {dur}');
    return Math.max(0.1, len - at);
  }

  H.blink = function (o) {
    o = o || {};
    var at = o.at || 0;
    var eyes = this.P('eyeL', 'eyeR');
    ft(eyes, { scaleY: 1 }, { scaleY: 0.08, duration: 0.06, ease: 'power2.in' }, at);
    ft(eyes, { scaleY: 0.08 }, { scaleY: 1, duration: 0.1, ease: 'power2.out' }, at + 0.06);
    return this;
  };

  // blinks: now and then, at gaps worked out from the seed, for `dur` seconds.
  H.blinks = function (o) {
    o = o || {};
    var at = o.at || 0;
    var dur = o.dur !== undefined ? o.dur : lasting(at, 'hero.blinks');
    var rand = tk.rng(o.seed === undefined ? 'blink' : o.seed);
    var t = at + (o.first === undefined ? 0.9 + rand() * 1.2 : o.first);
    var i = 0;
    var end = at + dur - 0.3;
    while (t < end) {
      this.blink({ at: t });
      var gap = BLINK_GAPS[i % BLINK_GAPS.length] * (0.8 + rand() * 0.4);
      if (rand() < 0.2) { this.blink({ at: t + 0.28 }); gap += 0.3; }
      t = r3(t + gap);
      i += 1;
    }
    return this;
  };

  // alive: what makes a still hero a living one.
  H.alive = function (o) {
    o = o || {};
    this.breathe(o);
    this.blinks(o);
    return this;
  };

  // ---- the eyes ----

  function lookVector(hero, dir) {
    if (dir === 'center' || dir === undefined || dir === null) return { x: 0, y: 0 };
    if (dir === 'left') return { x: -1, y: 0 };
    if (dir === 'right') return { x: 1, y: 0 };
    if (dir === 'up') return { x: 0, y: -1 };
    if (dir === 'down') return { x: 0, y: 1 };
    var head = hero.P('face')[0];
    var pivot = head.parentNode.getAttribute('data-pivot').split(' ').map(parseFloat);
    var from = screenPoint(hero.el, pivot[0], pivot[1] - 60);
    var to = centerOf(dir);
    var dx = to.x - from.x, dy = to.y - from.y;
    var dist = Math.sqrt(dx * dx + dy * dy) || 1;
    var k = Math.min(1, dist / (Math.min(window.innerWidth, window.innerHeight) * 0.35));
    return { x: dx / dist * k, y: dy / dist * k * 0.8 };
  }

  H.look = function (dir, o) {
    o = o || {};
    var at = o.at || 0;
    var dur = o.dur === undefined ? 0.28 : o.dur;
    var v = lookVector(this, dir);
    var s = this.state.look;
    var pu = this.cfg.pupil || 5;
    var fa = this.cfg.face || 6;
    ft(this.P('pupilL', 'pupilR'), { x: s.x * pu, y: s.y * pu }, { x: v.x * pu, y: v.y * pu, duration: dur, ease: 'power2.out' }, at);
    ft(this.P('face'), { x: s.x * fa, y: s.y * fa * 0.6 }, { x: v.x * fa, y: v.y * fa * 0.6, duration: dur * 1.2, ease: 'power2.out' }, at);
    this.state.look = v;
    return this;
  };

  // ---- the mouth ----

  // talk: the mouth works for dur seconds. With `text` its shapes follow the letters; without, a seeded run of syllables.
  H.talk = function (dur, o) {
    o = o || {};
    var at = o.at || 0;
    var rand = tk.rng(o.seed === undefined ? (o.text || 'talk') + ':' + at : o.seed);
    var list = visemes(o.text, dur, rand);
    var rest = o.rest || EMOTIONS[this.state.emotion].mouth || this.rest();
    var body = dur - 0.12;
    var t = at;
    for (var i = 0; i < list.length; i++) {
      var next = r3(at + body * (i + 1) / list.length);
      morphMouth(this, list[i], t, r3(next - t), 'sine.inOut');
      t = next;
    }
    morphMouth(this, rest, t, 0.12, 'sine.out');
    return this;
  };

  H.emotion = function (name, o) {
    o = o || {};
    var at = o.at || 0;
    var dur = o.dur === undefined ? 0.3 : o.dur;
    var to = EMOTIONS[name];
    if (!to) throw new Error('tk.rig: emotion is happy, surprised, sad or neutral');
    var was = this.state.emotion;
    var shape = to.mouth || this.rest();
    morphMouth(this, shape, at, dur, 'power2.out');
    var b = BROWS[name], a = BROWS[was];
    ft(this.P('browL', 'browR'), { y: a.y }, { y: b.y, duration: dur, ease: 'power2.out' }, at);
    ft(this.P('browL'), { rotation: a.l }, { rotation: b.l, duration: dur, ease: 'power2.out' }, at);
    ft(this.P('browR'), { rotation: a.r }, { rotation: b.r, duration: dur, ease: 'power2.out' }, at);
    ft(this.P('cheekL', 'cheekR'), { opacity: EMOTIONS[was].cheek }, { opacity: to.cheek, duration: dur, ease: 'power1.out' }, at);
    this.state.emotion = name;
    if (o.hold !== undefined && name !== 'neutral') this.emotion('neutral', { at: r3(at + dur + o.hold), dur: dur });
    return this;
  };

  H.happy = function (o) { return this.emotion('happy', o); };
  H.surprised = function (o) { return this.emotion('surprised', o); };

  // ---- the body ----

  function swing(hero, parts, sign, amp, at, dur, n, ease) {
    var h = dur / n;
    var pts = [0], times = [r3(at)];
    for (var i = 0; i < n; i++) {
      pts.push(sign * amp * (i % 2 ? -1 : 1));
      times.push(r3(at + (i === 0 ? h / 2 : h / 2 + h * i)));
    }
    pts.push(0);
    times.push(r3(at + dur));
    tk.chain(parts, 'rotation', pts, times, ease || 'sine.inOut');
  }

  // walk(from, to, dur, {at}): the hero crosses from one place to another (x of the drawing, "-60vw") with a step, a bob, arms.
  H.walk = function (from, to, dur, o) {
    o = o || {};
    var at = o.at || 0;
    var period = o.step || 0.5;
    var n = Math.max(2, 2 * Math.round(dur / period));
    var cfg = this.cfg;
    var dirSign = pxOf(to) >= pxOf(from) ? 1 : -1;
    if (this.state.x0 === null) {
      // the first walk puts the hero at its start from the first frame
      this.state.x0 = pxOf(from);
      tl().fromTo(this.el, { x: from }, { x: to, duration: dur, ease: o.ease || 'sine.inOut' }, r3(at));
    } else {
      ft(this.el, { x: from }, { x: to, duration: dur, ease: o.ease || 'sine.inOut' }, at);
    }
    this.state.x1 = pxOf(to);
    var A = cfg.swing || 22;
    swing(this, this.P('legL'), 1, A, at, dur, n);
    swing(this, this.P('legR'), -1, A, at, dur, n);
    swing(this, this.P('armL'), -1, A * 0.7, at, dur, n);
    swing(this, this.P('armR'), 1, A * 0.7, at, dur, n);
    var h = dur / n;
    var bob = cfg.bob || 7;
    var root = this.P('root');
    for (var i = 0; i < n; i++) {
      var t = at + h * i;
      ft(root, { y: 0 }, { y: -bob, duration: r3(h / 2), ease: 'sine.out' }, t);
      ft(root, { y: -bob }, { y: 0, duration: r3(h / 2), ease: 'sine.in' }, r3(t + h / 2));
    }
    var body = this.P('body');
    if (cfg.kind === 'round') {
      var amp = cfg.lean || 4;
      var pts = [0], times = [r3(at)];
      for (var k = 0; k < n; k++) { pts.push(amp * (k % 2 ? 1 : -1)); times.push(r3(at + (k === 0 ? h / 2 : h / 2 + h * k))); }
      pts.push(0); times.push(r3(at + dur));
      tk.chain(body, 'rotation', pts, times, 'sine.inOut');
    } else {
      var lean = (cfg.lean || 3) * dirSign;
      var ramp = Math.min(0.3, dur / 3);
      tk.chain(body, 'rotation', [0, lean, lean, 0], [r3(at), r3(at + ramp), r3(at + dur - ramp), r3(at + dur)], 'sine.inOut');
    }
    return this;
  };

  H.nod = function (o) {
    o = o || {};
    var at = o.at || 0;
    var times = o.times || 2;
    var pts = [0], ts = [r3(at)];
    for (var i = 0; i < times; i++) {
      pts.push(8, 0);
      ts.push(r3(at + 0.17 + i * 0.36), r3(at + 0.36 + i * 0.36));
    }
    tk.chain(this.P('head'), 'rotation', pts, ts, 'sine.inOut');
    return this;
  };

  H.shrug = function (o) {
    o = o || {};
    var at = o.at || 0;
    var hold = o.hold === undefined ? 0.8 : o.hold;
    var up = 0.28, down = 0.35;
    var t1 = r3(at + up), t2 = r3(t1 + hold), t3 = r3(t2 + down);
    tk.chain(this.P('armL'), 'rotation', [0, 16, 16, 0], [at, t1, t2, t3], 'power2.out');
    tk.chain(this.P('armR'), 'rotation', [0, -16, -16, 0], [at, t1, t2, t3], 'power2.out');
    tk.chain(this.P('foreL'), 'rotation', [0, 46, 46, 0], [at, t1, t2, t3], 'power2.out');
    tk.chain(this.P('foreR'), 'rotation', [0, -46, -46, 0], [at, t1, t2, t3], 'power2.out');
    tk.chain(this.P('body'), 'y', [0, -8, -8, 0], [at, t1, t2, t3], 'power2.out');
    tk.chain(this.P('head'), 'rotation', [0, -7, -7, 0], [at, t1, t2, t3], 'power2.out');
    return this;
  };

  // wave: the arm goes up, the forearm waves, the arm comes down.
  H.wave = function (o) {
    o = o || {};
    var at = o.at || 0;
    var dur = o.dur === undefined ? 1.6 : o.dur;
    var side = o.arm === 'L' ? 'L' : 'R';
    var s = side === 'L' ? 1 : -1;
    var up = (this.cfg.up || 148) * s;
    var arm = this.P('arm' + side);
    var fore = this.P('fore' + side);
    var raise = 0.4, lower = 0.45;
    var tUp = r3(at + raise), tDown = r3(at + dur - lower);
    var wag = (this.cfg.wag || 22) * s;
    var n = Math.max(2, Math.round((tDown - tUp) / 0.26));
    var pts = [0], ts = [tUp];
    for (var i = 0; i < n; i++) { pts.push(wag * (i % 2 ? 1 : -1)); ts.push(r3(tUp + (tDown - tUp) * (i + 0.5) / n)); }
    pts.push(0); ts.push(tDown);
    if (fore.length) {
      tk.chain(arm, 'rotation', [0, up, up, 0], [r3(at), tUp, tDown, r3(at + dur)], 'power2.inOut');
      tk.chain(fore, 'rotation', pts, ts, 'sine.inOut');
    } else {
      // an arm with no forearm waves as a whole, about the raised position.
      var p2 = [0, up], t2 = [r3(at), tUp];
      for (var k = 0; k < n; k++) { p2.push(up + wag * (k % 2 ? 1 : -1) * 0.6); t2.push(ts[k + 1]); }
      p2.push(up, 0); t2.push(tDown, r3(at + dur));
      tk.chain(arm, 'rotation', p2, t2, 'sine.inOut');
    }
    tk.chain(this.P('head'), 'rotation', [0, 5 * s, 5 * s, 0], [r3(at), tUp, tDown, r3(at + dur)], 'sine.inOut');
    return this;
  };

  // point(target, {at, arm, hold, look}): an arm goes straight to the target (a selector, an element, {x, y} on the screen or {angle}).
  H.point = function (target, o) {
    o = o || {};
    var at = o.at || 0;
    var hold = o.hold === undefined ? 1.4 : o.hold;
    var dur = o.dur === undefined ? 0.45 : o.dur;
    // Where the hero stands when it points: now it is where its walk starts, and it will be where the walk ends.
    var shift = this.state.x1 === null ? 0 : this.state.x1 - this.state.x0;
    var rotation;
    var side = o.arm;
    var armsGeom = ['L', 'R'].map(function (sd) {
      var a = part(this.el, 'arm' + sd)[0];
      var pv = a.getAttribute('data-pivot').split(' ').map(parseFloat);
      var tip = a.getAttribute('data-tip').split(' ').map(parseFloat);
      return { side: sd, el: a, pv: pv, rest: Math.atan2(tip[1] - pv[1], tip[0] - pv[0]) * 180 / Math.PI };
    }, this);
    if (target && typeof target === 'object' && !target.nodeType && target.angle !== undefined) {
      side = side || (Math.cos(target.angle * Math.PI / 180) >= 0 ? 'R' : 'L');
      rotation = target.angle - armsGeom[side === 'L' ? 0 : 1].rest;
    } else {
      var to = centerOf(target);
      if (!side) {
        var mid = screenPoint(this.el, 200, 300).x + shift;
        side = to.x >= mid ? 'R' : 'L';
      }
      var geo = armsGeom[side === 'L' ? 0 : 1];
      var from = screenPoint(this.el, geo.pv[0], geo.pv[1]);
      from.x += shift;
      rotation = Math.atan2(to.y - from.y, to.x - from.x) * 180 / Math.PI - geo.rest;
    }
    rotation = ((rotation + 540) % 360) - 180;
    var arm = this.P('arm' + side);
    var fore = this.P('fore' + side);
    var bend = fore.length ? parseFloat(fore[0].getAttribute('data-bend') || '0') : 0;
    var t1 = r3(at + dur), t2 = r3(t1 + hold), t3 = r3(t2 + 0.4);
    tk.chain(arm, 'rotation', [0, rotation, rotation, 0], [r3(at), t1, t2, t3], 'power2.inOut');
    if (fore.length) tk.chain(fore, 'rotation', [0, bend, bend, 0], [r3(at), t1, t2, t3], 'power2.inOut');
    if (o.look !== false && target && !(typeof target === 'object' && !target.nodeType && target.angle !== undefined)) {
      this.look(target, { at: at, dur: dur });
      this.look('center', { at: t2, dur: 0.3 });
    }
    return this;
  };

  // jump: crouch, spring, hang, land, settle; the shadow shrinks; `arms` throws them up.
  H.jump = function (o) {
    o = o || {};
    var at = o.at || 0;
    var height = o.height === undefined ? 90 : o.height;
    var air = o.dur === undefined ? 0.7 : o.dur;
    var root = this.P('root');
    var t0 = r3(at), t1 = r3(at + 0.16), t2 = r3(t1 + 0.1), t3 = r3(t1 + air), t4 = r3(t3 + 0.08), t5 = r3(t4 + 0.36);
    tk.chain(root, 'scaleY', [1, 0.86, 1.08, 1.0, 0.86, 1], [t0, t1, t2, t3, t4, t5], 'sine.inOut');
    tk.chain(root, 'scaleX', [1, 1.1, 0.94, 1.0, 1.12, 1], [t0, t1, t2, t3, t4, t5], 'sine.inOut');
    var mid = r3(t1 + air / 2);
    ft(root, { y: 0 }, { y: -height, duration: r3(mid - t1), ease: 'power2.out' }, t1);
    ft(root, { y: -height }, { y: 0, duration: r3(t3 - mid), ease: 'power2.in' }, mid);
    var shadow = this.P('shadow');
    ft(shadow, { scaleX: 1, opacity: 1 }, { scaleX: 0.7, opacity: 0.55, duration: r3(mid - t1), ease: 'power2.out' }, t1);
    ft(shadow, { scaleX: 0.7, opacity: 0.55 }, { scaleX: 1, opacity: 1, duration: r3(t3 - mid), ease: 'power2.in' }, mid);
    if (o.arms) {
      var A = (this.cfg.up || 148) * 0.8;
      tk.chain(this.P('armL'), 'rotation', [0, A, A, 0], [t1, mid, t3, t5], 'power2.out');
      tk.chain(this.P('armR'), 'rotation', [0, -A, -A, 0], [t1, mid, t3, t5], 'power2.out');
    }
    return this;
  };

  H.cheer = function (o) {
    o = o || {};
    this.emotion('happy', { at: o.at, hold: o.hold });
    return this.jump({ at: o.at, arms: true, height: o.height });
  };

  // the hero's place on the page, for a walk that is not from and to the drawing's origin.
  H.stand = function (x) {
    tk.start(this.el, { x: x });
    this.state.x0 = this.state.x1 = pxOf(x);
    return this;
  };

  function rig(target) {
    return new Hero(target);
  }
  rig.Hero = Hero;
  rig.pivots = pivots;
  tk.rig = rig;
  tk.pivots = pivots;
  tk.part = part;
})();
