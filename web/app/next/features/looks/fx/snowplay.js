// «Снежная игра»: real-looking snow (snow-lib.js) that settles on the chat's blocks and along the bottom, drifts away
// from the pointer and is swept by a quick swipe; a double click on an empty place calls the snowplough, which drives
// along the bottom and throws the snow ahead of its blade; a double click on a block shakes its snow off; now and then
// Ded Moroz flies by in his sleigh. The pictures are ART_SNOW (art-snow.js).
/* global hx, Snow, ART_SNOW */
const snow = Snow({ density: 1, caps: true, ground: true, start: 0.55 });
const load = (src) => {
  const i = new Image();
  i.src = src;
  return i;
};
const PLOW = load(ART_SNOW.plow);
const SLEIGH = load(ART_SNOW.sleigh);
let plow = null;
let santa = null;
let nextSanta = 18;
const sparks = [];

hx.on('dblclick', (e) => {
  if (e.block && snow.caps.get(e.block.id)) return snow.shake(e.block.id, 1);
  if (!plow) {
    const dir = e.x < hx.w / 2 ? 1 : -1;
    plow = { x: dir > 0 ? -160 : hx.w + 160, dir, t: 0 };
  }
});
// A new prompt pushes the field's snow off, as if the message went through it.
hx.on('prompt', () => {
  for (const b of hx.blocks) if (b.kind === 'composer') snow.shake(b.id, 0.6);
});

const drawSprite = (img, x, y, h, flip, tilt) => {
  if (!img.complete || !img.naturalWidth) return 0;
  const w = (img.naturalWidth * h) / img.naturalHeight;
  const ctx = hx.ctx;
  ctx.save();
  ctx.translate(x, y);
  ctx.rotate(tilt || 0);
  ctx.scale(flip ? -1 : 1, 1);
  ctx.drawImage(img, -w / 2, -h, w, h);
  ctx.restore();
  return w;
};

hx.on('frame', (t, dt) => {
  const ctx = hx.ctx;
  ctx.clearRect(0, 0, hx.w, hx.h);
  snow.update(t, dt);
  snow.draw(ctx, t, 'all');

  // The snowplough: bounces on its suspension, its beacon turns, the blade throws the snow up and ahead.
  if (plow) {
    plow.t += dt;
    plow.x += plow.dir * 140 * dt;
    const g = snow.ground;
    const H = 74;
    const y = hx.h - 2 + Math.sin(plow.t * 14) * 1.2;
    const tilt = Math.sin(plow.t * 7) * 0.012;
    // Its shadow on the snow.
    ctx.globalAlpha = 0.35;
    ctx.fillStyle = '#0a1a33';
    ctx.beginPath();
    ctx.ellipse(plow.x, hx.h - 3, 70, 7, 0, 0, Math.PI * 2);
    ctx.fill();
    ctx.globalAlpha = 1;
    // The headlight-eyes light the snow ahead.
    const hl = ctx.createRadialGradient(
      plow.x + plow.dir * 90,
      hx.h - 20,
      2,
      plow.x + plow.dir * 90,
      hx.h - 20,
      90,
    );
    hl.addColorStop(0, 'rgba(255,236,190,0.35)');
    hl.addColorStop(1, 'rgba(255,236,190,0)');
    ctx.fillStyle = hl;
    ctx.fillRect(plow.x + plow.dir * 0 - 180, hx.h - 120, 360, 120);
    const w = drawSprite(PLOW, plow.x, y, H, plow.dir < 0, tilt);
    // The beacon: a turning amber flash on the roof.
    const bx = plow.x + plow.dir * w * (0.434 - 0.5);
    const by = y - H * 0.93;
    const flash = Math.max(0, Math.sin(plow.t * 9));
    const bg = ctx.createRadialGradient(bx, by, 0, bx, by, 40);
    bg.addColorStop(0, `rgba(255,170,40,${0.7 * flash})`);
    bg.addColorStop(1, 'rgba(255,170,40,0)');
    ctx.globalCompositeOperation = 'lighter';
    ctx.fillStyle = bg;
    ctx.fillRect(bx - 40, by - 40, 80, 80);
    ctx.globalCompositeOperation = 'source-over';
    // The blade clears the ground and throws it.
    if (g) {
      const at = Math.floor((plow.x + plow.dir * w * 0.5) / snow.COL);
      for (let k = 0; k < 5; k++) {
        const j = at + k * plow.dir;
        if (j < 0 || j >= g.length || g[j] < 0.6) continue;
        const take = g[j] * 0.55;
        g[j] -= take;
        if (Math.random() < 0.8)
          snow.throwSnow(
            j * snow.COL,
            hx.h - g[j] - 6,
            plow.dir * (80 + Math.random() * 160),
            -120 - Math.random() * 140,
            2,
            3.2,
          );
      }
      snow.markGround();
    }
    if (plow.x < -220 || plow.x > hx.w + 220) plow = null;
  }

  // Ded Moroz, now and then; a little more often while the agent works.
  nextSanta -= dt * (hx.agent.state === 'working' ? 1.5 : 1);
  if (!santa && nextSanta <= 0 && !hx.reduced) {
    const dir = Math.random() < 0.5 ? 1 : -1;
    santa = { x: dir > 0 ? -260 : hx.w + 260, y0: 60 + Math.random() * Math.min(140, hx.h * 0.2), dir, t: 0 };
    nextSanta = 50 + Math.random() * 60;
  }
  if (santa) {
    santa.t += dt;
    santa.x += santa.dir * 150 * dt;
    const y = santa.y0 + Math.sin(santa.t * 1.6) * 14;
    const tilt = Math.cos(santa.t * 1.6) * 0.06 * santa.dir;
    const w = drawSprite(SLEIGH, santa.x, y, 92, santa.dir < 0, tilt);
    // A trail of golden dust from the runners.
    for (let i = 0; i < 3; i++)
      sparks.push({
        x: santa.x - santa.dir * w * 0.42 + (Math.random() - 0.5) * 20,
        y: y - 8 + (Math.random() - 0.5) * 8,
        vx: -santa.dir * (20 + Math.random() * 30),
        vy: 10 + Math.random() * 20,
        life: 1 + Math.random() * 0.8,
        s: 1 + Math.random() * 2,
      });
    if (santa.x < -320 || santa.x > hx.w + 320) santa = null;
  }
  ctx.globalCompositeOperation = 'lighter';
  for (let i = sparks.length - 1; i >= 0; i--) {
    const s = sparks[i];
    s.life -= dt;
    if (s.life <= 0) {
      sparks.splice(i, 1);
      continue;
    }
    s.x += s.vx * dt;
    s.y += s.vy * dt;
    const a = Math.min(1, s.life) * (0.6 + 0.4 * Math.sin(t * 20 + i));
    const g = ctx.createRadialGradient(s.x, s.y, 0, s.x, s.y, s.s * 3);
    g.addColorStop(0, `rgba(255,236,160,${a})`);
    g.addColorStop(1, 'rgba(255,200,80,0)');
    ctx.fillStyle = g;
    ctx.fillRect(s.x - s.s * 3, s.y - s.s * 3, s.s * 6, s.s * 6);
  }
  ctx.globalCompositeOperation = 'source-over';
});
