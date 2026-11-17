// «Ночник»: a metal-shaded lamp hangs on a cord in the top-right corner and shines a soft volumetric cone onto the
// chat, with a warm pool of light where it lands. The cone, the pool and the halo are baked once into offscreen
// canvases (blurred there, never per frame); a frame only draws them with an alpha. While the agent works the lamp
// comes up slowly and the filament breathes, with dust drifting in the beam (lit only inside the cone, with depth);
// when the turn ends it dims to an ember; when the agent waits for the person it pulses in the accent colour.
// A built-in effect, and an example of the hx SDK (runtime.js).
const S = 0.5; // the baked light layers are half the stage's size: they are blurred anyway
const APEX = 39; // from the shade's opening to the end of its cap, where the cord is
const R = 24; // half the width of the shade's opening
const WARM = [255, 184, 104];
const EMBER = [176, 52, 20];
const HOT = [255, 240, 212];
let level = 0.15; // how bright the lamp is now, 0–1
let pulse = 0;
let mix = 0; // 0 warm … 1 the accent colour
let flick = 0;
let flickTarget = 0;
let flickT = 0;
let dip = 0;
const motes = [];
let geo = null;
let sets = {}; // baked light, by colour: {cone, pool, halo}
let fix = null; // the baked shade
let spr = null; // the baked dust sprites
let accentKey = '';
let bakedFor = '';
let timer = 0;

const clamp = (v, a = 0, b = 1) => (v < a ? a : v > b ? b : v);
const lerp = (a, b, k) => a + (b - a) * k;
const rgba = (c, a) => `rgba(${c[0] | 0},${c[1] | 0},${c[2] | 0},${a})`;
const toward = (c, to, k) => [lerp(c[0], to[0], k), lerp(c[1], to[1], k), lerp(c[2], to[2], k)];
const parse = (css) => {
  const m = /rgba?\((\d+)[ ,]+(\d+)[ ,]+(\d+)/.exec(css || '');
  if (m) return [+m[1], +m[2], +m[3]];
  const h = /^#([0-9a-f]{6})$/i.exec(css || '');
  if (h) return [parseInt(h[1].slice(0, 2), 16), parseInt(h[1].slice(2, 4), 16), parseInt(h[1].slice(4), 16)];
  return [160, 140, 255];
};
const smooth = (a, b, v) => {
  const k = clamp((v - a) / (b - a));
  return k * k * (3 - 2 * k);
};
const layer = (w, h) => {
  const c = document.createElement('canvas');
  c.width = Math.max(1, Math.ceil(w));
  c.height = Math.max(1, Math.ceil(h));
  return [c, c.getContext('2d')];
};

const want = () => {
  const s = hx.agent.state;
  if (s === 'working') return 1;
  if (s === 'waiting') return 0.7;
  return 0.12;
};

hx.on('prompt', () => {
  pulse = 1;
});

const layout = () => {
  const { w, h } = hx;
  const Ox = w - 76;
  const Oy = 72;
  const tx = w * 0.55;
  const ty = h * 0.85;
  const phi = clamp(Math.atan2(ty - Oy, tx - Ox), 1.85, 2.62);
  const d = [Math.cos(phi), Math.sin(phi)];
  const Cx = Ox - d[0] * APEX;
  const Cy = Oy - d[1] * APEX;
  geo = {
    Ox,
    Oy,
    phi,
    d,
    len: clamp(Math.hypot(tx - Ox, ty - Oy) * 1.15, 280, Math.hypot(w, h)),
    half: 0.43,
    Ax: Cx,
    Ay: -8,
    cord: Cy + 8,
  };
};

// The light of one colour: a cone with soft edges, the pool where it lands, the halo round the bulb.
const bakeSet = (rgb) => {
  const g = geo;
  const [cone, cx] = layer(hx.w * S, hx.h * S);
  cx.scale(S, S);
  cx.translate(g.Ox, g.Oy);
  cx.rotate(g.phi);
  const wedge = (half, a0, blur) => {
    const e = Math.tan(half) * g.len;
    const gr = cx.createLinearGradient(0, 0, g.len, 0);
    gr.addColorStop(0, rgba(toward(rgb, [255, 250, 240], 0.35), a0));
    gr.addColorStop(0.5, rgba(rgb, a0 * 0.42));
    gr.addColorStop(1, rgba(rgb, 0));
    cx.filter = `blur(${Math.max(0.5, blur * S)}px)`;
    cx.fillStyle = gr;
    cx.beginPath();
    cx.moveTo(0, -R * 0.7);
    cx.lineTo(g.len, -e);
    cx.lineTo(g.len, e);
    cx.lineTo(0, R * 0.7);
    cx.closePath();
    cx.fill();
  };
  wedge(g.half, 0.34, 18);
  wedge(g.half * 0.58, 0.3, 26);
  wedge(g.half * 0.24, 0.2, 22);
  // Faint rays through the haze.
  for (let i = 0; i < 7; i++) {
    const a = (((i * 0.618 + 0.13) % 1) - 0.5) * 2 * g.half * 0.85;
    const da = 0.012 + ((i * 0.37) % 1) * 0.02;
    const gr = cx.createLinearGradient(0, 0, g.len * 0.9, 0);
    gr.addColorStop(0, rgba(rgb, 0.1));
    gr.addColorStop(1, rgba(rgb, 0));
    cx.filter = `blur(${Math.max(0.5, 5 * S)}px)`;
    cx.fillStyle = gr;
    cx.beginPath();
    cx.moveTo(0, 0);
    cx.lineTo(Math.cos(a - da) * g.len * 0.9, Math.sin(a - da) * g.len * 0.9);
    cx.lineTo(Math.cos(a + da) * g.len * 0.9, Math.sin(a + da) * g.len * 0.9);
    cx.closePath();
    cx.fill();
  }
  // The pool of light where the cone lands.
  const [pool, px] = layer(hx.w * S, hx.h * S);
  px.scale(S, S);
  px.translate(g.Ox + g.d[0] * g.len * 0.8, g.Oy + g.d[1] * g.len * 0.8);
  px.rotate(g.phi - Math.PI / 2);
  px.scale(1, 0.42);
  const pr = g.len * 0.34;
  const pg = px.createRadialGradient(0, 0, 0, 0, 0, pr);
  pg.addColorStop(0, rgba(rgb, 0.4));
  pg.addColorStop(0.5, rgba(rgb, 0.16));
  pg.addColorStop(1, rgba(rgb, 0));
  px.filter = `blur(${Math.max(0.5, 10 * S)}px)`;
  px.fillStyle = pg;
  px.beginPath();
  px.arc(0, 0, pr, 0, 6.29);
  px.fill();
  // The halo round the bulb: one small sprite, scaled when drawn.
  const [halo, hxc] = layer(128, 128);
  const hg = hxc.createRadialGradient(64, 64, 0, 64, 64, 64);
  hg.addColorStop(0, rgba(toward(rgb, [255, 250, 240], 0.5), 1));
  hg.addColorStop(0.1, rgba(toward(rgb, [255, 250, 240], 0.25), 0.62));
  hg.addColorStop(0.32, rgba(rgb, 0.2));
  hg.addColorStop(0.65, rgba(rgb, 0.055));
  hg.addColorStop(1, rgba(rgb, 0));
  hxc.fillStyle = hg;
  hxc.fillRect(0, 0, 128, 128);
  return { cone, pool, halo };
};

// The shade, as metal: a dome with a specular strip, a dark cavity, a rolled rim, a cap for the cord.
const bakeFixture = () => {
  const k = Math.max(2, hx.dpr);
  const [c, x] = layer(76 * k, 70 * k);
  x.scale(k, k);
  x.translate(48, 35);
  const dome = () => {
    x.beginPath();
    x.moveTo(-34, -5);
    x.bezierCurveTo(-30, -13, -10, -R - 1.5, 0, -R);
    x.lineTo(0, R);
    x.bezierCurveTo(-10, R + 1.5, -30, 13, -34, 5);
    x.closePath();
  };
  const body = x.createLinearGradient(0, -R, 0, R);
  body.addColorStop(0, '#0c0d10');
  body.addColorStop(0.22, '#232830');
  body.addColorStop(0.48, '#4a525e');
  body.addColorStop(0.64, '#aab4c4');
  body.addColorStop(0.7, '#f4f7fc');
  body.addColorStop(0.77, '#7b8696');
  body.addColorStop(0.9, '#2d333c');
  body.addColorStop(1, '#14171c');
  x.fillStyle = body;
  dome();
  x.fill();
  // Darker toward the apex, lighter at the rim.
  const ax = x.createLinearGradient(-34, 0, 0, 0);
  ax.addColorStop(0, 'rgba(0,0,0,0.5)');
  ax.addColorStop(0.55, 'rgba(0,0,0,0.08)');
  ax.addColorStop(1, 'rgba(255,255,255,0.05)');
  x.fillStyle = ax;
  dome();
  x.fill();
  // A soft reflection of the room along the dome.
  x.strokeStyle = 'rgba(210,222,240,0.32)';
  x.lineWidth = 1.1;
  x.beginPath();
  x.moveTo(-30, 8.4);
  x.bezierCurveTo(-24, 11, -12, R * 0.72, -4, R * 0.74);
  x.stroke();
  // The cap and the cord's ring.
  const cap = x.createLinearGradient(0, -6, 0, 6);
  cap.addColorStop(0, '#15181d');
  cap.addColorStop(0.65, '#8c97a8');
  cap.addColorStop(1, '#1b1f25');
  x.fillStyle = cap;
  x.beginPath();
  x.moveTo(-34, -5);
  x.lineTo(-38, -3.4);
  x.lineTo(-38, 3.4);
  x.lineTo(-34, 5);
  x.closePath();
  x.fill();
  x.fillStyle = '#9aa4b4';
  x.beginPath();
  x.arc(-39, 0, 1.6, 0, 6.29);
  x.fill();
  // The cavity of the opening, seen at a slant.
  x.fillStyle = '#07080a';
  x.beginPath();
  x.ellipse(0, 0, R * 0.25, R, 0, 0, 6.29);
  x.fill();
  const rim = x.createLinearGradient(0, -R, 0, R);
  rim.addColorStop(0, '#2a2f37');
  rim.addColorStop(0.55, '#9aa5b6');
  rim.addColorStop(0.72, '#f0f4fa');
  rim.addColorStop(1, '#3a414b');
  x.strokeStyle = rim;
  x.lineWidth = 2;
  x.beginPath();
  x.ellipse(0, 0, R * 0.25, R, 0, 0, 6.29);
  x.stroke();
  return c;
};

// Dust: a soft out-of-focus disc, a sharp speck, a dot.
const bakeDust = () => {
  const mk = (n, stops) => {
    const [c, x] = layer(n, n);
    const g = x.createRadialGradient(n / 2, n / 2, 0, n / 2, n / 2, n / 2);
    for (const [o, a] of stops) g.addColorStop(o, `rgba(255,228,184,${a})`);
    x.fillStyle = g;
    x.fillRect(0, 0, n, n);
    return c;
  };
  return {
    bokeh: mk(48, [
      [0, 0.5],
      [0.7, 0.62],
      [0.9, 0.95],
      [1, 0],
    ]),
    soft: mk(32, [
      [0, 1],
      [0.35, 0.45],
      [1, 0],
    ]),
    sharp: mk(16, [
      [0, 1],
      [0.5, 0.85],
      [1, 0],
    ]),
  };
};

const bakeAccent = () => {
  timer = 0;
  if (!geo) return;
  accentKey = hx.colors.accent;
  sets.accent = bakeSet(toward(parse(hx.colors.accent), [255, 255, 255], 0.12));
};
const bake = () => {
  if (!hx.w || !hx.h) return;
  layout();
  sets = { warm: bakeSet(WARM) };
  fix = bakeFixture();
  spr = bakeDust();
  bakedFor = hx.w + 'x' + hx.h;
  // The accent light is only for the waiting state: bake it a moment later, not in the same frame.
  clearTimeout(timer);
  timer = setTimeout(bakeAccent, 400);
};
hx.on('start', bake);
hx.on('resize', bake);
hx.on('theme', () => {
  clearTimeout(timer);
  timer = setTimeout(bakeAccent, 200);
});

const spawn = (m, initial) => {
  const g = geo;
  const s = g.len * (0.1 + 0.85 * Math.pow(Math.random(), 1.2));
  const a = (Math.random() * 2 - 1) * g.half * 0.95;
  const l = Math.tan(a) * s;
  m.x = g.Ox + g.d[0] * s - g.d[1] * l;
  m.y = g.Oy + g.d[1] * s + g.d[0] * l;
  m.z = Math.random(); // 0 close to the viewer, 1 far
  m.ph = Math.random() * 6.3;
  m.max = 5 + Math.random() * 8;
  m.age = initial ? Math.random() * m.max : 0;
  m.r =
    m.z < 0.16 ? 5 + Math.random() * 6 : m.z < 0.7 ? 1.2 + Math.random() * 1.1 : 0.7 + Math.random() * 0.5;
  m.vx = (Math.random() - 0.5) * 7;
  m.vy = (Math.random() - 0.5) * 5 - 2.5;
  return m;
};

hx.on('frame', (t, dt) => {
  const ctx = hx.ctx;
  const { w, h } = hx;
  ctx.clearRect(0, 0, w, h);
  if (!geo || bakedFor !== w + 'x' + h) bake();
  if (!geo) return;
  const g = geo;
  const light = hx.mode === 'light';
  const st = hx.agent.state;
  const working = st === 'working';
  const waiting = st === 'waiting';
  const pw = 0.5 + 0.5 * hx.power;

  // Up slowly, down slower: a lamp, not a switch.
  const goal = want() * (0.45 + 0.55 * hx.power);
  level += (goal - level) * Math.min(1, dt * (goal > level ? 0.9 : 0.45));
  pulse = Math.max(0, pulse - dt * 0.8);
  mix += ((waiting ? 1 : 0) - mix) * Math.min(1, dt * 1.6);

  // A real filament: a slow breath, a drifting flutter, now and then a faint dip. Subtle.
  flickT -= dt;
  if (flickT <= 0) {
    flickT = 0.15 + Math.random() * 0.4;
    flickTarget = Math.random() * 2 - 1;
  }
  flick += (flickTarget - flick) * Math.min(1, dt * 5);
  if (working && Math.random() < dt * 0.3) dip = 1;
  dip = Math.max(0, dip - dt * 4.5);
  let breath = 1;
  if (working)
    breath = 1 + 0.04 * Math.sin(t * 1.1) + 0.018 * Math.sin(t * 2.9 + 1) + 0.022 * flick - 0.05 * dip * dip;
  else if (waiting) breath = 0.8 + 0.2 * Math.sin(t * 2.4);
  else breath = 1 + 0.03 * Math.sin(t * 0.7);
  const I = clamp(level * breath + pulse * 0.22);
  const warmK = 1 - mix;

  // ---- The light: halo, cone, pool.
  const OP = light ? 'source-over' : 'lighter';
  const kc = light ? 0.55 : 1;
  ctx.globalCompositeOperation = OP;
  ctx.save();
  // The whole lamp sways a hair about the point where the cord hangs.
  const sway = Math.sin(t * 0.45) * 0.006 + Math.sin(t * 0.17 + 1) * 0.004;
  ctx.translate(g.Ax, g.Ay);
  ctx.rotate(sway);
  ctx.translate(-g.Ax, -g.Ay);
  for (const [key, k] of [
    ['warm', warmK],
    ['accent', mix],
  ]) {
    const set = sets[key];
    if (!set || k < 0.01) continue;
    const a = I * k;
    ctx.globalAlpha = clamp(a * 0.95 * kc * (0.7 + 0.3 * pw));
    ctx.drawImage(set.cone, 0, 0, w, h);
    ctx.globalAlpha = clamp(a * 0.9 * kc * (0.9 + 0.1 * Math.sin(t * 0.8)));
    ctx.drawImage(set.pool, 0, 0, w, h);
    ctx.globalAlpha = clamp(a * 0.85 * kc);
    const hr = 120 + 90 * I;
    ctx.drawImage(set.halo, g.Ox - hr, g.Oy - hr, hr * 2, hr * 2);
  }
  ctx.restore();

  // ---- Dust: lit only inside the cone, with depth.
  const target = Math.round((14 + 40 * pw) * clamp(I * 1.4));
  while (motes.length < target) motes.push(spawn({}, true));
  ctx.globalCompositeOperation = OP;
  for (let i = motes.length - 1; i >= 0; i--) {
    const m = motes[i];
    m.age += dt;
    if (m.age >= m.max || (motes.length > target && m.age > m.max * 0.5)) {
      if (motes.length > target) {
        motes.splice(i, 1);
        continue;
      }
      spawn(m, false);
    }
    const sp = 1.5 - m.z; // the near ones drift faster
    m.x += (m.vx + Math.sin(t * 0.6 + m.ph) * 5 * sp) * dt * sp;
    m.y += (m.vy + Math.cos(t * 0.45 + m.ph * 1.7) * 4 * sp) * dt * sp;
    const dx = m.x - g.Ox;
    const dy = m.y - g.Oy;
    const s = dx * g.d[0] + dy * g.d[1];
    if (s < 12) continue;
    const l = -dx * g.d[1] + dy * g.d[0];
    const ang = Math.abs(Math.atan2(l, s));
    // Inside the cone only: bright on the axis, gone at the edge and far from the lamp.
    const f =
      (1 - smooth(g.half * 0.35, g.half * 1.02, ang)) *
      Math.pow(clamp(1 - s / g.len), 0.7) *
      smooth(12, 60, s);
    if (f < 0.01) continue;
    const env = Math.sin(Math.PI * clamp(m.age / m.max));
    const tw = 0.6 + 0.4 * Math.sin(t * (1.5 + m.z * 3) + m.ph * 5);
    let sprite;
    let al;
    if (m.z < 0.16) {
      sprite = spr.bokeh;
      al = 0.2;
    } else if (m.z < 0.7) {
      sprite = spr.soft;
      al = 0.95;
    } else {
      sprite = spr.sharp;
      al = 0.6;
    }
    const a = f * env * tw * I * al * (light ? 0.55 : 1);
    if (a < 0.01) continue;
    ctx.globalAlpha = clamp(a);
    const r = m.r * (sprite === spr.soft ? 2.2 : 1);
    ctx.drawImage(sprite, m.x - r, m.y - r, r * 2, r * 2);
  }
  ctx.globalAlpha = 1;

  // ---- The fixture.
  ctx.globalCompositeOperation = 'source-over';
  ctx.save();
  ctx.translate(g.Ax, g.Ay);
  ctx.rotate(sway);
  // The cord.
  ctx.lineCap = 'round';
  ctx.strokeStyle = light ? '#2a2d34' : '#07080a';
  ctx.lineWidth = 2.4;
  ctx.beginPath();
  ctx.moveTo(0, 0);
  ctx.lineTo(0, g.cord);
  ctx.stroke();
  ctx.strokeStyle = 'rgba(190,198,212,0.3)';
  ctx.lineWidth = 0.8;
  ctx.beginPath();
  ctx.moveTo(-0.7, 0);
  ctx.lineTo(-0.7, g.cord);
  ctx.stroke();
  ctx.translate(0, g.cord);
  ctx.rotate(g.phi);
  ctx.translate(APEX, 0);
  ctx.drawImage(fix, -48, -35, 76, 70);
  // The lamp's own colour right now: from an ember to a hot filament, or the accent while it waits.
  const heatK = Math.pow(clamp(I), 0.75);
  const bulbCol = toward(
    toward(EMBER, HOT, heatK),
    toward(parse(hx.colors.accent), [255, 255, 255], 0.5),
    mix * 0.8,
  );
  const glowCol = toward(WARM, parse(hx.colors.accent), mix);
  // The cavity of the shade, lit from inside.
  ctx.save();
  ctx.scale(0.25, 1);
  const cav = ctx.createRadialGradient(0, 0, 0, 0, 0, R);
  cav.addColorStop(0, rgba(toward(bulbCol, [255, 255, 255], 0.35), 0.35 + 0.65 * heatK));
  cav.addColorStop(0.55, rgba(glowCol, (0.15 + 0.7 * heatK) * 0.9));
  cav.addColorStop(1, rgba(toward(glowCol, [0, 0, 0], 0.6), 0.15 + 0.35 * heatK));
  ctx.fillStyle = cav;
  ctx.beginPath();
  ctx.arc(0, 0, R * 0.93, 0, 6.29);
  ctx.fill();
  ctx.restore();
  // The bulb, standing a little out of the opening.
  const bx = 4;
  const bulb = ctx.createRadialGradient(bx - 1, -1, 0, bx, 0, 10);
  bulb.addColorStop(0, rgba(toward(bulbCol, [255, 255, 255], 0.55 * heatK), 1));
  bulb.addColorStop(0.45, rgba(bulbCol, 0.95));
  bulb.addColorStop(1, rgba(toward(bulbCol, [0, 0, 0], 0.5), 0.9));
  ctx.fillStyle = bulb;
  ctx.beginPath();
  ctx.arc(bx, 0, 9.5, 0, 6.29);
  ctx.fill();
  // The filament.
  ctx.strokeStyle = rgba(toward(bulbCol, [255, 255, 255], 0.7), 0.35 + 0.65 * heatK);
  ctx.lineWidth = 1.1;
  ctx.beginPath();
  ctx.moveTo(bx - 3.5, 4.6);
  ctx.lineTo(bx - 3, -1);
  for (let i = 0; i < 4; i++) ctx.lineTo(bx - 2 + i * 1.8, i % 2 ? -4 : -1.4);
  ctx.lineTo(bx + 3.6, 4.6);
  ctx.stroke();
  // A glint of the bulb on the rolled rim of the shade, and the bloom of the bulb itself.
  ctx.globalCompositeOperation = OP;
  ctx.strokeStyle = rgba(glowCol, (light ? 0.4 : 0.6) * heatK);
  ctx.lineWidth = 1.6;
  ctx.beginPath();
  ctx.ellipse(0, 0, R * 0.25, R, 0, 0, 6.29);
  ctx.stroke();
  const set = mix > 0.5 && sets.accent ? sets.accent : sets.warm;
  const br = 26 + 64 * heatK;
  ctx.globalAlpha = clamp(0.2 + 0.8 * heatK) * (light ? 0.6 : 1);
  ctx.drawImage(set.halo, bx - br, -br, br * 2, br * 2);
  ctx.restore();
  // A soft anamorphic streak across the bulb, the way a lens sees a bare filament.
  if (!light && heatK > 0.25) {
    const bxw = g.Ox + g.d[0] * 4;
    const byw = g.Oy + g.d[1] * 4;
    ctx.globalAlpha = 0.28 * (heatK - 0.25) * 1.33;
    ctx.drawImage(set.halo, bxw - 130, byw - 3, 260, 6);
  }
  ctx.globalAlpha = 1;
  ctx.globalCompositeOperation = 'source-over';
});
