// «Фабрика эльфов»: the chat inside Santa's workshop. Under it (layer back) the workshop lives — gifts ride the
// conveyor into the glowing machine, lanterns flicker, the garland twinkles, snow falls behind the arched window. Over it
// (layer front) every prompt stands on a conveyor belt of its own: when it is sent the belt brings it in; while the
// agent works, an elf hammers at it with sparks flying and another one keeps bringing gifts; when the answer is done it
// gets a red bow and a tag — packed. The pictures are ART_ELVES (art-elves.js) and ART_BOW (art-bow.js).
/* global hx, Snow, SceneLib, ART_ELVES, ART_BOW */
const L = SceneLib;
const A = ART_ELVES;
if (hx.layer === 'back') {
  const img = L.load(A.bg.src);
  const cover = L.Cover(img, 0.5, 0.5);
  const winSnow = L.WindowSnow(70);
  let bulbs = null;
  let belt = 0;
  // The row of gifts on the picture's conveyor, cut out once, to roll.
  const ROW = { u0: 0.197, u1: 0.748, v0: 0.776, v1: 0.912, end: 0.872 };
  let row = null;
  const embers = [];
  hx.on('frame', (t, dt) => {
    const ctx = hx.ctx;
    ctx.clearRect(0, 0, hx.w, hx.h);
    if (!L.ready(img)) return;
    cover.draw(ctx);
    const k = cover.len(1) / 1600;
    if (!bulbs) bulbs = L.lights(img, [0.24, 0.14, 0.76, 0.23], 30);
    if (!row) {
      const W = img.naturalWidth;
      const H = img.naturalHeight;
      row = document.createElement('canvas');
      row.width = Math.round((ROW.u1 - ROW.u0) * W);
      row.height = Math.round((ROW.v1 - ROW.v0) * H);
      row
        .getContext('2d')
        .drawImage(img, ROW.u0 * W, ROW.v0 * H, row.width, row.height, 0, 0, row.width, row.height);
    }
    // The conveyor rolls: faster while the agent works.
    belt += dt * (hx.agent.state === 'working' ? 46 : 16) * k;
    const [x0, y0] = cover.at(ROW.u0, ROW.v0);
    const [xe, y1] = cover.at(ROW.end, ROW.v1);
    const tw = row.width * cover.s;
    ctx.save();
    ctx.beginPath();
    ctx.rect(x0, y0, xe - x0, y1 - y0);
    ctx.clip();
    for (let x = x0 - tw + (belt % tw); x < xe; x += tw) ctx.drawImage(row, x, y0, tw, y1 - y0);
    ctx.restore();
    // Behind the arched window: snow.
    const [wx, wy] = cover.at(0.33, 0.0);
    const [wx2, wy2] = cover.at(0.66, 0.14);
    winSnow(ctx, wx, wy, wx2 - wx, wy2 - wy, t, dt);
    ctx.globalCompositeOperation = 'lighter';
    for (const b of bulbs) {
      const a = 0.25 + 0.4 * (0.5 + 0.5 * Math.sin(t * b.sp + b.ph));
      L.glow(ctx, ...cover.at(b.u, b.v), 15 * k, [255, 205, 120], a * hx.power);
    }
    for (const [u, v, ph] of [
      [0.284, 0.069, 0.2],
      [0.712, 0.069, 1.7],
      [0.027, 0.84, 3.1],
    ])
      L.glow(ctx, ...cover.at(u, v), 80 * k, [255, 175, 80], 0.32 * L.flicker(t * 1.5, ph));
    L.glow(ctx, ...cover.at(0.06, 0.255), 60 * k, [255, 220, 120], 0.16 + 0.06 * Math.sin(t * 1.3));
    // The machine's oven: a breathing glow and embers drifting up.
    const [ox, oy] = cover.at(0.924, 0.68);
    L.glow(
      ctx,
      ox,
      oy,
      120 * k,
      [255, 140, 50],
      0.26 * L.flicker(t, 4) * (hx.agent.state === 'working' ? 1.3 : 1),
    );
    if (Math.random() < dt * 5) embers.push({ x: ox + (Math.random() - 0.5) * 50 * k, y: oy, life: 1.5 });
    for (let i = embers.length - 1; i >= 0; i--) {
      const e = embers[i];
      e.life -= dt;
      e.y -= 24 * k * dt;
      e.x += Math.sin(e.life * 6) * 10 * k * dt;
      if (e.life <= 0) embers.splice(i, 1);
      else L.glow(ctx, e.x, e.y, 4 * k, [255, 160, 60], 0.6 * e.life);
    }
    ctx.globalCompositeOperation = 'source-over';
  });
} else {
  const snow = Snow({ density: 0, caps: true, ground: false, start: 0.7, capMax: 16, tint: [255, 214, 170] });
  const beltImg = L.load(A.belt.src);
  const hammer = L.load(A.hammer.src);
  const carry = L.load(A.carry.src);
  const gifts = A.gifts.map((g) => L.load(g.src));
  const bows = L.Bows(L.load(ART_BOW.bow), L.load(ART_BOW.tag));
  const belts = new Map(); // user block id -> {off, speed, born}
  const sparks = [];
  const flying = []; // gifts the carrying elf drops on the belt
  let runner = { x: -80, carrying: true, gift: 0 };
  let lastHit = 0;

  const newestOf = (kind) => {
    let best = null;
    for (const b of hx.blocks) if (b.kind === kind && (!best || b.y > best.y)) best = b;
    return best;
  };

  hx.on('prompt', (e) => {
    if (!e.block) return;
    belts.set(e.block.id, { off: 0, speed: 420, born: 0 });
  });
  hx.on('answer-end', (e) => e.block && bows.tie(e.block.id, true));
  hx.on('dblclick', (e) => e.block && snow.shake(e.block.id, 1));

  const sprite = (ctx, img, x, y, h, flip, rot, sx, sy) => {
    if (!L.ready(img)) return 0;
    const w = (img.naturalWidth * h) / img.naturalHeight;
    ctx.save();
    ctx.translate(x, y);
    if (rot) ctx.rotate(rot);
    ctx.scale((flip ? -1 : 1) * (sx || 1), sy || 1);
    ctx.drawImage(img, -w / 2, -h, w, h);
    ctx.restore();
    return w;
  };

  // A belt under a block: the tile repeated, rolled by off.
  const drawBelt = (ctx, b, st) => {
    if (!L.ready(beltImg)) return;
    const h = 22;
    const tw = (beltImg.naturalWidth * h) / beltImg.naturalHeight;
    const x0 = b.x - 34;
    const x1 = b.x + b.w + 34;
    const y = b.y + b.h + 3;
    ctx.save();
    ctx.beginPath();
    ctx.roundRect(x0, y, x1 - x0, h, 6);
    ctx.clip();
    ctx.shadowColor = 'rgba(0,0,0,0.4)';
    for (let x = x0 - tw + (st.off % tw); x < x1; x += tw) ctx.drawImage(beltImg, x, y, tw + 0.5, h);
    ctx.restore();
  };

  hx.on('frame', (t, dt) => {
    const ctx = hx.ctx;
    ctx.clearRect(0, 0, hx.w, hx.h);
    const working = hx.agent.state === 'working';
    const user = newestOf('user');
    // Every prompt on screen stands on its belt; the newest rolls while the agent works, a new one rolls in fast.
    for (const b of hx.blocks) {
      if (b.kind !== 'user') continue;
      let st = belts.get(b.id);
      if (!st) belts.set(b.id, (st = { off: 0, speed: 0, born: 1 }));
      const target = user && b.id === user.id && working ? 30 : 0;
      st.speed += (target - st.speed) * Math.min(1, dt * 1.6);
      st.off += st.speed * dt;
      drawBelt(ctx, b, st);
    }
    snow.update(t, dt);
    snow.draw(ctx, t, 'lying');

    // The elves work at the newest prompt while the agent works.
    if (user && working && user.x > 120) {
      const fy = user.y + user.h + 4;
      const ex = user.x - 44;
      // The hammer comes down about twice a second: the elf leans in, squashes on the hit.
      const ph = (t * 2.1) % 1;
      const lean = ph < 0.7 ? -0.12 * (ph / 0.7) : 0.18 * Math.sin(((ph - 0.7) / 0.3) * Math.PI);
      const sq = ph > 0.85 ? 1 - 0.06 * Math.sin(((ph - 0.85) / 0.15) * Math.PI) : 1;
      sprite(ctx, hammer, ex, fy, 92, false, lean, 1 / sq, sq);
      if (ph > 0.85 && t - lastHit > 0.3) {
        lastHit = t;
        for (let i = 0; i < 10; i++)
          sparks.push({
            x: user.x + 2,
            y: fy - 30 + Math.random() * 10,
            vx: 40 + Math.random() * 160,
            vy: -60 - Math.random() * 160,
            life: 0.4 + Math.random() * 0.5,
            gold: Math.random() < 0.5,
          });
      }
      // The other elf brings gifts from the left edge and drops them on the belt.
      runner.x += 70 * dt;
      const rx = runner.x;
      if (runner.carrying) {
        sprite(ctx, carry, rx, fy + Math.abs(Math.sin(t * 9)) * -3, 86, false, Math.sin(t * 9) * 0.04);
        if (rx > ex - 60) {
          runner.carrying = false;
          flying.push({
            x: rx + 20,
            y: fy - 56,
            vy: -60,
            img: gifts[runner.gift % gifts.length],
            life: 2.4,
            land: fy - 2,
          });
          runner.gift++;
        }
      } else if (rx > ex - 40) {
        runner = { x: -80, carrying: true, gift: runner.gift };
      }
    } else runner.x = Math.min(runner.x, -80);

    // Gifts dropped on the belt ride it to the prompt and pop into sparkles.
    for (let i = flying.length - 1; i >= 0; i--) {
      const g = flying[i];
      g.life -= dt;
      g.vy += 600 * dt;
      g.y = Math.min(g.land, g.y + g.vy * dt);
      if (g.y >= g.land) {
        g.vy = 0;
        g.x += 30 * dt;
      }
      sprite(ctx, g.img, g.x, g.y, 32, false, 0);
      if (g.life <= 0) {
        for (let k = 0; k < 8; k++)
          sparks.push({
            x: g.x,
            y: g.y - 12,
            vx: (Math.random() - 0.5) * 120,
            vy: -Math.random() * 120,
            life: 0.6,
            gold: true,
          });
        flying.splice(i, 1);
      }
    }

    ctx.globalCompositeOperation = 'lighter';
    for (let i = sparks.length - 1; i >= 0; i--) {
      const s = sparks[i];
      s.life -= dt;
      if (s.life <= 0) {
        sparks.splice(i, 1);
        continue;
      }
      s.vy += 520 * dt;
      s.x += s.vx * dt;
      s.y += s.vy * dt;
      L.glow(ctx, s.x, s.y, 5, s.gold ? [255, 210, 110] : [255, 250, 230], Math.min(1, s.life * 2));
    }
    ctx.globalCompositeOperation = 'source-over';
    bows.draw(ctx, dt, 50);
  });
}
