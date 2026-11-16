// «Снежная игра»: big fluffy flakes that drift away from the pointer, settle on the tops of the chat's blocks and
// at the bottom of the screen; a quick swipe of the mouse sweeps a drift away, a double click on an empty place calls
// a snowplough along the bottom, a double click on a block shakes its snow off, and now and then Ded Moroz flies by.
// A built-in effect, and an example of the hx SDK (runtime.js).
const COL = 3; // px of one column of a drift
const BLOCK_CAP = 16; // the highest drift on a block, px
const GROUND_CAP = 34;
const light = () => hx.mode === 'light';

// Sprites: soft round flakes and six-armed crystals, drawn once.
const sprite = (r, crystal) => {
  const c = document.createElement('canvas');
  const s = Math.ceil(r * 2 + 4);
  c.width = c.height = s;
  const g = c.getContext('2d');
  g.translate(s / 2, s / 2);
  const tone = light() ? '214,228,246' : '255,255,255';
  if (crystal) {
    g.strokeStyle = `rgba(${tone},0.95)`;
    g.lineWidth = Math.max(1, r / 7);
    g.lineCap = 'round';
    for (let k = 0; k < 6; k++) {
      g.rotate(Math.PI / 3);
      g.beginPath();
      g.moveTo(0, 0);
      g.lineTo(0, r);
      g.moveTo(0, r * 0.5);
      g.lineTo(r * 0.28, r * 0.75);
      g.moveTo(0, r * 0.5);
      g.lineTo(-r * 0.28, r * 0.75);
      g.stroke();
    }
    if (light()) {
      g.globalCompositeOperation = 'destination-over';
      g.shadowColor = 'rgba(40,70,120,0.35)';
      g.shadowBlur = 2;
      g.stroke();
    }
  } else {
    const grad = g.createRadialGradient(0, 0, 0, 0, 0, r);
    grad.addColorStop(0, `rgba(${tone},1)`);
    grad.addColorStop(0.45, `rgba(${tone},0.75)`);
    grad.addColorStop(1, `rgba(${tone},0)`);
    g.fillStyle = grad;
    g.beginPath();
    g.arc(0, 0, r, 0, Math.PI * 2);
    g.fill();
  }
  return c;
};
let SPR = [];
const makeSprites = () => {
  SPR = [];
  for (const r of [3, 4.5, 6.5, 9, 12]) SPR.push({ r, img: sprite(r, false), crystal: false });
  for (const r of [11, 15, 20]) SPR.push({ r, img: sprite(r, true), crystal: true });
};

const flakes = [];
const puffs = [];
const drifts = new Map(); // block id -> {cols:Float32Array, x, y, w}
let ground = new Float32Array(0);
let plow = null;
let santa = null;
let nextSanta = 25;
let clock = 0;

const target = () => Math.round((40 + 160 * hx.power) * Math.min(1.6, (hx.w * hx.h) / (1400 * 900)));
const newFlake = (top) => {
  const big = Math.random();
  const spr = big > 0.88 ? SPR[5 + ((Math.random() * 3) | 0)] : SPR[(Math.random() * 5) | 0];
  const depth = spr.crystal ? 1 : 0.5 + spr.r / 16;
  return {
    x: Math.random() * hx.w,
    y: top ? -20 - Math.random() * 40 : Math.random() * hx.h,
    vx: 0,
    vy: (12 + spr.r * 2.4) * depth,
    spr,
    rot: Math.random() * 6.28,
    spin: (Math.random() - 0.5) * 0.8,
    sway: Math.random() * 6.28,
    swayAmp: 8 + Math.random() * 18,
    alpha: spr.crystal ? 0.9 : 0.55 + Math.random() * 0.4,
  };
};

const syncDrifts = () => {
  const seen = new Set();
  for (const b of hx.blocks) {
    if (b.kind === 'pane' || b.kind === 'sidebar') continue;
    seen.add(b.id);
    const n = Math.max(1, Math.round(b.w / COL));
    let d = drifts.get(b.id);
    if (!d) drifts.set(b.id, (d = { cols: new Float32Array(n), x: b.x, y: b.y, w: b.w }));
    else if (d.cols.length !== n) {
      const next = new Float32Array(n);
      next.set(d.cols.subarray(0, Math.min(n, d.cols.length)));
      d.cols = next;
    }
    d.x = b.x;
    d.y = b.y;
    d.w = b.w;
  }
  // A block that went away (scrolled out, removed) lets its snow fall.
  for (const [id, d] of drifts) {
    if (seen.has(id)) continue;
    if (d.y > -20 && d.y < hx.h + 20) shake(d, 0.4);
    drifts.delete(id);
  }
};

const shake = (d, power) => {
  for (let i = 0; i < d.cols.length; i += 2) {
    const h = d.cols[i];
    if (h < 0.5) continue;
    for (let k = 0; k < Math.min(3, h / 3); k++)
      puffs.push({
        x: d.x + i * COL,
        y: d.y - h * Math.random(),
        vx: (Math.random() - 0.5) * 60 * power,
        vy: -Math.random() * 90 * power,
        r: 1.2 + Math.random() * 2.2,
        life: 1.6,
        settle: true,
      });
    d.cols[i] = 0;
    if (i + 1 < d.cols.length) d.cols[i + 1] = 0;
  }
};

const addTo = (cols, i, amount, cap) => {
  for (let k = -2; k <= 2; k++) {
    const j = i + k;
    if (j < 0 || j >= cols.length) continue;
    cols[j] = Math.min(cap, cols[j] + amount * (1 - Math.abs(k) / 3));
  }
};

// Snow slides down a slope that is too steep, and the ends of a block lose it.
const relax = (cols, cap) => {
  for (let i = 1; i < cols.length; i++) {
    const diff = cols[i] - cols[i - 1];
    if (Math.abs(diff) > 1.6) {
      const m = diff * 0.25;
      cols[i] -= m;
      cols[i - 1] += m;
    }
  }
  if (cols.length > 4) {
    cols[0] = Math.min(cols[0], cap * 0.25);
    cols[cols.length - 1] = Math.min(cols[cols.length - 1], cap * 0.25);
  }
};

const settle = (x, y, amount) => {
  for (const d of drifts.values()) {
    if (x < d.x || x > d.x + d.w) continue;
    const i = Math.floor((x - d.x) / COL);
    const top = d.y - d.cols[i];
    if (y >= top && y <= d.y + 6) {
      addTo(d.cols, i, amount, BLOCK_CAP);
      return true;
    }
  }
  const gi = Math.floor(x / COL);
  if (gi >= 0 && gi < ground.length && y >= hx.h - ground[gi]) {
    addTo(ground, gi, amount, GROUND_CAP);
    return true;
  }
  return false;
};

// A swipe through a drift throws its snow along the swipe.
const sweep = () => {
  const p = hx.pointer;
  const speed = Math.hypot(p.vx, p.vy);
  if (!p.inside || speed < 250) return;
  const reach = 26;
  const dig = (cols, x0, base) => {
    const from = Math.max(0, Math.floor((p.x - reach - x0) / COL));
    const to = Math.min(cols.length - 1, Math.ceil((p.x + reach - x0) / COL));
    for (let i = from; i <= to; i++) {
      const h = cols[i];
      const top = base - h;
      if (h < 0.6 || p.y < top - reach || p.y > base + 10) continue;
      const take = h * Math.min(1, speed / 1400);
      cols[i] -= take;
      if (Math.random() < take / 2)
        puffs.push({
          x: x0 + i * COL,
          y: top,
          vx: p.vx * (0.15 + Math.random() * 0.2),
          vy: p.vy * 0.15 - 40 - Math.random() * 60,
          r: 1 + Math.random() * 2.5,
          life: 1.4,
          settle: true,
        });
    }
  };
  for (const d of drifts.values()) dig(d.cols, d.x, d.y);
  dig(ground, 0, hx.h);
};

const drawDrift = (ctx, cols, x0, base) => {
  let any = false;
  for (let i = 0; i < cols.length; i++) if (cols[i] > 0.4) any = true;
  if (!any) return;
  ctx.beginPath();
  ctx.moveTo(x0, base);
  for (let i = 0; i < cols.length; i++) ctx.lineTo(x0 + i * COL + COL / 2, base - cols[i]);
  ctx.lineTo(x0 + cols.length * COL, base);
  ctx.closePath();
  ctx.fill();
  ctx.stroke();
};

// The snowplough: a little orange machine that runs along the bottom and throws the ground's snow ahead.
const drawPlow = (ctx, p) => {
  const gi = Math.max(0, Math.min(ground.length - 1, Math.floor(p.x / COL)));
  const y = hx.h - Math.min(4, ground[gi] || 0);
  ctx.save();
  ctx.translate(p.x, y);
  ctx.scale(p.dir * 1.25, 1.25);
  ctx.fillStyle = '#f39a1e';
  ctx.beginPath();
  ctx.roundRect(-26, -20, 36, 14, 3);
  ctx.fill();
  ctx.beginPath();
  ctx.roundRect(-18, -33, 18, 14, 3);
  ctx.fill();
  ctx.fillStyle = '#bfe3ff';
  ctx.fillRect(-15, -30, 12, 8);
  ctx.fillStyle = '#d9dee6';
  ctx.beginPath();
  ctx.moveTo(10, -18);
  ctx.lineTo(22, -21);
  ctx.lineTo(27, 0);
  ctx.lineTo(10, -3);
  ctx.closePath();
  ctx.fill();
  ctx.fillStyle = '#24262b';
  for (const wx of [-18, 2]) {
    ctx.beginPath();
    ctx.arc(wx, -4, 5.5, 0, 6.29);
    ctx.fill();
  }
  ctx.fillStyle = clock % 0.5 < 0.25 ? '#ff7a00' : '#ffd25e';
  ctx.fillRect(-11, -37, 6, 4);
  ctx.restore();
};

const drawSanta = (ctx, s) => {
  const bob = Math.sin(clock * 3) * 5;
  ctx.save();
  ctx.translate(s.x, s.y + bob);
  ctx.scale(s.dir * 1.3, 1.3);
  for (let k = 0; k < 2; k++) {
    const dx = 52 + k * 26;
    const leg = Math.sin(clock * 10 + k) * 3;
    ctx.fillStyle = '#8a5a2b';
    ctx.beginPath();
    ctx.ellipse(dx + 8, -3, 10, 4.5, 0, 0, 6.29);
    ctx.fill();
    ctx.fillRect(dx + 14, -12, 4, 9);
    ctx.beginPath();
    ctx.ellipse(dx + 19, -13, 5, 3, -0.3, 0, 6.29);
    ctx.fill();
    ctx.strokeStyle = '#6b4220';
    ctx.lineWidth = 1.3;
    ctx.beginPath();
    ctx.moveTo(dx + 16, -15);
    ctx.lineTo(dx + 13, -22);
    ctx.moveTo(dx + 18, -16);
    ctx.lineTo(dx + 21, -23);
    ctx.moveTo(dx + 2, 0);
    ctx.lineTo(dx + 2 - leg, 7);
    ctx.moveTo(dx + 13, 0);
    ctx.lineTo(dx + 13 + leg, 7);
    ctx.stroke();
    if (k === 1) {
      ctx.fillStyle = '#ff3b3b';
      ctx.beginPath();
      ctx.arc(dx + 24, -13, 1.8, 0, 6.29);
      ctx.fill();
    }
  }
  ctx.strokeStyle = '#d8c7a0';
  ctx.lineWidth = 0.8;
  ctx.beginPath();
  ctx.moveTo(32, -6);
  ctx.lineTo(54, -4);
  ctx.stroke();
  ctx.fillStyle = '#c4162a';
  ctx.beginPath();
  ctx.moveTo(0, -10);
  ctx.lineTo(34, -10);
  ctx.lineTo(29, 4);
  ctx.lineTo(4, 4);
  ctx.closePath();
  ctx.fill();
  ctx.strokeStyle = '#ffcf5a';
  ctx.lineWidth = 1.6;
  ctx.beginPath();
  ctx.moveTo(-5, 7);
  ctx.quadraticCurveTo(15, 9, 36, 6);
  ctx.stroke();
  ctx.fillStyle = '#d42030';
  ctx.fillRect(15, -22, 10, 13);
  ctx.fillStyle = '#f8d2b0';
  ctx.beginPath();
  ctx.arc(20, -25, 4, 0, 6.29);
  ctx.fill();
  ctx.fillStyle = '#fff';
  ctx.beginPath();
  ctx.arc(20, -22, 4, 0, Math.PI);
  ctx.fill();
  ctx.fillStyle = '#d42030';
  ctx.beginPath();
  ctx.moveTo(15.5, -27);
  ctx.lineTo(24.5, -27);
  ctx.lineTo(17, -35);
  ctx.closePath();
  ctx.fill();
  ctx.fillStyle = '#fff';
  ctx.beginPath();
  ctx.arc(17, -35, 1.8, 0, 6.29);
  ctx.fill();
  ctx.fillStyle = '#3f8f3a';
  ctx.beginPath();
  ctx.arc(7, -13, 5.5, 0, 6.29);
  ctx.fill();
  ctx.restore();
};

hx.on('start', () => {
  makeSprites();
  ground = new Float32Array(Math.ceil(hx.w / COL) + 1);
  for (let i = 0; i < target(); i++) flakes.push(newFlake(false));
  syncDrifts();
});
hx.on('theme', makeSprites);
hx.on('resize', () => {
  const next = new Float32Array(Math.ceil(hx.w / COL) + 1);
  next.set(ground.subarray(0, Math.min(next.length, ground.length)));
  ground = next;
});
hx.on('blocks', syncDrifts);
hx.on('dblclick', (e) => {
  const d = e.block && drifts.get(e.block.id);
  if (d) {
    shake(d, 1);
    return;
  }
  if (!plow) {
    const dir = e.x < hx.w / 2 ? 1 : -1;
    plow = { x: dir > 0 ? -60 : hx.w + 60, dir };
  }
});
// A new prompt shakes the field's snow off, as if the message pushed through it.
hx.on('prompt', () => {
  for (const b of hx.blocks) if (b.kind === 'composer' && drifts.get(b.id)) shake(drifts.get(b.id), 0.7);
});

hx.on('frame', (t, dt) => {
  const ctx = hx.ctx;
  clock = t;
  ctx.clearRect(0, 0, hx.w, hx.h);
  if (!SPR.length) return;
  const p = hx.pointer;
  const want = target();
  while (flakes.length < want) flakes.push(newFlake(true));

  // Flakes: fall, sway, step away from the pointer and are carried by its swipe.
  for (let i = flakes.length - 1; i >= 0; i--) {
    const f = flakes[i];
    f.sway += dt * 1.3;
    let ax = 0;
    let ay = 0;
    if (p.inside) {
      const dx = f.x - p.x;
      const dy = f.y - p.y;
      const d2 = dx * dx + dy * dy;
      const R = 110;
      if (d2 < R * R && d2 > 1) {
        const d = Math.sqrt(d2);
        const k = (1 - d / R) ** 2;
        ax += (dx / d) * k * 900 + p.vx * k * 1.2;
        ay += (dy / d) * k * 900 + p.vy * k * 1.2;
      }
    }
    f.vx += ax * dt;
    f.vx *= 1 - Math.min(1, dt * 1.8);
    const fall = f.vy + ay * dt * 0.6;
    f.x += (f.vx + Math.sin(f.sway) * f.swayAmp * 0.6) * dt;
    f.y += fall * dt;
    f.rot += f.spin * dt;
    if (f.x < -30) f.x += hx.w + 60;
    if (f.x > hx.w + 30) f.x -= hx.w + 60;
    if (f.y > -5 && settle(f.x, f.y, f.spr.r * 0.4)) {
      flakes[i] = newFlake(true);
      continue;
    }
    if (f.y > hx.h + 20) flakes[i] = newFlake(true);
  }

  sweep();
  for (const d of drifts.values()) relax(d.cols, BLOCK_CAP);
  relax(ground, GROUND_CAP);

  // The drifts.
  ctx.fillStyle = light() ? 'rgba(236,243,252,0.97)' : 'rgba(244,248,255,0.95)';
  ctx.strokeStyle = light() ? 'rgba(120,150,190,0.45)' : 'rgba(190,215,255,0.5)';
  ctx.lineWidth = 1;
  ctx.lineJoin = 'round';
  for (const d of drifts.values()) drawDrift(ctx, d.cols, d.x, d.y + 1);
  drawDrift(ctx, ground, 0, hx.h + 1);

  // The flakes.
  for (const f of flakes) {
    const s = f.spr.img;
    ctx.globalAlpha = f.alpha;
    if (f.spr.crystal) {
      ctx.save();
      ctx.translate(f.x, f.y);
      ctx.rotate(f.rot);
      ctx.drawImage(s, -s.width / 2, -s.height / 2);
      ctx.restore();
    } else ctx.drawImage(s, f.x - s.width / 2, f.y - s.height / 2);
  }
  ctx.globalAlpha = 1;

  // Thrown snow and sparks.
  for (let i = puffs.length - 1; i >= 0; i--) {
    const q = puffs[i];
    q.life -= dt;
    q.vy += (q.spark ? 20 : 260) * dt;
    q.vx *= 1 - dt * 0.8;
    q.x += q.vx * dt;
    q.y += q.vy * dt;
    if (q.settle && q.vy > 0 && settle(q.x, q.y, q.r * 0.3)) {
      puffs.splice(i, 1);
      continue;
    }
    if (q.life <= 0 || q.y > hx.h + 10) {
      puffs.splice(i, 1);
      continue;
    }
    ctx.globalAlpha = Math.min(1, q.life);
    ctx.fillStyle = q.spark ? '#ffe27a' : light() ? '#e3ecf8' : '#fff';
    ctx.beginPath();
    ctx.arc(q.x, q.y, q.r, 0, 6.29);
    ctx.fill();
  }
  ctx.globalAlpha = 1;

  // The snowplough clears the ground in front of its blade.
  if (plow) {
    plow.x += plow.dir * 150 * dt;
    const bladeAt = Math.floor((plow.x + plow.dir * 30) / COL);
    for (let k = 0; k < 4; k++) {
      const j = bladeAt + k * plow.dir;
      if (j < 0 || j >= ground.length || ground[j] < 0.5) continue;
      const take = ground[j] * 0.5;
      ground[j] -= take;
      if (Math.random() < 0.6)
        puffs.push({
          x: j * COL,
          y: hx.h - ground[j] - 4,
          vx: plow.dir * (60 + Math.random() * 120),
          vy: -80 - Math.random() * 120,
          r: 1.3 + Math.random() * 2,
          life: 1.2,
          settle: false,
        });
    }
    drawPlow(ctx, plow);
    if (plow.x < -80 || plow.x > hx.w + 80) plow = null;
  }

  // Ded Moroz, now and then, a little faster while the agent works.
  nextSanta -= dt * (hx.agent.state === 'working' ? 1.6 : 1);
  if (!santa && nextSanta <= 0 && !hx.reduced) {
    const dir = Math.random() < 0.5 ? 1 : -1;
    santa = { x: dir > 0 ? -140 : hx.w + 140, y: 50 + Math.random() * Math.min(160, hx.h * 0.25), dir };
    nextSanta = 45 + Math.random() * 60;
  }
  if (santa) {
    santa.x += santa.dir * 120 * dt;
    if (Math.random() < 0.7)
      puffs.push({
        x: santa.x - santa.dir * 4,
        y: santa.y + 4 + Math.sin(clock * 3) * 5,
        vx: -santa.dir * 20,
        vy: 8,
        r: 1 + Math.random(),
        life: 1.1,
        spark: true,
      });
    drawSanta(ctx, santa);
    if (santa.x < -200 || santa.x > hx.w + 200) santa = null;
  }
});
