// Helpers of the effects that paint a picture under the chat (newyear, elves): the picture fitted to the stage like
// CSS cover and baked once per size, points of the picture mapped to the stage, the warm lights found in it to twinkle,
// a flickering glow, rising steam, a bow that drops on a block.
/* global hx */
const SceneLib = (() => {
  const load = (src) => {
    const i = new Image();
    i.src = src;
    return i;
  };
  const ready = (img) => img.complete && img.naturalWidth > 0;

  /** The picture fitted like `cover`, anchored at (ax, ay) of its free room, baked into a canvas of the stage's size. */
  const Cover = (img, ax = 0.5, ay = 0.5) => {
    const self = { s: 1, ox: 0, oy: 0, canvas: null, key: '' };
    self.fit = () => {
      if (!ready(img)) return false;
      const key = `${hx.w}x${hx.h}`;
      if (self.key === key) return true;
      self.key = key;
      const s = Math.max(hx.w / img.naturalWidth, hx.h / img.naturalHeight);
      self.s = s;
      self.ox = (hx.w - img.naturalWidth * s) * ax;
      self.oy = (hx.h - img.naturalHeight * s) * ay;
      const c = document.createElement('canvas');
      c.width = Math.max(1, Math.round(hx.w * hx.dpr));
      c.height = Math.max(1, Math.round(hx.h * hx.dpr));
      const g = c.getContext('2d');
      g.imageSmoothingQuality = 'high';
      g.drawImage(
        img,
        self.ox * hx.dpr,
        self.oy * hx.dpr,
        img.naturalWidth * s * hx.dpr,
        img.naturalHeight * s * hx.dpr,
      );
      self.canvas = c;
      return true;
    };
    /** A point of the picture (0–1 of its size) on the stage. */
    self.at = (u, v) => [self.ox + u * img.naturalWidth * self.s, self.oy + v * img.naturalHeight * self.s];
    /** A length of the picture (share of its width) on the stage. */
    self.len = (u) => u * img.naturalWidth * self.s;
    self.draw = (ctx) => {
      if (self.fit()) ctx.drawImage(self.canvas, 0, 0, hx.w, hx.h);
    };
    return self;
  };

  /** The warm bright points of a region of the picture (0–1 box): its bulbs and flames, to twinkle. */
  const lights = (img, box, max = 40) => {
    const W = 400;
    const H = Math.round((W * img.naturalHeight) / img.naturalWidth);
    const c = document.createElement('canvas');
    c.width = W;
    c.height = H;
    const g = c.getContext('2d');
    g.drawImage(img, 0, 0, W, H);
    let data;
    try {
      data = g.getImageData(0, 0, W, H).data;
    } catch {
      return [];
    }
    const found = [];
    const [u0, v0, u1, v1] = box;
    for (let y = Math.floor(v0 * H); y < v1 * H; y++)
      for (let x = Math.floor(u0 * W); x < u1 * W; x++) {
        const i = (y * W + x) * 4;
        const r = data[i];
        const gg = data[i + 1];
        const b = data[i + 2];
        if (r > 235 && gg > 190 && b < 200 && r + gg > 450) {
          if (found.every((p) => Math.abs(p[0] - x) + Math.abs(p[1] - y) > 6)) found.push([x, y]);
        }
      }
    found.sort(() => Math.random() - 0.5);
    return found.slice(0, max).map(([x, y]) => ({
      u: (x + 0.5) / W,
      v: (y + 0.5) / H,
      ph: Math.random() * 6.28,
      sp: 0.6 + Math.random() * 1.6,
    }));
  };

  /** A soft additive glow. */
  const glow = (ctx, x, y, r, rgb, a) => {
    if (a <= 0.003) return;
    const g = ctx.createRadialGradient(x, y, 0, x, y, r);
    g.addColorStop(0, `rgba(${rgb[0]},${rgb[1]},${rgb[2]},${a})`);
    g.addColorStop(0.35, `rgba(${rgb[0]},${rgb[1]},${rgb[2]},${a * 0.35})`);
    g.addColorStop(1, `rgba(${rgb[0]},${rgb[1]},${rgb[2]},0)`);
    ctx.fillStyle = g;
    ctx.fillRect(x - r, y - r, r * 2, r * 2);
  };

  /** A flame's or a filament's flicker, 0.75–1.05, smooth. */
  const flicker = (t, ph) =>
    0.9 +
    0.06 * Math.sin(t * 7.3 + ph) +
    0.04 * Math.sin(t * 13.1 + ph * 2) +
    0.03 * Math.sin(t * 23.7 + ph * 3);

  /** Steam rising from a point: soft puffs that widen, drift and fade. */
  const Steam = () => {
    const puffs = [];
    return (ctx, x, y, scale, dt) => {
      if (Math.random() < dt * 9)
        puffs.push({
          x: x + (Math.random() - 0.5) * 8 * scale,
          y,
          r: 4 * scale,
          life: 0,
          ph: Math.random() * 6,
        });
      for (let i = puffs.length - 1; i >= 0; i--) {
        const p = puffs[i];
        p.life += dt;
        if (p.life > 3) {
          puffs.splice(i, 1);
          continue;
        }
        p.y -= 16 * scale * dt;
        p.x += Math.sin(p.life * 2 + p.ph) * 6 * scale * dt;
        p.r += 5 * scale * dt;
        const a = 0.13 * Math.sin((p.life / 3) * Math.PI);
        const g = ctx.createRadialGradient(p.x, p.y, 0, p.x, p.y, p.r);
        g.addColorStop(0, `rgba(255,248,240,${a})`);
        g.addColorStop(1, 'rgba(255,248,240,0)');
        ctx.fillStyle = g;
        ctx.fillRect(p.x - p.r, p.y - p.r, p.r * 2, p.r * 2);
      }
    };
  };

  /** Snow falling behind a window of the picture, clipped to it. */
  const WindowSnow = (n) => {
    const f = [];
    for (let i = 0; i < n; i++)
      f.push({ u: Math.random(), v: Math.random(), s: 0.4 + Math.random(), ph: Math.random() * 6 });
    return (ctx, x, y, w, h, t, dt) => {
      ctx.save();
      ctx.beginPath();
      ctx.rect(x, y, w, h);
      ctx.clip();
      ctx.fillStyle = '#fff';
      for (const p of f) {
        p.v += dt * 0.05 * (0.6 + p.s);
        if (p.v > 1) {
          p.v = 0;
          p.u = Math.random();
        }
        const px = x + (p.u + Math.sin(t * 0.8 + p.ph) * 0.02) * w;
        const py = y + p.v * h;
        ctx.globalAlpha = 0.4 + 0.5 * (p.s / 1.4);
        ctx.beginPath();
        ctx.arc(px, py, 0.5 + p.s * 0.9, 0, 6.29);
        ctx.fill();
      }
      ctx.restore();
      ctx.globalAlpha = 1;
    };
  };

  /** Bows tied on blocks: a bow drops on a block's corner, bounces and stays (the last few). */
  const Bows = (img, tagImg) => {
    const list = [];
    const self = {
      tie(blockId, withTag) {
        if (list.some((b) => b.id === blockId)) return;
        list.push({ id: blockId, t: 0, tag: withTag });
        if (list.length > 6) list.shift();
      },
      draw(ctx, dt, h = 54) {
        for (const b of list) {
          const blk = hx.blocks.find((x) => x.id === b.id);
          if (!blk) continue;
          b.t += dt;
          // Drops from above with a springy landing.
          const k = Math.min(1, b.t / 0.45);
          const fall = (1 - k) * (1 - k) * -120;
          const squash =
            b.t > 0.45 ? 1 + Math.sin((b.t - 0.45) * 18) * Math.exp(-(b.t - 0.45) * 6) * 0.15 : 1;
          const x = blk.x + blk.w - 22;
          const y = blk.y + 8 + fall;
          if (ready(img)) {
            const w = (img.naturalWidth * h) / img.naturalHeight;
            ctx.save();
            ctx.translate(x, y);
            ctx.scale(1 / squash, squash);
            ctx.rotate(0.12);
            ctx.shadowColor = 'rgba(0,0,0,0.35)';
            ctx.shadowBlur = 8;
            ctx.shadowOffsetY = 4;
            ctx.drawImage(img, -w / 2, -h / 2, w, h);
            ctx.restore();
          }
          if (b.tag && tagImg && ready(tagImg) && b.t > 0.3) {
            const th = 46;
            const tw = (tagImg.naturalWidth * th) / tagImg.naturalHeight;
            const sw = Math.sin(b.t * 2.4) * 0.12 * Math.exp(-b.t * 0.3) + 0.05;
            ctx.save();
            ctx.translate(x - 4, y + 10);
            ctx.rotate(sw);
            ctx.drawImage(tagImg, -tw / 2, 0, tw, th);
            ctx.restore();
          }
        }
      },
    };
    return self;
  };

  return { load, ready, Cover, lights, glow, flicker, Steam, WindowSnow, Bows };
})();
