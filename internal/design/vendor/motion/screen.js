// A recording of an interface in a scene (HRN-627). `harnessly design video record` shoots a site at twice the pixels of the
// final video and writes a clip and a log; this draws them into the scene as a function of the scene's time: the clip is
// sought frame by frame, a cursor is drawn over it along the logged path, a click rings, and the camera drives to the place
// of a click or of typing and back. The window, the cursor, the camera and the time are all drawn here, so none of them needs
// the interface shot again.
//
//   tkScreen(el, {src, log, at, ...}) -> {ready, seek, ...}
//     el      the scene's box the window goes in (a block with a size; it is emptied)
//     src     the clip, log  its log (both from `design video record`, as the project's relative paths)
//     at      scene seconds when the clip starts (default 0)
//   Time (edit here, no new recording):
//     speed   1 plays at the shot's speed, 2 twice as fast
//     trim    [from, to] clip seconds kept (default all)
//     cuts    [[from, to], ...] clip seconds cut out, e.g. a wait that is not worth showing
//   Window:
//     frame   'window' (default: a browser window with a bar, shadow and round corners) | 'none' (the clip fills el)
//     chrome  'light' | 'dark' (the bar; default is the page's theme)
//     url     true (the logged address without its port) | false | 'harnessly.ru/agents' (the text to show instead of the address)
//     pad     the margin round the window, % of the box's shorter side (default 5.5)
//     intro   the window comes in over the first 0.6 s (default true)
//   Cursor:
//     cursor  true | false   size  1.3 is the default   hand  true: a hand over links and buttons   ring  false: no ring on a click
//   Camera:
//     camera  'auto' (default: drives to every click and typing) | false | [{at, x, y, zoom, dur}] by hand (scene seconds, x and y in
//             the page's CSS pixels; no x and y is the whole page again)
//     zoom    1.7: how close the camera goes   lead  0.9: seconds ahead of a click it starts   hold  0.9: seconds it stays after
//     skip    [numbers] clicks, hovers and typing the camera ignores, counted from 0 in the log's order
//   tkScreen.duration(log, opts) is how long the clip is in the scene, in seconds, after the edits above (async).
(function () {
  'use strict';

  var CSS_ID = 'tks-style';
  var CSS = [
    '.tks-stage{position:relative;width:100%;height:100%;overflow:hidden;background:var(--tks-bg,radial-gradient(120% 120% at 20% 10%,color-mix(in srgb,var(--deck-bg,#eef0f4) 82%,#6c7cff 18%),color-mix(in srgb,var(--deck-bg,#eef0f4) 92%,#000 8%)))}',
    '.tks-window{position:absolute;overflow:hidden;background:#fff;will-change:transform,opacity}',
    '.tks-bar{position:absolute;left:0;right:0;top:0;display:flex;align-items:center;box-sizing:border-box}',
    '.tks-dots{display:flex;gap:.55em}.tks-dots i{display:block;border-radius:50%}',
    '.tks-url{position:absolute;left:50%;top:50%;transform:translate(-50%,-50%);overflow:hidden;white-space:nowrap;text-overflow:ellipsis;text-align:center;font-family:var(--deck-font,system-ui,sans-serif)}',
    '.tks-view{position:absolute;left:0;display:block}',
    '.tks-over{position:absolute;left:0;overflow:hidden;pointer-events:none}',
    '.tks-cur{position:absolute;left:0;top:0;transform-origin:0 0;filter:drop-shadow(0 1.4px 2.2px rgba(0,0,0,.42))}',
    '.tks-ring{position:absolute;left:0;top:0;border-radius:50%;box-sizing:border-box}',
    '.tks-media{position:absolute;width:2px;height:2px;left:0;top:0;opacity:.01;pointer-events:none}',
  ].join('');

  function css() {
    if (document.getElementById(CSS_ID)) return;
    var s = document.createElement('style');
    s.id = CSS_ID;
    s.textContent = CSS;
    (document.head || document.documentElement).appendChild(s);
  }

  function clamp(v, a, b) { return v < a ? a : v > b ? b : v; }
  function smoother(p) { p = clamp(p, 0, 1); return p * p * p * (p * (p * 6 - 15) + 10); }
  function lerp(a, b, p) { return a + (b - a) * p; }

  // ---- time: the scene's t to the clip's ----

  function timeMap(o, dur) {
    var from = o.trim ? o.trim[0] : 0;
    var to = o.trim ? Math.min(o.trim[1], dur) : dur;
    var cuts = (o.cuts || []).map(function (c) { return [Math.max(from, c[0]), Math.min(to, c[1])]; })
      .filter(function (c) { return c[1] > c[0]; }).sort(function (a, b) { return a[0] - b[0]; });
    var kept = [];
    var at = from;
    cuts.forEach(function (c) {
      if (c[0] > at) kept.push([at, c[0]]);
      at = Math.max(at, c[1]);
    });
    if (to > at) kept.push([at, to]);
    var length = kept.reduce(function (s, k) { return s + (k[1] - k[0]); }, 0);
    var speed = o.speed > 0 ? o.speed : 1;
    return {
      length: length / speed,
      // sec: seconds since the clip's start in the scene -> seconds in the clip
      clip: function (sec) {
        var s = Math.max(0, sec) * speed;
        for (var i = 0; i < kept.length; i++) {
          var l = kept[i][1] - kept[i][0];
          if (s <= l) return kept[i][0] + s;
          s -= l;
        }
        return kept.length ? kept[kept.length - 1][1] : 0;
      },
    };
  }

  // ---- the cursor's path ----

  function pathAt(cur, ms) {
    var n = cur.length;
    if (!n) return null;
    if (ms <= cur[0][0]) return [cur[0][1], cur[0][2]];
    if (ms >= cur[n - 1][0]) return [cur[n - 1][1], cur[n - 1][2]];
    var lo = 0;
    var hi = n - 1;
    while (hi - lo > 1) {
      var mid = (lo + hi) >> 1;
      if (cur[mid][0] <= ms) lo = mid; else hi = mid;
    }
    var p0 = cur[Math.max(0, lo - 1)];
    var p1 = cur[lo];
    var p2 = cur[hi];
    var p3 = cur[Math.min(n - 1, hi + 1)];
    var u = (ms - p1[0]) / Math.max(1e-6, p2[0] - p1[0]);
    var u2 = u * u;
    var u3 = u2 * u;
    function cr(a, b, c, d) { return 0.5 * ((2 * b) + (-a + c) * u + (2 * a - 5 * b + 4 * c - d) * u2 + (-a + 3 * b - 3 * c + d) * u3); }
    return [cr(p0[1], p1[1], p2[1], p3[1]), cr(p0[2], p1[2], p2[2], p3[2])];
  }

  // ---- the camera ----

  // shots: where the camera is at which moment (ms of the clip): {t, x, y, z}; between two it eases.
  function autoCamera(lg, o) {
    var PW = lg.viewport[0];
    var PH = lg.viewport[1];
    var zoom = o.zoom || 1.7;
    var lead = (o.lead === undefined ? 0.9 : o.lead) * 1000;
    var hold = (o.hold === undefined ? 0.9 : o.hold) * 1000;
    var skip = {};
    (o.skip || []).forEach(function (n) { skip[n] = true; });
    var points = [];
    var n = 0;
    var lastBox = null;
    (lg.events || []).forEach(function (e) {
      if (e.type !== 'click' && e.type !== 'type' && e.type !== 'hover') return;
      n++;
      var box = e.bbox || (e.type === 'type' ? lastBox : null);
      if (e.bbox) lastBox = e.bbox;
      if (skip[n - 1] || e.x === undefined) return;
      // a wide element needs less zoom to fit; a small one gets the whole of it
      var big = box ? Math.max(box[2], box[3] * 1.6) : 0;
      var z = box ? clamp((PW * 0.8) / Math.max(big, 120), 1.4, zoom) : zoom;
      points.push({ t: e.t, end: e.end || e.t, x: e.x, y: e.y, z: Math.min(zoom, z) });
    });
    var keys = [{ t: 0, x: PW / 2, y: PH / 2, z: 1 }];
    var open = false;
    function push(t, x, y, z) {
      keys.push({ t: Math.max(keys[keys.length - 1].t + 1, t), x: x, y: y, z: z });
    }
    points.forEach(function (p, i) {
      var arrive = p.t - 250;
      if (!open) push(arrive - lead, PW / 2, PH / 2, 1);
      push(arrive, p.x, p.y, p.z);
      open = true;
      var holdEnd = Math.max(p.end, p.t) + hold;
      var next = points[i + 1];
      if (next && next.t - 250 - holdEnd < 2200) {
        // the next place is close in time: the camera goes on to it without going back out
        if (holdEnd < next.t - 450) push(holdEnd, p.x, p.y, p.z);
      } else {
        push(holdEnd, p.x, p.y, p.z);
        push(holdEnd + 800, PW / 2, PH / 2, 1);
        open = false;
      }
    });
    return keys;
  }

  function handCamera(lg, o, atSec, map) {
    var PW = lg.viewport[0];
    var PH = lg.viewport[1];
    var keys = [{ t: 0, x: PW / 2, y: PH / 2, z: 1 }];
    o.camera.slice().sort(function (a, b) { return a.at - b.at; }).forEach(function (k) {
      var t = map.clip(k.at - atSec) * 1000;
      var dur = (k.dur === undefined ? 1.0 : k.dur) * 1000;
      var last = keys[keys.length - 1];
      var whole = k.x === undefined || k.y === undefined;
      keys.push({ t: Math.max(last.t + 1, t), x: last.x, y: last.y, z: last.z });
      keys.push({ t: Math.max(keys[keys.length - 1].t + 1, t + dur), x: whole ? PW / 2 : k.x, y: whole ? PH / 2 : k.y, z: whole ? 1 : (k.zoom || o.zoom || 1.7) });
    });
    return keys;
  }

  function camAt(keys, ms) {
    var n = keys.length;
    if (!n) return { x: 0, y: 0, z: 1 };
    if (ms <= keys[0].t) return keys[0];
    if (ms >= keys[n - 1].t) return keys[n - 1];
    var lo = 0;
    var hi = n - 1;
    while (hi - lo > 1) {
      var mid = (lo + hi) >> 1;
      if (keys[mid].t <= ms) lo = mid; else hi = mid;
    }
    var p = smoother((ms - keys[lo].t) / (keys[hi].t - keys[lo].t));
    return { x: lerp(keys[lo].x, keys[hi].x, p), y: lerp(keys[lo].y, keys[hi].y, p), z: lerp(keys[lo].z, keys[hi].z, p) };
  }

  // ---- the component ----

  function load(url) {
    return fetch(url).then(function (r) {
      if (!r.ok) throw new Error('tkScreen: ' + url + ' ' + r.status);
      return r.json();
    });
  }

  function screen(el, o) {
    o = o || {};
    if (!el) throw new Error('tkScreen needs the element to draw in');
    if (!o.src || !o.log) throw new Error('tkScreen needs src and log (from design video record)');
    css();
    var atSec = o.at || 0;
    var lg = null;
    var map = null;
    var keys = null;
    var video = null;
    var canvas = null;
    var ctx = null;
    var geo = null;
    var parts = {};
    var drawn = { frame: -2, key: '' };
    var token = 0;

    function build() {
      el.textContent = '';
      var stage = document.createElement('div');
      stage.className = 'tks-stage';
      var win = document.createElement('div');
      win.className = 'tks-window';
      var bar = null;
      if (o.frame !== 'none') {
        bar = document.createElement('div');
        bar.className = 'tks-bar';
        var dots = document.createElement('div');
        dots.className = 'tks-dots';
        for (var i = 0; i < 3; i++) dots.appendChild(document.createElement('i'));
        bar.appendChild(dots);
        if (o.url !== false) {
          var url = document.createElement('div');
          url.className = 'tks-url';
          bar.appendChild(url);
          parts.url = url;
        }
        win.appendChild(bar);
        parts.dots = dots;
      }
      canvas = document.createElement('canvas');
      canvas.className = 'tks-view';
      win.appendChild(canvas);
      var over = document.createElement('div');
      over.className = 'tks-over';
      var ring = document.createElement('div');
      ring.className = 'tks-ring';
      var cur = document.createElement('div');
      cur.className = 'tks-cur';
      cur.innerHTML = '<svg viewBox="0 0 24 28" width="24" height="28" aria-hidden="true"><path class="tks-arrow" d="M2.5 1.8 L2.5 21.2 L7.2 16.9 L10.3 24.6 L13.6 23.2 L10.5 15.7 L16.9 15.7 Z" fill="#fff" stroke="#14161c" stroke-width="1.7" stroke-linejoin="round"/>'
        + '<path class="tks-handp" d="M9.2 2.4 C8 2.4 7.2 3.3 7.2 4.5 L7.2 12.6 L6.2 11.6 C5.4 10.8 4.2 10.8 3.5 11.5 C2.8 12.2 2.8 13.2 3.5 14 L8.1 19.6 C9 20.8 10.4 21.5 12 21.5 L15.4 21.5 C17.8 21.5 19.5 19.7 19.5 17.4 L19.5 11.6 C19.5 10.6 18.8 9.9 17.9 9.9 C17.5 9.9 17.1 10 16.8 10.3 C16.6 9.6 16 9.1 15.2 9.1 C14.7 9.1 14.3 9.3 14 9.6 C13.8 8.9 13.1 8.4 12.4 8.4 C12 8.4 11.7 8.5 11.4 8.7 L11.4 4.5 C11.4 3.3 10.4 2.4 9.2 2.4 Z" fill="#fff" stroke="#14161c" stroke-width="1.5" stroke-linejoin="round" style="display:none"/></svg>';
      over.appendChild(ring);
      over.appendChild(cur);
      win.appendChild(over);
      stage.appendChild(win);
      video = document.createElement('video');
      video.className = 'tks-media';
      video.muted = true;
      video.playsInline = true;
      video.preload = 'auto';
      stage.appendChild(video);
      el.appendChild(stage);
      parts.stage = stage;
      parts.win = win;
      parts.bar = bar;
      parts.over = over;
      parts.ring = ring;
      parts.cur = cur;
      parts.hand = cur.querySelector('.tks-handp');
      parts.arrow = cur.querySelector('.tks-arrow');
      ctx = canvas.getContext('2d');
    }

    function layout() {
      var W = el.clientWidth || 1920;
      var H = el.clientHeight || 1080;
      var dpr = window.devicePixelRatio || 1;
      var u = Math.min(W, H) / 100;
      var win = o.frame !== 'none';
      var pad = win ? (o.pad === undefined ? 5.5 : o.pad) * u : 0;
      var barH = win ? 4.4 * u : 0;
      var aspect = lg.width / lg.height;
      var availW = W - 2 * pad;
      var availH = H - 2 * pad - barH;
      var vw = Math.min(availW, availH * aspect);
      var vh = vw / aspect;
      if (!win) {
        // cover the box
        vw = Math.max(W, H * aspect);
        vh = vw / aspect;
      }
      var winW = win ? vw : W;
      var winH = win ? vh + barH : H;
      geo = { W: W, H: H, u: u, dpr: dpr, vw: win ? vw : W, vh: win ? vh : H, barH: barH, left: win ? (W - winW) / 2 : 0, top: win ? (H - winH) / 2 : 0, winW: winW, winH: winH };
      var dark = (o.chrome || lg.theme) === 'dark';
      var w = parts.win.style;
      w.left = geo.left + 'px';
      w.top = geo.top + 'px';
      w.width = geo.winW + 'px';
      w.height = geo.winH + 'px';
      w.borderRadius = win ? 1.5 * u + 'px' : '0';
      w.boxShadow = win ? '0 ' + 2.6 * u + 'px ' + 7 * u + 'px rgba(8,12,28,.38), 0 ' + 0.35 * u + 'px ' + 1.1 * u + 'px rgba(8,12,28,.34), 0 0 0 ' + Math.max(1, 0.1 * u) + 'px rgba(255,255,255,.14)' : 'none';
      w.background = dark ? '#1c1e24' : '#fff';
      if (parts.bar) {
        var b = parts.bar.style;
        b.height = barH + 'px';
        b.background = dark ? '#262932' : '#e9ebf0';
        b.borderBottom = Math.max(1, 0.08 * u) + 'px solid ' + (dark ? '#14161b' : '#d3d6de');
        b.padding = '0 ' + 1.6 * u + 'px';
        b.fontSize = 1.55 * u + 'px';
        var ds = parts.dots.children;
        var dc = dark ? ['#ff5f57', '#febc2e', '#28c840'] : ['#ff5f57', '#febc2e', '#28c840'];
        for (var i = 0; i < ds.length; i++) {
          ds[i].style.width = ds[i].style.height = 1.25 * u + 'px';
          ds[i].style.background = dc[i];
        }
        parts.dots.style.fontSize = u + 'px';
        if (parts.url) {
          var us = parts.url.style;
          us.height = 2.8 * u + 'px';
          us.lineHeight = 2.8 * u + 'px';
          us.width = Math.min(56 * u, geo.winW * 0.5) + 'px';
          us.borderRadius = 1.4 * u + 'px';
          us.background = dark ? '#171920' : '#fff';
          us.color = dark ? '#c7cad4' : '#4a4f5c';
          us.fontSize = 1.5 * u + 'px';
          us.padding = '0 ' + 1.2 * u + 'px';
        }
      }
      var cs = canvas.style;
      cs.top = geo.barH + 'px';
      cs.width = geo.vw + 'px';
      cs.height = geo.vh + 'px';
      canvas.width = Math.max(2, Math.round(geo.vw * dpr));
      canvas.height = Math.max(2, Math.round(geo.vh * dpr));
      var os = parts.over.style;
      os.top = geo.barH + 'px';
      os.width = geo.vw + 'px';
      os.height = geo.vh + 'px';
      drawn.key = '';
    }

    function urlAt(ms) {
      if (typeof o.url === 'string') return o.url;
      var u = '';
      (lg.events || []).forEach(function (e) { if (e.type === 'open' && e.t <= ms + 1 && e.url) u = e.url; });
      try {
        var p = new URL(u);
        return p.hostname + (p.pathname === '/' ? '' : p.pathname);
      } catch (err) { return u; }
    }

    // draw puts the clip's moment clipSec on the canvas and the overlay over it. Everything here is a function of clipSec
    // and the scene's own time.
    function paint(clipSec, sceneSec) {
      var ms = clipSec * 1000;
      var cam = o.camera === false ? { x: lg.viewport[0] / 2, y: lg.viewport[1] / 2, z: 1 } : camAt(keys, ms);
      var PW = lg.viewport[0];
      var PH = lg.viewport[1];
      var pw = PW / cam.z;
      var ph = PH / cam.z;
      var x0 = clamp(cam.x - pw / 2, 0, PW - pw);
      var y0 = clamp(cam.y - ph / 2, 0, PH - ph);
      var s = lg.scale;
      ctx.imageSmoothingEnabled = true;
      ctx.imageSmoothingQuality = 'high';
      ctx.drawImage(video, x0 * s, y0 * s, pw * s, ph * s, 0, 0, canvas.width, canvas.height);
      var k = geo.vw / pw; // screen px of the box per CSS px of the page
      // the window comes in
      var w = parts.win.style;
      if (o.intro === false || o.frame === 'none') {
        w.opacity = '1';
        w.transform = '';
      } else {
        var p = smoother(sceneSec / 0.6);
        w.opacity = String(Math.round(p * 1000) / 1000);
        w.transform = 'translateY(' + (1 - p) * 1.6 * geo.u + 'px) scale(' + (0.97 + 0.03 * p) + ')';
      }
      if (parts.url) parts.url.textContent = urlAt(ms);
      // the cursor
      var c = o.cursor === false ? null : pathAt(lg.cursor || [], ms);
      var cur = parts.cur.style;
      if (c) {
        var size = (o.size || 1.3) * geo.u * 1.9 / 24 * (1 + (cam.z - 1) * 0.35);
        var press = 0;
        var hand = false;
        (lg.events || []).forEach(function (e) {
          if (e.type === 'click') {
            var d = ms - e.t;
            if (d >= -40 && d < 260) press = Math.max(press, d < 90 ? smoother((d + 40) / 130) : 1 - smoother((d - 90) / 170));
          }
          if (o.hand && e.bbox && (e.tag === 'a' || e.tag === 'button' || e.tag === 'summary') && ms >= e.t - 450 && ms <= (e.end || e.t) + 150) hand = true;
        });
        var sc = size * (1 - 0.14 * press);
        var cx = (c[0] - x0) * k;
        var cy = (c[1] - y0) * k;
        cur.display = 'block';
        cur.transform = 'translate(' + cx + 'px,' + cy + 'px) scale(' + sc + ') translate(' + (hand ? -8 : -2.5) + 'px,' + (hand ? -3 : -1.8) + 'px)';
        parts.hand.style.display = hand ? 'block' : 'none';
        parts.arrow.style.display = hand ? 'none' : 'block';
      } else {
        cur.display = 'none';
      }
      // the ring of a click
      var ring = parts.ring.style;
      ring.opacity = '0';
      if (o.ring !== false) {
        (lg.events || []).forEach(function (e) {
          if (e.type !== 'click' || e.x === undefined) return;
          var d = (ms - e.t) / 520;
          if (d < 0 || d >= 1) return;
          var r = (1.4 + 4.4 * (1 - Math.pow(1 - d, 3))) * geo.u * (1 + (cam.z - 1) * 0.3);
          ring.opacity = String(Math.min(1, 1.4 * (1 - d) * (1 - d)));
          ring.width = ring.height = 2 * r + 'px';
          ring.border = Math.max(3, 0.45 * geo.u) + 'px solid #4f7dff';
          ring.background = 'rgba(79,125,255,.22)';
          ring.transform = 'translate(' + ((e.x - x0) * k - r) + 'px,' + ((e.y - y0) * k - r) + 'px)';
        });
      }
    }

    function seekTo(ms) {
      var sceneSec = ms / 1000 - atSec;
      var clipSec = map.clip(sceneSec);
      var frame = clamp(Math.round(clipSec * lg.fps - 0.0001), 0, lg.frames - 1);
      var my = ++token;
      var want = (frame + 0.5) / lg.fps;
      var finish = function () {
        if (my !== token) return false;
        // the overlay uses the exact clip time, the picture the frame under it
        paint(Math.min(clipSec, (lg.frames - 0.01) / lg.fps), Math.max(0, sceneSec));
        drawn.frame = frame;
        return true;
      };
      if (drawn.frame === frame && video.readyState >= 2) return Promise.resolve(finish());
      if (Math.abs(video.currentTime - want) < 0.5 / lg.fps && video.readyState >= 2 && !video.seeking) return Promise.resolve(finish());
      return new Promise(function (resolve) {
        var done = false;
        var timer = 0;
        function ok() {
          if (done) return;
          done = true;
          clearTimeout(timer);
          video.removeEventListener('seeked', onSeeked);
          resolve(finish());
        }
        // 'seeked' comes before the new picture can be drawn from; the frame callback names the picture on its way to the
        // screen (mediaTime), so the canvas is drawn only when it is the one asked for.
        function watch() {
          video.requestVideoFrameCallback(function (now, meta) {
            if (done) return;
            if (Math.round(meta.mediaTime * lg.fps) === frame) ok(); else watch();
          });
        }
        function onSeeked() {
          if (!video.requestVideoFrameCallback) ok();
        }
        video.addEventListener('seeked', onSeeked);
        if (video.requestVideoFrameCallback) {
          watch();
          timer = setTimeout(ok, 4000); // a picture that never comes (the page is hidden) must not hold the scene
        }
        video.currentTime = want;
      });
    }

    var api = {
      name: 'screen',
      ready: function () {
        build();
        return load(o.log).then(function (j) {
          lg = j;
          map = timeMap(o, lg.frames / lg.fps);
          if (o.camera === false) keys = [];
          else if (Array.isArray(o.camera)) keys = handCamera(lg, o, atSec, map);
          else keys = autoCamera(lg, o);
          api.length = map.length;
          return new Promise(function (resolve, reject) {
            video.addEventListener('loadeddata', function () { resolve(); }, { once: true });
            video.addEventListener('error', function () { reject(new Error('tkScreen: the clip ' + o.src + ' did not load')); }, { once: true });
            video.src = o.src;
          });
        }).then(function () {
          layout();
          return seekTo(atSec * 1000);
        });
      },
      seek: function (ms) {
        if (!lg) return false;
        return seekTo(ms);
      },
      length: 0,
      relayout: function () { if (lg) layout(); },
    };
    if (window.tkVideo && window.tkVideo.register) window.tkVideo.register(api);
    return api;
  }

  screen.duration = function (logUrl, o) {
    return load(logUrl).then(function (j) { return timeMap(o || {}, j.frames / j.fps).length; });
  };

  window.tkScreen = screen;
  if (window.tk) window.tk.screen = screen; // when tk.js came first (it replaces window.tk if it comes later: use tkScreen)
})();
