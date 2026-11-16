// «Ночник»: a warm lamp shines from the top corner of the chat. While the agent works it comes up slowly and breathes,
// with dust drifting in its beam; when the turn ends it dims to an ember; when the agent waits for the person it
// pulses in the accent colour. A built-in effect, and an example of the hx SDK (runtime.js).
let level = 0.15; // how bright the lamp is now, 0–1
let pulse = 0;
const motes = [];

const want = () => {
  const s = hx.agent.state;
  if (s === 'working') return 1;
  if (s === 'waiting') return 0.7;
  return 0.12;
};

const rgba = (rgb, a) => `rgba(${rgb[0]},${rgb[1]},${rgb[2]},${a})`;
const WARM = [255, 186, 110];
const parse = (css) => {
  const m = /rgba?\((\d+)[ ,]+(\d+)[ ,]+(\d+)/.exec(css || '');
  if (m) return [+m[1], +m[2], +m[3]];
  const h = /^#([0-9a-f]{6})$/i.exec(css || '');
  if (h) return [parseInt(h[1].slice(0, 2), 16), parseInt(h[1].slice(2, 4), 16), parseInt(h[1].slice(4), 16)];
  return [160, 140, 255];
};

hx.on('prompt', () => {
  pulse = 1;
});

hx.on('frame', (t, dt) => {
  const ctx = hx.ctx;
  const { w, h } = hx;
  ctx.clearRect(0, 0, w, h);
  const goal = want() * (0.45 + 0.55 * hx.power);
  // Up slowly, down slower: a lamp, not a switch.
  level += (goal - level) * Math.min(1, dt * (goal > level ? 0.9 : 0.45));
  pulse = Math.max(0, pulse - dt * 0.8);
  const waiting = hx.agent.state === 'waiting';
  const color = waiting ? parse(hx.colors.accent) : WARM;
  const breathe =
    hx.agent.state === 'working' ? 0.88 + 0.12 * Math.sin(t * 1.7) + 0.03 * Math.sin(t * 7.3) : 1;
  const wpulse = waiting ? 0.75 + 0.25 * Math.sin(t * 3) : 1;
  const a = Math.min(1, level * breathe * wpulse + pulse * 0.25);
  const light = hx.mode === 'light';

  // The lamp itself, a small shade in the corner.
  const lx = w - 34;
  const ly = 6;
  ctx.globalCompositeOperation = light ? 'multiply' : 'lighter';
  const R = Math.max(w, h) * 0.95;
  const glow = ctx.createRadialGradient(lx, ly, 4, lx, ly, R);
  const k = light ? 0.22 : 0.34;
  glow.addColorStop(0, rgba(color, k * a * 1.4));
  glow.addColorStop(0.18, rgba(color, k * a * 0.6));
  glow.addColorStop(0.5, rgba(color, k * a * 0.15));
  glow.addColorStop(1, rgba(color, 0));
  if (light) {
    // On a light page a glow is a warm tint, not more light.
    ctx.globalCompositeOperation = 'source-over';
  }
  ctx.fillStyle = glow;
  ctx.fillRect(0, 0, w, h);

  // The beam: a cone down and to the left.
  ctx.save();
  ctx.translate(lx, ly);
  ctx.rotate(Math.PI * 0.68 + Math.sin(t * 0.3) * 0.02);
  const len = Math.hypot(w, h) * 0.8;
  const beam = ctx.createLinearGradient(0, 0, len, 0);
  beam.addColorStop(0, rgba(color, (light ? 0.12 : 0.2) * a));
  beam.addColorStop(1, rgba(color, 0));
  ctx.fillStyle = beam;
  ctx.beginPath();
  ctx.moveTo(0, -8);
  ctx.lineTo(len, -len * 0.42);
  ctx.lineTo(len, len * 0.42);
  ctx.lineTo(0, 8);
  ctx.closePath();
  ctx.fill();
  ctx.restore();
  ctx.globalCompositeOperation = 'source-over';

  // Dust in the beam while it is bright.
  if (a > 0.3 && motes.length < 60 * hx.power + 10 && Math.random() < dt * 30) {
    const d = Math.random() * Math.hypot(w, h) * 0.6;
    const ang = Math.PI * 0.68 + (Math.random() - 0.5) * 0.7;
    motes.push({
      x: lx + Math.cos(ang) * d,
      y: ly + Math.sin(ang) * d,
      life: 4 + Math.random() * 4,
      r: 0.6 + Math.random() * 1.4,
      ph: Math.random() * 6,
    });
  }
  for (let i = motes.length - 1; i >= 0; i--) {
    const m = motes[i];
    m.life -= dt;
    m.x += Math.sin(t * 0.7 + m.ph) * 6 * dt - 3 * dt;
    m.y += Math.cos(t * 0.5 + m.ph) * 4 * dt + 2 * dt;
    if (m.life <= 0) {
      motes.splice(i, 1);
      continue;
    }
    ctx.globalAlpha = Math.min(1, m.life / 2) * a * 0.8;
    ctx.fillStyle = rgba(color, 1);
    ctx.beginPath();
    ctx.arc(m.x, m.y, m.r, 0, 6.29);
    ctx.fill();
  }
  ctx.globalAlpha = 1;

  // The shade and the bulb.
  ctx.fillStyle = light ? '#3a3f4a' : '#20232b';
  ctx.beginPath();
  ctx.moveTo(lx - 16, ly + 12);
  ctx.lineTo(lx + 16, ly + 12);
  ctx.lineTo(lx + 8, ly - 4);
  ctx.lineTo(lx - 8, ly - 4);
  ctx.closePath();
  ctx.fill();
  const bulb = ctx.createRadialGradient(lx, ly + 13, 0, lx, ly + 13, 12);
  bulb.addColorStop(0, rgba([255, 244, 220], 0.4 + 0.6 * a));
  bulb.addColorStop(1, rgba(color, 0));
  ctx.fillStyle = bulb;
  ctx.beginPath();
  ctx.arc(lx, ly + 13, 12, 0, 6.29);
  ctx.fill();
});
