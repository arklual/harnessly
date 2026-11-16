// «Лазер»: while the agent's answer is written, a beam from the top corner burns it in — it runs along the answer's
// newest line, throwing sparks, and a bright edge follows the text as it grows; when the answer is done a flash runs
// along it. A new prompt is outlined by a light that runs once around it. A built-in effect, and an example of the hx
// SDK (runtime.js).
const sparks = [];
const rings = []; // lights that run around a block: {id, t}
let writing = null; // {id, since, x}
let flash = null; // {id, t}
let hum = 0;

const accentRgb = () => {
  const c = hx.colors.accent || '';
  const m = /rgba?\((\d+)[ ,]+(\d+)[ ,]+(\d+)/.exec(c);
  if (m) return [+m[1], +m[2], +m[3]];
  const h = /^#([0-9a-f]{6})$/i.exec(c);
  if (h) return [parseInt(h[1].slice(0, 2), 16), parseInt(h[1].slice(2, 4), 16), parseInt(h[1].slice(4), 16)];
  return [255, 60, 90];
};
const col = (a, boost = 0) => {
  const [r, g, b] = accentRgb();
  return `rgba(${Math.min(255, r + boost)},${Math.min(255, g + boost)},${Math.min(255, b + boost)},${a})`;
};
const byId = (id) => hx.blocks.find((b) => b.id === id) || null;

hx.on('prompt', (e) => {
  if (e.block) rings.push({ id: e.block.id, t: 0 });
});
hx.on('answer', (e) => {
  if (!e.block) return;
  if (!writing || writing.id !== e.block.id) writing = { id: e.block.id, since: 0, x: e.block.x };
});
hx.on('answer-end', (e) => {
  const id = (e.block && e.block.id) || (writing && writing.id);
  if (id) flash = { id, t: 0 };
  writing = null;
});
hx.on('state', (s) => {
  if (s.state !== 'working' && writing) {
    flash = { id: writing.id, t: 0 };
    writing = null;
  }
});

// A path around a rounded box, as a point at share u (0–1) of its perimeter.
const around = (b, u) => {
  const p = 2 * (b.w + b.h);
  let d = (u % 1) * p;
  if (d < b.w) return [b.x + d, b.y];
  d -= b.w;
  if (d < b.h) return [b.x + b.w, b.y + d];
  d -= b.h;
  if (d < b.w) return [b.x + b.w - d, b.y + b.h];
  d -= b.w;
  return [b.x, b.y + b.h - d];
};

hx.on('frame', (t, dt) => {
  const ctx = hx.ctx;
  ctx.clearRect(0, 0, hx.w, hx.h);
  const light = hx.mode === 'light';
  ctx.globalCompositeOperation = light ? 'source-over' : 'lighter';
  const power = 0.5 + hx.power * 0.5;

  // The prompt's ring of light.
  for (let i = rings.length - 1; i >= 0; i--) {
    const r = rings[i];
    r.t += dt / 1.1;
    const b = byId(r.id);
    if (!b || r.t >= 1.25) {
      rings.splice(i, 1);
      continue;
    }
    const head = Math.min(1, r.t);
    const fade = r.t > 1 ? 1 - (r.t - 1) * 4 : 1;
    ctx.lineCap = 'round';
    for (let k = 0; k < 24; k++) {
      const u0 = head - 0.22 + (k / 24) * 0.22;
      const u1 = u0 + 0.22 / 24;
      if (u1 < 0) continue;
      const [x0, y0] = around(b, Math.max(0, u0));
      const [x1, y1] = around(b, u1);
      ctx.strokeStyle = col((k / 24) * 0.9 * fade * power, 60);
      ctx.lineWidth = 1 + (k / 24) * 2.5;
      ctx.beginPath();
      ctx.moveTo(x0, y0);
      ctx.lineTo(x1, y1);
      ctx.stroke();
    }
    // The whole outline glows faintly after the light has gone round.
    if (r.t > 0.6) {
      ctx.strokeStyle = col(0.35 * fade * power);
      ctx.lineWidth = 1;
      ctx.strokeRect(b.x, b.y, b.w, b.h);
    }
  }

  // The beam burning the newest line of the answer.
  hum = Math.max(0, hum - dt * 2);
  if (writing) {
    const b = byId(writing.id);
    if (b && b.y + b.h > 0 && b.y < hx.h + 40) {
      writing.since += dt;
      hum = 1;
      // The hit point sweeps along the bottom line of the answer, like a printer's head.
      const span = Math.max(40, b.w - 24);
      const sweep = (Math.sin(writing.since * 2.6) * 0.5 + 0.5) * span;
      const hx0 = b.x + 12 + sweep;
      const hy0 = Math.min(hx.h - 6, b.y + b.h - 10);
      const ex = hx.w - 18;
      const ey = 14;
      // The emitter.
      ctx.fillStyle = col(0.9, 80);
      ctx.beginPath();
      ctx.arc(ex, ey, 4 + Math.sin(t * 30) * 0.8, 0, 6.29);
      ctx.fill();
      // The beam: a wide soft glow and a hot core.
      for (const [wdt, a, boost] of [
        [9, 0.12, 0],
        [4, 0.35, 40],
        [1.4, 0.95, 140],
      ]) {
        ctx.strokeStyle = col(a * power, boost);
        ctx.lineWidth = wdt;
        ctx.beginPath();
        ctx.moveTo(ex, ey);
        ctx.lineTo(hx0, hy0);
        ctx.stroke();
      }
      // The burnt edge: a bright line under the answer that fades from where the beam is.
      const edge = ctx.createLinearGradient(b.x, 0, b.x + b.w, 0);
      const at = Math.min(0.999, Math.max(0.001, (hx0 - b.x) / b.w));
      edge.addColorStop(0, col(0));
      edge.addColorStop(Math.max(0, at - 0.2), col(0.15 * power));
      edge.addColorStop(at, col(0.9 * power, 120));
      edge.addColorStop(Math.min(1, at + 0.2), col(0.15 * power));
      edge.addColorStop(1, col(0));
      ctx.fillStyle = edge;
      ctx.fillRect(b.x, hy0 + 6, b.w, 2);
      // The hot spot and its sparks.
      const spot = ctx.createRadialGradient(hx0, hy0, 0, hx0, hy0, 18);
      spot.addColorStop(0, col(0.9 * power, 160));
      spot.addColorStop(1, col(0));
      ctx.fillStyle = spot;
      ctx.beginPath();
      ctx.arc(hx0, hy0, 18, 0, 6.29);
      ctx.fill();
      for (let k = 0; k < 3 * power; k++)
        sparks.push({
          x: hx0,
          y: hy0,
          vx: (Math.random() - 0.5) * 220,
          vy: -Math.random() * 160,
          life: 0.4 + Math.random() * 0.5,
        });
    }
  }

  // The flash along a finished answer.
  if (flash) {
    flash.t += dt / 0.9;
    const b = byId(flash.id);
    if (!b || flash.t >= 1) flash = null;
    else {
      const x = b.x + b.w * flash.t;
      const g = ctx.createLinearGradient(x - 120, 0, x + 20, 0);
      g.addColorStop(0, col(0));
      g.addColorStop(0.85, col(0.18 * power, 80));
      g.addColorStop(1, col(0));
      ctx.fillStyle = g;
      ctx.fillRect(Math.max(b.x, x - 120), b.y, Math.min(140, x - b.x + 20), b.h);
      ctx.strokeStyle = col(0.6 * (1 - flash.t) * power, 100);
      ctx.lineWidth = 1.2;
      ctx.strokeRect(b.x - 2, b.y - 2, b.w + 4, b.h + 4);
    }
  }

  for (let i = sparks.length - 1; i >= 0; i--) {
    const s = sparks[i];
    s.life -= dt;
    if (s.life <= 0) {
      sparks.splice(i, 1);
      continue;
    }
    s.vy += 420 * dt;
    s.x += s.vx * dt;
    s.y += s.vy * dt;
    ctx.strokeStyle = col(Math.min(1, s.life * 2), 150);
    ctx.lineWidth = 1.3;
    ctx.beginPath();
    ctx.moveTo(s.x, s.y);
    ctx.lineTo(s.x - s.vx * 0.02, s.y - s.vy * 0.02);
    ctx.stroke();
  }
  ctx.globalCompositeOperation = 'source-over';
});
