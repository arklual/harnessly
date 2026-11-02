// Motion presets for a video's board (HRN-576): one vocabulary of movement on top of GSAP. Load it after gsap.min.js and
// call it inside tkVideo.motion. Every call adds to one timeline at a time you give, so the frame stays a function of t.
//   tk.enter(target, {at, from: 'up'|'down'|'left'|'right'|'scale'|'fade'|'mask', dur, stagger, ease})
//   tk.exit(target, {at, to: 'up'|'down'|'left'|'right'|'scale'|'fade', dur, ease})
//   tk.accent(target, {at, kind: 'pulse'|'nudge'|'wiggle', dur})
//   tk.camera(world, {at, dur, x, y, zoom, ease})     world is the wrapper that holds the scene: one move at a time
//   tk.draw(paths, {at, dur, stagger, ease})          needs DrawSVGPlugin.min.js
//   tk.text(target, {at, by: 'chars'|'words'|'lines', dur, stagger, from}) needs SplitText.min.js
//   tk.physical.drop(target, {at, height, bounces})   a fall with squash and stretch
//   tk.physical.hop(target, {at, height, dur})        a jump with anticipation, stretch and landing
//   tk.tl is the timeline; add your own tweens to it with tk.tl.to(target, vars, at).
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
      tl().set(target, { transformOrigin: '50% 100%', y: '-' + height }, 0);
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
      tl().set(target, { transformOrigin: '50% 100%' }, 0);
      tl().to(target, { scaleY: 0.8, scaleX: 1.12, duration: 0.1, ease: 'power1.out' }, at);
      tl().to(target, { scaleY: 1.1, scaleX: 0.94, duration: 0.08, ease: 'power1.out' }, at + 0.1);
      tl().to(target, { y: '-' + height, duration: dur / 2, ease: 'power2.out' }, at + 0.1);
      tl().to(target, { y: 0, duration: dur / 2, ease: 'power2.in' }, at + 0.1 + dur / 2);
      tl().to(target, { scaleY: 0.82, scaleX: 1.14, duration: 0.07, ease: 'power1.out' }, at + 0.1 + dur);
      tl().to(target, { scaleY: 1, scaleX: 1, duration: 0.2, ease: 'back.out(3)' }, at + 0.17 + dur);
      return tl();
    },
  };

  window.tk = {
    get tl() { return tl(); },
    enter: enter, exit: exit, accent: accent, camera: camera, draw: draw, text: text, physical: physical,
  };
})();
