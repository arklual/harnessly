// «Лазер»: a real laser. A small metal emitter sits in the top-right corner and aims a beam (wide bloom, glow, a
// white-hot core, a shimmer running along it, dust specks lit as it passes) at the newest line of the answer being
// written. Where it touches, a white-hot spot with a coloured bloom and heat haze burns a trail that cools like hot
// metal (white-yellow, orange, deep red, gone) and throws sparks (glowing streaks with gravity that bounce off the
// burnt line) and thin wisps of smoke. A new prompt is outlined once by the same hot head; when the answer is done a
// scan line sweeps down it. A built-in effect, and an example of the hx SDK (runtime.js).
const COOL = 1.5; // seconds a burn takes to cool down to nothing
const trails = []; // {pts: [{x, y, t, brk}], live}
const heads = []; // the hot points the beam is on: {k: 'w'|'t', id, a, x, y, tr, end, ...}
const sparks = [];
const smoke = [];
const dust = [];
let print = null; // the answer being printed: {id, p (px of the head's travel), fin}
const printed = new Set(); // answers already printed: never masked again
const LH = 22; // the line height the beam rasters by
let mask = null; // the rects still covered this frame
let scan = null; // {id, t}
let aim = 2.6; // the emitter's direction, radians
let spr = null; // baked sprites
let sprKey = '';

const clamp = (v, a = 0, b = 1) => (v < a ? a : v > b ? b : v);
const lerp = (a, b, k) => a + (b - a) * k;
const accentRgb = () => {
  const c = hx.colors.accent || '';
  const m = /rgba?\((\d+)[ ,]+(\d+)[ ,]+(\d+)/.exec(c);
  if (m) return [+m[1], +m[2], +m[3]];
  const h = /^#([0-9a-f]{6})$/i.exec(c);
  if (h) return [parseInt(h[1].slice(0, 2), 16), parseInt(h[1].slice(2, 4), 16), parseInt(h[1].slice(4), 16)];
  return [255, 60, 90];
};
// A colour of the accent, pulled toward white by k (0–1) or toward black (k < 0).
const tone = (k) => {
  const [r, g, b] = accentRgb();
  const f = (v) => Math.round(k >= 0 ? v + (255 - v) * k : v * (1 + k));
  return [f(r), f(g), f(b)];
};
const rgba = (c, a) => `rgba(${c[0]},${c[1]},${c[2]},${a})`;
const byId = (id) => hx.blocks.find((b) => b.id === id) || null;

// Hot metal: age 0–1 -> [r, g, b, alpha]. A light page needs deeper colours: white or yellow is invisible there.
const HOT_D = [
  [0, 255, 250, 232, 1],
  [0.12, 255, 224, 140, 1],
  [0.35, 255, 150, 52, 0.85],
  [0.6, 236, 72, 22, 0.6],
  [0.82, 140, 22, 12, 0.32],
  [1, 60, 6, 6, 0],
];
const HOT_L = [
  [0, 255, 190, 40, 1],
  [0.15, 255, 130, 20, 0.95],
  [0.45, 232, 64, 14, 0.85],
  [0.75, 150, 20, 14, 0.6],
  [1, 90, 12, 12, 0],
];
const heat = (u, light) => {
  const st = light ? HOT_L : HOT_D;
  u = clamp(u);
  let i = 1;
  while (i < st.length - 1 && u > st[i][0]) i++;
  const a = st[i - 1];
  const b = st[i];
  const k = (u - a[0]) / (b[0] - a[0]);
  return [lerp(a[1], b[1], k) | 0, lerp(a[2], b[2], k) | 0, lerp(a[3], b[3], k) | 0, lerp(a[4], b[4], k)];
};

const sprite = (n, draw) => {
  const c = document.createElement('canvas');
  c.width = c.height = n;
  draw(c.getContext('2d'), n);
  return c;
};
const bakeSprites = () => {
  const key = hx.colors.accent + hx.mode;
  if (key === sprKey) return;
  sprKey = key;
  const light = hx.mode === 'light';
  const rad = (g, stops) => stops.forEach(([o, c]) => g.addColorStop(o, c));
  spr = {
    // The coloured bloom around a hot point.
    glow: sprite(128, (c, n) => {
      const g = c.createRadialGradient(n / 2, n / 2, 0, n / 2, n / 2, n / 2);
      rad(g, [
        [0, rgba(tone(0.6), 1)],
        [0.14, rgba(tone(0.3), 0.6)],
        [0.4, rgba(tone(0), 0.2)],
        [0.7, rgba(tone(0), 0.05)],
        [1, rgba(tone(0), 0)],
      ]);
      c.fillStyle = g;
      c.fillRect(0, 0, n, n);
    }),
    // The warm and the deep red glow a burn gives off.
    emberO: sprite(64, (c, n) => {
      const g = c.createRadialGradient(n / 2, n / 2, 0, n / 2, n / 2, n / 2);
      rad(g, [
        [0, 'rgba(255,170,70,0.9)'],
        [0.4, 'rgba(255,120,40,0.35)'],
        [1, 'rgba(255,90,20,0)'],
      ]);
      c.fillStyle = g;
      c.fillRect(0, 0, n, n);
    }),
    emberR: sprite(64, (c, n) => {
      const g = c.createRadialGradient(n / 2, n / 2, 0, n / 2, n / 2, n / 2);
      rad(g, [
        [0, 'rgba(210,40,20,0.85)'],
        [0.45, 'rgba(170,25,15,0.3)'],
        [1, 'rgba(140,15,10,0)'],
      ]);
      c.fillStyle = g;
      c.fillRect(0, 0, n, n);
    }),
    // A white-hot glare.
    hot: sprite(64, (c, n) => {
      const g = c.createRadialGradient(n / 2, n / 2, 0, n / 2, n / 2, n / 2);
      rad(
        g,
        light
          ? [
              [0, 'rgba(255,240,170,1)'],
              [0.25, 'rgba(255,150,30,0.9)'],
              [0.6, 'rgba(230,60,20,0.35)'],
              [1, 'rgba(200,40,20,0)'],
            ]
          : [
              [0, 'rgba(255,255,250,1)'],
              [0.2, 'rgba(255,240,215,0.75)'],
              [0.55, 'rgba(255,200,150,0.18)'],
              [1, 'rgba(255,180,120,0)'],
            ],
      );
      c.fillStyle = g;
      c.fillRect(0, 0, n, n);
    }),
    // A puff of smoke: grey on a dark page, a dark grey on a light one.
    smoke: sprite(64, (c, n) => {
      const g = c.createRadialGradient(n / 2, n / 2, 0, n / 2, n / 2, n / 2);
      const s = light ? '60,62,72' : '176,182,198';
      rad(g, [
        [0, `rgba(${s},0.6)`],
        [0.45, `rgba(${s},0.24)`],
        [1, `rgba(${s},0)`],
      ]);
      c.fillStyle = g;
      c.fillRect(0, 0, n, n);
    }),
  };
};

hx.on('prompt', (e) => {
  if (!e.block) return;
  const tr = { pts: [], live: true, cool: COOL };
  trails.push(tr);
  heads.push({ k: 't', id: e.block.id, u: 0, a: 0, x: 0, y: 0, tr, end: false, sacc: 0, macc: 0, hot: 1 });
});
// An answer the beam has seen start is covered and printed line by line; one it has not is never touched.
hx.on('answer', (e) => {
  const id = e.block && e.block.id;
  if (!id || printed.has(id) || (print && print.id === id)) return;
  if (print) printed.add(print.id);
  print = { id, p: 0, fin: false, scanAfter: false };
});
const finish = (id) => {
  if (print && (!id || print.id === id)) {
    print.fin = true;
    print.scanAfter = true;
  } else if (id) scan = { id, t: 0 };
};
hx.on('answer-end', (e) => finish(e.block && e.block.id));
hx.on('state', (s) => {
  if (s.state !== 'working' && print && !print.fin) finish();
});

// A point at share u of the perimeter of a rounded box.
const around = (b, u) => {
  const r = Math.min(14, b.w / 2, b.h / 2);
  const sw = b.w - 2 * r;
  const sh = b.h - 2 * r;
  const ar = (Math.PI * r) / 2;
  let d = (((u % 1) + 1) % 1) * (2 * sw + 2 * sh + 4 * ar);
  const x1 = b.x + b.w - r;
  const y1 = b.y + b.h - r;
  const x0 = b.x + r;
  const y0 = b.y + r;
  if (d < sw) return [x0 + d, b.y];
  d -= sw;
  if (d < ar) return [x1 + Math.cos(-Math.PI / 2 + d / r) * r, y0 + Math.sin(-Math.PI / 2 + d / r) * r];
  d -= ar;
  if (d < sh) return [b.x + b.w, y0 + d];
  d -= sh;
  if (d < ar) return [x1 + Math.cos(d / r) * r, y1 + Math.sin(d / r) * r];
  d -= ar;
  if (d < sw) return [x1 - d, b.y + b.h];
  d -= sw;
  if (d < ar) return [x0 + Math.cos(Math.PI / 2 + d / r) * r, y1 + Math.sin(Math.PI / 2 + d / r) * r];
  d -= ar;
  if (d < sh) return [b.x, y1 - d];
  d -= sh;
  return [x0 + Math.cos(Math.PI + d / r) * r, y0 + Math.sin(Math.PI + d / r) * r];
};

const rr = (c, x, y, w, h, r) => {
  c.beginPath();
  c.moveTo(x + r, y);
  c.arcTo(x + w, y, x + w, y + h, r);
  c.arcTo(x + w, y + h, x, y + h, r);
  c.arcTo(x, y + h, x, y, r);
  c.arcTo(x, y, x + w, y, r);
  c.closePath();
};

const SC = 1.55; // the emitter's scale
const LENS = 38 * SC; // from the emitter's pivot to its lens
const px = () => hx.w - 52;
const py = () => 46;

// The emitter: a metal housing on a bracket. Local x runs along the beam, local +y is the lit upper side.
const device = (ctx, ang, a, light) => {
  const X = px();
  const Y = py();
  // The bracket to the corner.
  ctx.lineCap = 'round';
  ctx.strokeStyle = '#14171c';
  ctx.lineWidth = 11;
  ctx.beginPath();
  ctx.moveTo(X + 1, Y - 1);
  ctx.lineTo(hx.w + 8, -8);
  ctx.stroke();
  ctx.strokeStyle = '#4b5361';
  ctx.lineWidth = 7;
  ctx.stroke();
  ctx.strokeStyle = 'rgba(214,222,236,0.5)';
  ctx.lineWidth = 1.4;
  ctx.beginPath();
  ctx.moveTo(X - 1, Y - 3);
  ctx.lineTo(hx.w + 4, -9);
  ctx.stroke();
  ctx.save();
  ctx.translate(X, Y);
  ctx.rotate(ang);
  ctx.scale(SC, SC * 1.2);
  // The cast shadow under the housing.
  ctx.fillStyle = 'rgba(0,0,0,0.28)';
  rr(ctx, -13, -8, 46, 17, 5);
  ctx.fill();
  // The body: brushed metal, a hard specular strip on the upper edge.
  const body = ctx.createLinearGradient(0, -9, 0, 9);
  body.addColorStop(0, '#0d0f13');
  body.addColorStop(0.3, '#2c323c');
  body.addColorStop(0.55, '#6c7788');
  body.addColorStop(0.68, '#d3dbe8');
  body.addColorStop(0.76, '#7f8a9c');
  body.addColorStop(1, '#20252d');
  ctx.fillStyle = body;
  rr(ctx, -14, -8, 44, 16, 4.5);
  ctx.fill();
  // Seams and cooling ribs.
  ctx.strokeStyle = 'rgba(8,10,13,0.7)';
  ctx.lineWidth = 1;
  for (const x of [-9, -5.5, -2, 14, 22]) {
    ctx.beginPath();
    ctx.moveTo(x, -7.6);
    ctx.lineTo(x, 7.6);
    ctx.stroke();
  }
  ctx.strokeStyle = 'rgba(255,255,255,0.16)';
  for (const x of [-8, -4.5, -1, 15, 23]) {
    ctx.beginPath();
    ctx.moveTo(x, -7);
    ctx.lineTo(x, 7);
    ctx.stroke();
  }
  // The front barrel.
  const barrel = ctx.createLinearGradient(0, -6, 0, 6);
  barrel.addColorStop(0, '#0b0c0f');
  barrel.addColorStop(0.5, '#3e4552');
  barrel.addColorStop(0.7, '#9aa5b6');
  barrel.addColorStop(1, '#171a20');
  ctx.fillStyle = barrel;
  rr(ctx, 29, -6, 9, 12, 2);
  ctx.fill();
  // The beam lights the metal around the lens.
  if (a > 0.02) {
    const lit = ctx.createLinearGradient(8, 0, 40, 0);
    lit.addColorStop(0, rgba(tone(0.3), 0));
    lit.addColorStop(1, rgba(tone(0.3), 0.55 * a));
    ctx.fillStyle = lit;
    rr(ctx, 8, -8, 31, 16, 4);
    ctx.fill();
  }
  // The lens ring and the glass.
  ctx.fillStyle = '#07080a';
  ctx.beginPath();
  ctx.ellipse(38, 0, 3.4, 6.2, 0, 0, 6.29);
  ctx.fill();
  const glass = ctx.createRadialGradient(38.4, 0, 0, 38, 0, 5);
  glass.addColorStop(0, a > 0.02 ? 'rgba(255,255,252,1)' : rgba(tone(0.2), 0.5));
  glass.addColorStop(0.5, rgba(tone(a > 0.02 ? 0.35 : 0), 0.35 + 0.65 * a));
  glass.addColorStop(1, rgba(tone(-0.6), 0.9));
  ctx.fillStyle = glass;
  ctx.beginPath();
  ctx.ellipse(38.2, 0, 2.4, 4.8, 0, 0, 6.29);
  ctx.fill();
  // The status light.
  ctx.fillStyle = rgba(tone(0.2), 0.35 + 0.65 * a);
  ctx.beginPath();
  ctx.arc(6, 4.6, 1.3, 0, 6.29);
  ctx.fill();
  ctx.restore();
  // The glow of the lens and of the status light, on top of the metal.
  if (spr) {
    ctx.globalCompositeOperation = light ? 'source-over' : 'lighter';
    const lx = X + Math.cos(ang) * LENS;
    const ly = Y + Math.sin(ang) * LENS;
    const r = 14 + 34 * a;
    ctx.globalAlpha = (0.25 + 0.75 * a) * (light ? 0.7 : 1);
    ctx.drawImage(spr.glow, lx - r, ly - r, r * 2, r * 2);
    ctx.globalAlpha = a * 0.9;
    ctx.drawImage(spr.hot, lx - 10, ly - 10, 20, 20);
    ctx.globalAlpha = 1;
    ctx.globalCompositeOperation = 'source-over';
  }
};

const noise = (t) => 0.94 + 0.035 * Math.sin(t * 53) + 0.025 * Math.sin(t * 31.7 + 1.3);

// The beam from the lens to a point: bloom, glow, a travelling shimmer, a white-hot core.
const beam = (ctx, t, ex, ey, x, y, k, light) => {
  const f = noise(t);
  const w = 1 + 0.08 * Math.sin(t * 61);
  ctx.lineCap = 'round';
  const layers = light
    ? [
        [26, 0.07, tone(0)],
        [10, 0.18, tone(0)],
        [4.2, 0.7, tone(-0.15)],
      ]
    : [
        [64, 0.035, tone(0)],
        [26, 0.07, tone(0.05)],
        [10, 0.26, tone(0.25)],
        [4.4, 0.55, tone(0.65)],
      ];
  for (const [lw, al, c] of layers) {
    ctx.strokeStyle = rgba(c, al * k * f);
    ctx.lineWidth = lw * w;
    ctx.beginPath();
    ctx.moveTo(ex, ey);
    ctx.lineTo(x, y);
    ctx.stroke();
  }
  // Shimmer: soft blobs of light running down the beam.
  ctx.setLineDash([26, 38, 9, 22]);
  ctx.lineDashOffset = -t * 160;
  ctx.strokeStyle = rgba(tone(light ? 0 : 0.5), (light ? 0.3 : 0.16) * k * f);
  ctx.lineWidth = 5 * w;
  ctx.beginPath();
  ctx.moveTo(ex, ey);
  ctx.lineTo(x, y);
  ctx.stroke();
  ctx.setLineDash([]);
  // The white-hot core.
  ctx.strokeStyle = light ? `rgba(255,246,248,${0.95 * k})` : `rgba(255,250,250,${(0.9 + 0.1 * f) * k})`;
  ctx.lineWidth = 1.9 * w;
  ctx.beginPath();
  ctx.moveTo(ex, ey);
  ctx.lineTo(x, y);
  ctx.stroke();
};

const distSeg = (x, y, ax, ay, bx, by) => {
  const dx = bx - ax;
  const dy = by - ay;
  const l2 = dx * dx + dy * dy || 1;
  const k = clamp(((x - ax) * dx + (y - ay) * dy) / l2);
  return Math.hypot(x - (ax + dx * k), y - (ay + dy * k));
};

hx.on('frame', (t, dt) => {
  const ctx = hx.ctx;
  ctx.clearRect(0, 0, hx.w, hx.h);
  bakeSprites();
  const light = hx.mode === 'light';
  const pw = 0.55 + 0.45 * hx.power;
  const OP = light ? 'source-over' : 'lighter';
  const X = px();
  const Y = py();

  // ---- The heads: where the beam is, and the burn it leaves.
  // The printer: how far the beam has got through the answer, and what is still covered.
  mask = null;
  const pb = print && byId(print.id);
  let pr = null; // {row, x, y} of the head
  if (print && !pb) {
    printed.add(print.id);
    print = null;
  }
  if (print) {
    const W = Math.max(40, pb.w);
    const total = Math.max(1, Math.ceil(pb.h / LH));
    // While text streams in, its newest line stays covered: the beam cannot print what is not written yet.
    const allowed = print.fin ? total : Math.max(0, total - 1);
    const maxP = allowed * W;
    const lag = (total * W - print.p) / W;
    let speed = 700 * (1 + 1.4 * Math.max(0, lag - 1));
    if (print.fin) speed = Math.max(speed, (total * W - print.p) / 0.6);
    const before = print.p;
    print.p = Math.min(maxP, print.p + speed * dt);
    print.moved = print.p > before;
    let row;
    let x;
    if (print.p >= maxP) {
      row = Math.max(0, allowed - 1);
      x = allowed > 0 ? W : 0;
      mask = [[pb.x, pb.y + allowed * LH, W, pb.h - allowed * LH + 3]];
    } else {
      row = Math.floor(print.p / W);
      x = print.p - row * W;
      mask = [
        [pb.x + x, pb.y + row * LH, W - x, LH],
        [pb.x, pb.y + (row + 1) * LH, W, pb.h - (row + 1) * LH + 3],
      ];
    }
    pr = { row, x, y: pb.y + row * LH, W };
    if (print.fin && print.p >= total * W - 0.5) {
      printed.add(print.id);
      if (print.scanAfter) scan = { id: print.id, t: 0 };
      print = null;
      mask = null;
      pr = null;
    }
  }
  if (print && !heads.some((h) => h.k === 'w' && !h.end && h.id === print.id)) {
    const tr = { pts: [], live: true, cool: 1.1 };
    trails.push(tr);
    heads.push({
      k: 'w',
      id: print.id,
      a: 0,
      x: pb.x,
      y: pb.y,
      tr,
      end: false,
      sacc: 0,
      macc: 0,
      hot: 1,
      brk: true,
      row: -1,
      wrapT: 0,
    });
  }
  for (let i = heads.length - 1; i >= 0; i--) {
    const h = heads[i];
    const b = byId(h.id);
    if (h.k === 'w') {
      if (!(print && print.id === h.id && pr)) h.end = true;
      else {
        // The head rasters along the line, then drops to the next one.
        const ty = clamp(pr.y + LH * 0.55, 8, hx.h - 8);
        h.x = pb.x + pr.x + (pr.x > 0 && pr.x < pr.W ? Math.sin(t * 17) * 0.8 : 0);
        h.y = ty;
        if (h.row !== pr.row) {
          if (h.row >= 0) h.wrapT = 0.05;
          h.brk = true;
          h.row = pr.row;
        }
        h.hot = h.wrapT > 0 ? 0.3 : 1;
        h.wrapT -= dt;
        h.rowY = pr.y;
        h.rowX = pb.x + pr.x;
        h.rowX0 = pb.x;
        if (print.moved && h.wrapT <= 0) {
          h.tr.pts.push({ x: h.x, y: h.y, t, brk: h.brk });
          h.brk = false;
        }
      }
    } else if (!h.end) {
      if (!b) h.end = true;
      else {
        // The outline is drawn in about 1.25 s, in steps short enough to keep the corners round.
        const u0 = h.u;
        h.u = Math.min(1, h.u + dt / 1.25);
        const n = Math.max(1, Math.ceil(((h.u - u0) * 2 * (b.w + b.h)) / 7));
        for (let s = 1; s <= n; s++) {
          const [x, y] = around(b, u0 + ((h.u - u0) * s) / n);
          h.tr.pts.push({ x, y, t: t - dt * (1 - s / n), brk: h.u === u0 && s === 1 });
          h.x = x;
          h.y = y;
        }
        if (h.u >= 1) h.end = true;
        h.hot = 1;
      }
    }
    h.a += ((h.end ? 0 : 1) - h.a) * Math.min(1, dt * (h.end ? 10 : 16));
    if (h.end && h.a < 0.02) {
      h.tr.live = false;
      heads.splice(i, 1);
      continue;
    }
    // Sparks and smoke, where it burns.
    if (!h.end && h.hot > 0.5 && h.a > 0.3) {
      h.sacc += (h.k === 't' ? 90 : 70) * pw * dt;
      while (h.sacc >= 1 && sparks.length < 150) {
        h.sacc -= 1;
        const a = -Math.PI / 2 + (Math.random() - 0.5) * 2.5;
        const sp = 80 + Math.random() * 330;
        const life = 0.28 + Math.random() * 0.55;
        sparks.push({
          x: h.x,
          y: h.y,
          vx: Math.cos(a) * sp - 40 - Math.random() * 40,
          vy: Math.sin(a) * sp,
          life,
          max: life,
          floor: h.k === 'w' ? h.y + 1 + Math.random() * 4 : 0,
          bounces: 0,
        });
      }
      h.sacc = Math.min(h.sacc, 2);
      h.macc += 9 * dt;
      while (h.macc >= 1 && smoke.length < 40) {
        h.macc -= 1;
        smoke.push({
          x: h.x + (Math.random() - 0.5) * 8,
          y: h.y - 2,
          age: 0,
          max: 1.8 + Math.random() * 1.6,
          ph: Math.random() * 6.3,
          vy: 16 + Math.random() * 20,
          sx: 6 + Math.random() * 10,
        });
      }
      h.macc = Math.min(h.macc, 2);
    }
  }
  for (let i = trails.length - 1; i >= 0; i--) {
    const tr = trails[i];
    while (tr.pts.length && t - tr.pts[0].t > tr.cool) tr.pts.shift();
    if (!tr.live && !tr.pts.length) trails.splice(i, 1);
  }

  // ---- Where the emitter looks.
  let target = null;
  for (const h of heads) if (h.a > 0.2 && (!target || h.a >= target.a)) target = h;
  let tx = hx.w * 0.45;
  let ty = hx.h * 0.55;
  let beamK = 0;
  if (target) {
    tx = target.x;
    ty = target.y;
    beamK = target.a * (0.35 + 0.65 * target.hot);
  }
  const sb = scan && byId(scan.id);
  let scanY = 0;
  let scanK = 0;
  if (scan) {
    scan.t += dt / 1.15;
    if (!sb || scan.t >= 1.2) scan = null;
    else {
      const p = clamp(scan.t / 1.05);
      scanY = sb.y + sb.h * (0.5 - 0.5 * Math.cos(Math.PI * p));
      scanK = scan.t > 1.05 ? 1 - (scan.t - 1.05) / 0.15 : Math.min(1, scan.t * 10);
      if (!target) {
        tx = sb.x + sb.w * 0.5;
        ty = scanY;
        beamK = scanK;
      }
    }
  }
  const want = Math.atan2(ty - Y, tx - X);
  let da = want - aim;
  da = Math.atan2(Math.sin(da), Math.cos(da));
  aim += da * Math.min(1, dt * 16);
  const ex = X + Math.cos(aim) * LENS;
  const ey = Y + Math.sin(aim) * LENS;
  const active = beamK * pw;

  // ---- The mask first: the part of the answer the beam has not printed yet is covered.
  ctx.globalCompositeOperation = 'source-over';
  if (mask) {
    ctx.fillStyle = hx.colors.stage || hx.colors.bg || '#111';
    for (const [x, y, w, h] of mask) if (h > 0 && w > 0) ctx.fillRect(x, y, w, h);
  }

  // ---- Smoke: it is behind the light.
  for (let i = smoke.length - 1; i >= 0; i--) {
    const s = smoke[i];
    s.age += dt;
    const k = s.age / s.max;
    if (k >= 1) {
      smoke.splice(i, 1);
      continue;
    }
    s.y -= s.vy * dt * (1 - k * 0.5);
    s.x += Math.sin(t * 1.3 + s.ph) * s.sx * dt;
    const r = 5 + k * 20;
    const env = k < 0.15 ? k / 0.15 : (1 - k) / 0.85;
    ctx.globalAlpha = (light ? 0.2 : 0.14) * env * pw;
    ctx.drawImage(spr.smoke, s.x - r * 0.55, s.y - r * 1.4, r * 1.1, r * 2.8);
  }
  ctx.globalAlpha = 1;

  ctx.globalCompositeOperation = OP;

  // ---- The scan line after a finished answer.
  if (scan && sb) {
    const k = scanK * pw;
    const x0 = sb.x;
    const x1 = sb.x + sb.w;
    // The fan of light from the emitter to the line.
    ctx.fillStyle = rgba(tone(0.1), (light ? 0.05 : 0.035) * k);
    ctx.beginPath();
    ctx.moveTo(ex, ey);
    ctx.lineTo(x0, scanY);
    ctx.lineTo(x1, scanY);
    ctx.closePath();
    ctx.fill();
    // The afterglow it leaves above itself.
    const y0 = Math.max(sb.y, scanY - 54);
    if (scanY > y0) {
      const wake = ctx.createLinearGradient(0, scanY - 54, 0, scanY);
      wake.addColorStop(0, rgba(tone(0), 0));
      wake.addColorStop(1, rgba(tone(light ? 0 : 0.2), (light ? 0.2 : 0.16) * k));
      ctx.fillStyle = wake;
      ctx.fillRect(x0, y0, sb.w, scanY - y0);
    }
    const line = (lw, al, c) => {
      const g = ctx.createLinearGradient(x0, 0, x1, 0);
      g.addColorStop(0, rgba(c, 0));
      g.addColorStop(0.05, rgba(c, al * k));
      g.addColorStop(0.95, rgba(c, al * k));
      g.addColorStop(1, rgba(c, 0));
      ctx.strokeStyle = g;
      ctx.lineWidth = lw;
      ctx.beginPath();
      ctx.moveTo(x0, scanY);
      ctx.lineTo(x1, scanY);
      ctx.stroke();
    };
    line(30, light ? 0.1 : 0.06, tone(0));
    line(11, light ? 0.3 : 0.2, tone(0));
    line(4, light ? 0.55 : 0.42, tone(light ? -0.1 : 0.4));
    line(1.8, 0.98, light ? [255, 246, 248] : [255, 252, 252]);
    // The hot ends, and the beam's own spot on the right end.
    ctx.globalAlpha = 0.85 * k;
    const gr = 40 + 8 * noise(t * 3);
    ctx.drawImage(spr.glow, x1 - gr, scanY - gr, gr * 2, gr * 2);
    ctx.drawImage(spr.glow, x0 - gr, scanY - gr, gr * 2, gr * 2);
    ctx.globalAlpha = 1;
  }

  // ---- The burn trails: a soft glow of stamps under a thin hot line.
  for (const tr of trails) {
    const pts = tr.pts;
    // Stamps every few px along the line, so the glow is a smooth band and not a string of beads.
    for (let i = 0; i < pts.length; i++) {
      const p = pts[i];
      const q = i + 1 < pts.length && !pts[i + 1].brk ? pts[i + 1] : null;
      const n = q ? Math.max(1, Math.ceil(Math.hypot(q.x - p.x, q.y - p.y) / 5)) : 1;
      for (let k = 0; k < n; k++) {
        const f = k / n;
        const u = (t - lerp(p.t, q ? q.t : p.t, f)) / tr.cool;
        if (u >= 1) continue;
        const r = 9 + 10 * (1 - u);
        ctx.globalAlpha = (light ? 0.2 : 0.17) * Math.pow(1 - u, 1.3) * pw;
        ctx.drawImage(
          u < 0.5 ? spr.emberO : spr.emberR,
          lerp(p.x, q ? q.x : p.x, f) - r,
          lerp(p.y, q ? q.y : p.y, f) - r,
          r * 2,
          r * 2,
        );
      }
    }
    ctx.globalAlpha = 1;
    ctx.lineCap = 'butt';
    for (let i = 1; i < pts.length; i++) {
      const p1 = pts[i];
      if (p1.brk) continue;
      const p0 = pts[i - 1];
      const u0 = (t - p0.t) / tr.cool;
      const u1 = (t - p1.t) / tr.cool;
      if (u0 >= 1) continue;
      const c0 = heat(u0, light);
      const c1 = heat(u1, light);
      const um = (u0 + u1) / 2;
      const g2 = ctx.createLinearGradient(p0.x, p0.y, p1.x, p1.y);
      g2.addColorStop(0, rgba(c0, c0[3] * pw));
      g2.addColorStop(1, rgba(c1, c1[3] * pw));
      ctx.strokeStyle = g2;
      ctx.lineWidth = 1 + 2.4 * (1 - um);
      ctx.beginPath();
      ctx.moveTo(p0.x, p0.y);
      ctx.lineTo(p1.x, p1.y);
      ctx.stroke();
    }
  }

  // ---- The line just printed glows behind the head, white-yellow, then orange, then red.
  for (const h of heads) {
    if (h.k !== 'w' || h.end || h.a < 0.05 || h.rowX === undefined) continue;
    const len = Math.min(h.rowX - h.rowX0, 320);
    if (len < 4) continue;
    const k = h.a * pw;
    const y = h.rowY + LH * 0.5;
    ctx.globalAlpha = (light ? 0.45 : 0.38) * k;
    ctx.drawImage(spr.emberR, h.rowX - len, y - LH * 0.9, len * 0.9, LH * 1.8);
    ctx.globalAlpha = (light ? 0.5 : 0.42) * k;
    ctx.drawImage(spr.emberO, h.rowX - len * 0.55, y - LH * 0.85, len * 0.55, LH * 1.7);
    ctx.globalAlpha = (light ? 0.5 : 0.4) * k;
    ctx.drawImage(spr.hot, h.rowX - Math.min(len, 110), y - LH * 0.8, Math.min(len, 110) + 12, LH * 1.6);
  }
  ctx.globalAlpha = 1;

  // ---- Dust in the air, lit only where the beam is.
  if (active > 0.05 && dust.length < 46 * pw && Math.random() < dt * 30) {
    const s = 0.12 + Math.random() * 0.82;
    const nx = -(ty - ey);
    const ny = tx - ex;
    const nl = Math.hypot(nx, ny) || 1;
    const off = (Math.random() - 0.5) * 36;
    dust.push({
      x: lerp(ex, tx, s) + (nx / nl) * off,
      y: lerp(ey, ty, s) + (ny / nl) * off,
      vx: (Math.random() - 0.5) * 8,
      vy: -2 - Math.random() * 7,
      r: 0.5 + Math.random() * 0.9,
      age: 0,
      max: 2.5 + Math.random() * 3,
      ph: Math.random() * 6.3,
    });
  }
  for (let i = dust.length - 1; i >= 0; i--) {
    const d = dust[i];
    d.age += dt;
    if (d.age >= d.max) {
      dust.splice(i, 1);
      continue;
    }
    d.x += (d.vx + Math.sin(t * 0.9 + d.ph) * 5) * dt;
    d.y += d.vy * dt;
    if (active < 0.02) continue;
    const near = distSeg(d.x, d.y, ex, ey, tx, ty);
    const lit = Math.exp(-(near * near) / 110);
    if (lit < 0.03) continue;
    const k = d.age / d.max;
    const env = Math.sin(Math.PI * k);
    const tw = 0.55 + 0.45 * Math.sin(t * 7 + d.ph * 3);
    ctx.globalAlpha = Math.min(1, lit * env * tw * active * 1.4);
    ctx.fillStyle = light ? rgba(tone(-0.2), 1) : rgba(tone(0.8), 1);
    ctx.beginPath();
    ctx.arc(d.x, d.y, d.r * (1 + lit * 0.5), 0, 6.29);
    ctx.fill();
  }
  ctx.globalAlpha = 1;

  // ---- The beam, then the hot spots it makes.
  if (beamK > 0.01) {
    if (target) {
      for (const h of heads) {
        if (h.a < 0.02) continue;
        beam(ctx, t, ex, ey, h.x, h.y, h.a * (0.35 + 0.65 * h.hot) * pw, light);
      }
    } else beam(ctx, t, ex, ey, tx, ty, active, light);
  }
  for (const h of heads) {
    const k = h.a * h.hot * pw;
    if (k < 0.02) continue;
    const fl = noise(t * 2 + h.x * 0.01);
    // Heat haze: thin ripples rising from the point.
    ctx.lineWidth = 1.4;
    for (let n = 0; n < 3; n++) {
      const ph = (t * 1.15 + n / 3) % 1;
      const hw = 15 * (1 - ph * 0.35);
      const y = h.y - 5 - ph * 30;
      ctx.strokeStyle = light
        ? `rgba(120,60,40,${0.12 * Math.sin(Math.PI * ph) * k})`
        : `rgba(255,236,214,${0.1 * Math.sin(Math.PI * ph) * k})`;
      ctx.beginPath();
      ctx.moveTo(h.x - hw, y);
      ctx.quadraticCurveTo(h.x + Math.sin(t * 8 + n * 2) * 4, y - 4 * Math.sin(t * 6 + n), h.x + hw, y);
      ctx.stroke();
    }
    // The coloured bloom, an anamorphic streak, the white-hot spot.
    const R = (46 + 10 * fl) * (0.8 + 0.2 * k);
    ctx.globalAlpha = Math.min(1, 0.85 * k);
    ctx.drawImage(spr.glow, h.x - R, h.y - R, R * 2, R * 2);
    ctx.globalAlpha = Math.min(1, 0.55 * k);
    ctx.drawImage(spr.glow, h.x - 110, h.y - 3, 220, 6);
    ctx.globalAlpha = Math.min(1, k * fl);
    const r2 = 14 + 4 * fl;
    ctx.drawImage(spr.hot, h.x - r2, h.y - r2, r2 * 2, r2 * 2);
    ctx.fillStyle = light ? '#fff8f2' : '#ffffff';
    ctx.beginPath();
    ctx.arc(h.x, h.y, 2.2 + 0.5 * fl, 0, 6.29);
    ctx.fill();
    ctx.globalAlpha = 1;
  }

  // ---- Sparks: glowing streaks that fall, bounce off the burnt line and cool.
  ctx.lineCap = 'round';
  for (let i = sparks.length - 1; i >= 0; i--) {
    const s = sparks[i];
    s.life -= dt;
    if (s.life <= 0) {
      sparks.splice(i, 1);
      continue;
    }
    s.vy += 640 * dt;
    s.vx *= 1 - 0.8 * dt;
    s.x += s.vx * dt;
    s.y += s.vy * dt;
    if (s.floor && s.vy > 0 && s.y > s.floor && s.bounces < 2) {
      s.y = s.floor;
      s.vy *= -0.38;
      s.vx *= 0.75;
      s.bounces++;
    }
    const u = 1 - s.life / s.max;
    const c = heat(u * 0.92, light);
    const al = Math.min(1, s.life * 4) * c[3];
    const len = 0.032;
    ctx.strokeStyle = rgba(c, al * 0.28 * pw);
    ctx.lineWidth = 3.4;
    ctx.beginPath();
    ctx.moveTo(s.x - s.vx * len, s.y - s.vy * len);
    ctx.lineTo(s.x, s.y);
    ctx.stroke();
    ctx.strokeStyle = rgba(c, al * pw);
    ctx.lineWidth = 1.5 * (1 - u * 0.5);
    ctx.stroke();
    if (u < 0.35) {
      ctx.globalAlpha = (1 - u / 0.35) * 0.9 * pw;
      ctx.drawImage(spr.hot, s.x - 4, s.y - 4, 8, 8);
      ctx.globalAlpha = 1;
    }
  }

  ctx.globalCompositeOperation = 'source-over';
  ctx.globalAlpha = 1;
  device(ctx, aim, clamp(active * 1.1), light);
});
