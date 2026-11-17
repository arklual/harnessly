// The animated backgrounds of a look, drawn on a 2D canvas: in a worker on an OffscreenCanvas where the browser has
// it (scene-worker.ts), else on the page's own thread (backdrop.tsx). Each scene keeps its state in plain arrays, draws
// only shapes and ready sprites (no text layout, no DOM), batches what shares a colour into one path, and takes the
// frame's time step, so it runs at the screen's rate.
//
// `detail` (0.35–1) is how much a scene draws: the runner lowers it when frames run late (runner.ts), so a weak device
// keeps 60 frames a second with fewer particles, shorter trails and fewer rings, never a lower rate.
// SCENE_SCALE is the canvas' pixels per CSS pixel: soft scenes (aurora, lava) are drawn small and scaled up by CSS.
import type { ISceneColors } from '@/next/features/looks/tokens';
import type { TScene } from '@/next/features/looks/types';

type TCtx = OffscreenCanvasRenderingContext2D | CanvasRenderingContext2D;
type TCanvas = OffscreenCanvas | HTMLCanvasElement;

export interface IScene {
  /** Draws one frame: `t` seconds since the start, `dt` since the last frame (capped). */
  frame: (ctx: TCtx, t: number, dt: number) => void;
  /** A scene that does not move (paper) is drawn once. */
  still?: boolean;
}

/** How small a scene is drawn, of the CSS size (the device's pixels are capped at 1.5 before). */
export const SCENE_SCALE: Record<TScene, number> = {
  matrix: 1,
  stars: 1,
  aurora: 0.5,
  rain: 1,
  synthwave: 0.75,
  twinkle: 1,
  paper: 1,
  lava: 0.2,
  snow: 1,
  topo: 0.75,
};

/** A small seeded random, so a scene looks the same on every device and after a resize. */
const rng = (seed: number) => () => {
  seed = (seed * 1664525 + 1013904223) >>> 0;
  return seed / 4294967296;
};

/** A canvas off the page: an OffscreenCanvas where there is one (a worker has no document). */
const spriteCanvas = (w: number, h: number): TCanvas | null => {
  if (typeof OffscreenCanvas !== 'undefined') return new OffscreenCanvas(w, h);
  if (typeof document !== 'undefined') {
    const c = document.createElement('canvas');
    c.width = w;
    c.height = h;
    return c;
  }
  return null;
};

/** How many alpha steps a batched scene draws in: one path and one fill per step. */
const STEPS = 6;

const GLYPHS = '0123456789ABCDEFXYZ<>/{}[]=+*#$%&@:;|~';

const matrix = (w: number, h: number, c: ISceneColors, speed: number, detail: number, px: number): IScene => {
  const r = rng(7);
  const size = 16;
  const cols = Math.ceil(w / size);
  const heads = Array.from({ length: cols }, () => r() * h);
  const rates = Array.from({ length: cols }, () => (40 + r() * 90) * speed);
  const trail = Math.max(6, Math.round(18 * detail));
  // Every glyph in every colour, drawn once: a frame copies sprites instead of laying out text.
  const inks = [...c.hues, c.ink];
  const cell = Math.ceil(size * px);
  const atlas = spriteCanvas(cell * GLYPHS.length, cell * inks.length);
  const actx = atlas?.getContext('2d') as TCtx | null | undefined;
  if (actx) {
    actx.font = `${(size - 2) * px}px ui-monospace, monospace`;
    actx.textBaseline = 'top';
    inks.forEach((ink, row) => {
      actx.fillStyle = ink;
      for (let g = 0; g < GLYPHS.length; g++) actx.fillText(GLYPHS[g], g * cell, row * cell);
    });
  }
  const glyph = (i: number, row: number) => (i * 31 + row * 7) % GLYPHS.length;
  const draw = (ctx: TCtx, g: number, ink: number, x: number, y: number) => {
    if (atlas) ctx.drawImage(atlas, g * cell, ink * cell, cell, cell, x, y, size, size);
    else {
      ctx.fillStyle = inks[ink];
      ctx.fillText(GLYPHS[g], x, y);
    }
  };
  return {
    frame: (ctx, t, dt) => {
      ctx.fillStyle = c.bg;
      ctx.fillRect(0, 0, w, h);
      if (!atlas) {
        ctx.font = `${size - 2}px ui-monospace, monospace`;
        ctx.textBaseline = 'top';
      }
      const inkRow = inks.length - 1;
      for (let i = 0; i < cols; i++) {
        heads[i] += rates[i] * dt;
        if (heads[i] - trail * size > h) heads[i] = -r() * h * 0.5;
      }
      // By the trail's step: one alpha for a whole row of glyphs across the columns.
      for (let k = trail - 1; k >= 0; k--) {
        ctx.globalAlpha = k === 0 ? 0.9 : (1 - k / trail) * 0.45;
        for (let i = 0; i < cols; i++) {
          const headRow = Math.floor(heads[i] / size);
          const row = headRow - k;
          if (row < 0 || row * size > h) continue;
          const flicker = k === 2 ? Math.floor(t * 8 + i) % 5 : 0;
          draw(ctx, glyph(i, row + flicker), k === 0 ? inkRow : i % c.hues.length, i * size, row * size);
        }
      }
      ctx.globalAlpha = 1;
    },
  };
};

const stars = (w: number, h: number, c: ISceneColors, speed: number, detail: number): IScene => {
  const r = rng(11);
  const n = Math.max(40, Math.round(Math.min(420, (w * h) / 4200) * detail));
  const xs = Float32Array.from({ length: n }, () => r() * w);
  const ys = Float32Array.from({ length: n }, () => r() * h);
  const zs = Float32Array.from({ length: n }, () => 0.2 + r() * 0.8);
  const ph = Float32Array.from({ length: n }, () => r() * 6.28);
  const step = new Uint8Array(n);
  return {
    frame: (ctx, t, dt) => {
      ctx.fillStyle = c.bg;
      ctx.fillRect(0, 0, w, h);
      for (let i = 0; i < n; i++) {
        xs[i] -= zs[i] * 6 * speed * dt;
        if (xs[i] < 0) xs[i] += w;
        const a = (0.35 + 0.65 * zs[i]) * (0.6 + 0.4 * Math.sin(t * 2 * speed + ph[i]));
        step[i] = Math.min(STEPS - 1, Math.floor(a * STEPS));
      }
      // The coloured stars (every ninth) and the white ones, each alpha step in one fill.
      for (const coloured of [false, true]) {
        ctx.fillStyle = coloured ? c.hues[0] : c.ink;
        for (let s = 0; s < STEPS; s++) {
          ctx.globalAlpha = (s + 0.5) / STEPS;
          ctx.beginPath();
          for (let i = 0; i < n; i++) {
            if (step[i] !== s || (i % 9 === 0) !== coloured) continue;
            const size = zs[i] > 0.85 ? 2 : 1.2;
            ctx.rect(xs[i], ys[i], size, size);
          }
          ctx.fill();
        }
      }
      ctx.globalAlpha = 1;
    },
  };
};

/** One ray of an aurora: a thin vertical strip, bright at its lower edge (green oxygen light), fading up through
 *  teal into the faint red-violet of the high atmosphere. Drawn stretched for every ray of every curtain. */
const auroraRay = (low: [number, number, number], high: [number, number, number]): TCanvas | null => {
  const H = 256;
  const c = spriteCanvas(4, H);
  const g = c?.getContext('2d') as TCtx | null;
  if (!c || !g) return null;
  const grad = g.createLinearGradient(0, H, 0, 0);
  const rgba = (k: [number, number, number], a: number) => `rgba(${k[0]},${k[1]},${k[2]},${a})`;
  grad.addColorStop(0, rgba(low, 0));
  grad.addColorStop(0.035, 'rgba(215,255,225,0.95)');
  grad.addColorStop(0.1, rgba(low, 0.85));
  grad.addColorStop(0.32, rgba(low, 0.42));
  grad.addColorStop(0.6, rgba(high, 0.2));
  grad.addColorStop(1, rgba(high, 0));
  g.fillStyle = grad;
  g.fillRect(0, 0, 4, H);
  return c;
};

/** Northern lights: curtains of thin rays hanging from a folded, slowly drifting lower edge; bright pulses run along
 *  each curtain, its rays flicker on their own, a starry night behind. */
const aurora = (w: number, h: number, c: ISceneColors, speed: number, detail: number, px: number): IScene => {
  const r = rng(41);
  const dark = c.mode === 'dark';
  const rays = [
    auroraRay([70, 255, 150], [150, 70, 210]),
    auroraRay([60, 230, 190], [120, 90, 230]),
    auroraRay([120, 255, 120], [210, 70, 140]),
  ];
  // The curtains: where each hangs, how deep it folds, its own drift.
  const curtains = [0, 1, 2].map((i) => ({
    base: h * (0.42 + i * 0.13 + r() * 0.05),
    len: h * (0.34 - i * 0.06),
    fold: 0.6 + r() * 0.8,
    ph: r() * 10,
    sp: 0.6 + r() * 0.6,
    a: [1, 0.7, 0.45][i],
    tilt: (r() - 0.5) * 0.35,
    ray: rays[i],
  }));
  const step = Math.max(1.2, 2.2 / Math.max(0.25, px)) / Math.max(0.5, detail);
  const nRay = Math.ceil((w * 1.3) / step);
  const flick = Float32Array.from({ length: nRay }, () => r() * 6.28);
  const vary = Float32Array.from({ length: nRay }, () => 0.55 + r() * 0.45);
  // Stars, drawn once into their own layer.
  const sky = spriteCanvas(Math.max(1, Math.round(w * px)), Math.max(1, Math.round(h * px)));
  const sg = sky?.getContext('2d') as TCtx | null;
  if (sky && sg) {
    sg.scale(px, px);
    const grad = sg.createLinearGradient(0, 0, 0, h);
    grad.addColorStop(0, dark ? 'rgba(2,4,14,0.55)' : 'rgba(255,255,255,0)');
    grad.addColorStop(1, dark ? 'rgba(14,32,44,0.35)' : 'rgba(200,235,225,0.35)');
    sg.fillStyle = grad;
    sg.fillRect(0, 0, w, h);
    if (dark) {
      const n = Math.round((w * h) / 2600);
      for (let i = 0; i < n; i++) {
        const z = r();
        sg.globalAlpha = 0.25 + z * 0.75;
        sg.fillStyle = z > 0.93 ? '#ffe9d0' : '#e8f0ff';
        const s = (z > 0.97 ? 2.4 : 1.3) / Math.max(0.5, px);
        sg.fillRect(r() * w, r() * h * 0.85, s, s);
      }
      sg.globalAlpha = 1;
    }
  }
  return {
    frame: (ctx, t) => {
      const ts = t * speed;
      ctx.fillStyle = c.bg;
      ctx.fillRect(0, 0, w, h);
      if (sky) ctx.drawImage(sky, 0, 0, w, h);
      ctx.globalCompositeOperation = dark ? 'lighter' : 'source-over';
      for (const cu of curtains) {
        if (!cu.ray) continue;
        const ph = cu.ph;
        for (let i = 0; i < nRay; i++) {
          const s = i * step - w * 0.15;
          // The folds: the curtain's line bends back and forth along x, so rays bunch where it turns (brighter).
          const fx =
            s +
            Math.sin(s / (w * 0.11) + ts * 0.11 * cu.sp + ph) * w * 0.05 * cu.fold +
            Math.sin(s / (w * 0.035) - ts * 0.23 * cu.sp + ph * 2) * w * 0.012;
          if (fx < -4 || fx > w + 4) continue;
          // Where along its length the curtain is lit: ribbons with gaps between them, slowly shifting.
          const e = 0.5 + 0.5 * Math.sin(s / (w * 0.21) + ph * 1.3 + ts * 0.03) * 0.8 + 0.5 * 0.2 * Math.sin(s / (w * 0.08) + ph);
          const env = e * e * (3 - 2 * e);
          if (env < 0.04) continue;
          const y =
            cu.base +
            (s / w - 0.5) * h * cu.tilt +
            Math.sin(s / (w * 0.19) + ts * 0.07 * cu.sp + ph) * h * 0.07 +
            Math.sin(s / (w * 0.06) - ts * 0.15 * cu.sp + ph * 1.7) * h * 0.018;
          // Pulses running along the curtain and each ray's own flicker.
          const pulse =
            0.35 +
            0.45 * Math.max(0, Math.sin(s / (w * 0.07) - ts * 0.9 + ph)) +
            0.2 * Math.sin(s / (w * 0.23) + ts * 0.31 + ph);
          const fl = 0.75 + 0.25 * Math.sin(ts * (1.3 + vary[i]) + flick[i]);
          const a = cu.a * env * pulse * fl * vary[i] * (dark ? 0.6 : 0.34);
          if (a <= 0.01) continue;
          const len = cu.len * (0.6 + 0.4 * Math.sin(s / (w * 0.13) + ph * 3 + ts * 0.05)) * (0.8 + 0.2 * vary[i]) * (0.55 + 0.45 * env);
          ctx.globalAlpha = Math.min(1, a);
          ctx.drawImage(cu.ray, fx, y - len, step * 1.25, len);
        }
      }
      ctx.globalAlpha = 1;
      ctx.globalCompositeOperation = 'source-over';
    },
  };
};

const rain = (w: number, h: number, c: ISceneColors, speed: number, detail: number): IScene => {
  const r = rng(23);
  const n = Math.max(30, Math.round(Math.min(260, (w * h) / 7000) * detail));
  const xs = Float32Array.from({ length: n }, () => r() * w);
  const ys = Float32Array.from({ length: n }, () => r() * h);
  const vs = Float32Array.from({ length: n }, () => 500 + r() * 500);
  const beads = Array.from({ length: Math.round(n / 3) }, () => [r() * w, r() * h, 1 + r() * 2.5] as const);
  return {
    frame: (ctx, _t, dt) => {
      ctx.fillStyle = c.bg;
      ctx.fillRect(0, 0, w, h);
      ctx.strokeStyle = c.hues[0];
      ctx.lineWidth = 1;
      ctx.globalAlpha = 0.35;
      ctx.beginPath();
      for (let i = 0; i < n; i++) {
        ys[i] += vs[i] * speed * dt;
        xs[i] += 40 * speed * dt;
        if (ys[i] > h) {
          ys[i] = -20;
          xs[i] = r() * w;
        }
        if (xs[i] > w) xs[i] -= w;
        ctx.moveTo(xs[i], ys[i]);
        ctx.lineTo(xs[i] - 3, ys[i] - 18);
      }
      ctx.stroke();
      // The beads on the glass in one path.
      ctx.globalAlpha = 0.28;
      ctx.fillStyle = c.ink;
      ctx.beginPath();
      for (const [x, y, s] of beads) {
        ctx.moveTo(x + s, y);
        ctx.arc(x, y, s, 0, 6.283);
      }
      ctx.fill();
      ctx.globalAlpha = 1;
    },
  };
};

const synthwave = (w: number, h: number, c: ISceneColors, speed: number): IScene => {
  const horizon = h * 0.55;
  return {
    frame: (ctx, t) => {
      ctx.fillStyle = c.bg;
      ctx.fillRect(0, 0, w, h);
      // The sun: a disc in the first hue with slits.
      const sunR = Math.min(w, h) * 0.16;
      ctx.globalAlpha = 0.55;
      ctx.fillStyle = c.hues[0];
      ctx.beginPath();
      ctx.arc(w / 2, horizon - sunR * 0.35, sunR, Math.PI, 0);
      ctx.fill();
      ctx.fillStyle = c.bg;
      for (let i = 1; i < 6; i++)
        ctx.fillRect(w / 2 - sunR, horizon - sunR * 0.35 - i * sunR * 0.13, sunR * 2, i * 0.9);
      // The grid running to the horizon.
      ctx.globalAlpha = 0.45;
      ctx.strokeStyle = c.hues[1 % c.hues.length];
      ctx.lineWidth = 1;
      ctx.beginPath();
      const rows = 14;
      const shift = (t * 0.35 * speed) % 1;
      for (let i = 0; i < rows; i++) {
        const k = (i + shift) / rows;
        const y = horizon + (h - horizon) * k * k;
        ctx.moveTo(0, y);
        ctx.lineTo(w, y);
      }
      for (let i = -12; i <= 12; i++) {
        ctx.moveTo(w / 2 + i * (w / 60), horizon);
        ctx.lineTo(w / 2 + i * (w / 7), h);
      }
      ctx.stroke();
      ctx.globalAlpha = 1;
    },
  };
};

const twinkle = (w: number, h: number, c: ISceneColors, speed: number, detail: number): IScene => {
  const gap = detail < 0.6 ? 40 : 28;
  const cols = Math.ceil(w / gap);
  const rows = Math.ceil(h / gap);
  const r = rng(5);
  const ph = Float32Array.from({ length: cols * rows }, () => r() * 6.28);
  const rate = Float32Array.from({ length: cols * rows }, () => 0.4 + r() * 1.4);
  const step = new Uint8Array(cols * rows);
  return {
    frame: (ctx, t) => {
      ctx.fillStyle = c.bg;
      ctx.fillRect(0, 0, w, h);
      for (let i = 0; i < step.length; i++) {
        const v = Math.sin(t * rate[i] * speed + ph[i]);
        // 0 and 1 are the faint dots at rest; the bright top in the hues is STEPS.
        step[i] =
          v > 0.9 ? STEPS : Math.min(STEPS - 1, Math.floor((0.1 + 0.5 * Math.max(0, v) ** 3) * 2 * STEPS));
      }
      for (let s = 0; s <= STEPS; s++) {
        ctx.globalAlpha = s === STEPS ? 0.6 : Math.min(0.6, (s + 0.5) / (2 * STEPS));
        ctx.fillStyle = s === STEPS ? c.hues[0] : c.ink;
        ctx.beginPath();
        for (let y = 0; y < rows; y++)
          for (let x = 0; x < cols; x++)
            if (step[y * cols + x] === s) ctx.rect(x * gap + gap / 2, y * gap + gap / 2, 2, 2);
        ctx.fill();
      }
      ctx.globalAlpha = 1;
    },
  };
};

const paper = (w: number, h: number, c: ISceneColors): IScene => ({
  still: true,
  frame: (ctx) => {
    ctx.fillStyle = c.bg;
    ctx.fillRect(0, 0, w, h);
    const r = rng(3);
    // Fibres and a faint ruling: the grain of a sheet.
    ctx.strokeStyle = c.ink;
    ctx.lineWidth = 0.5;
    ctx.globalAlpha = 0.035;
    ctx.beginPath();
    for (let i = 0; i < (w * h) / 900; i++) {
      const x = r() * w;
      const y = r() * h;
      ctx.moveTo(x, y);
      ctx.lineTo(x + (r() - 0.5) * 14, y + (r() - 0.5) * 4);
    }
    ctx.stroke();
    ctx.globalAlpha = 0.05;
    ctx.strokeStyle = c.hues[0];
    ctx.beginPath();
    for (let y = 32; y < h; y += 32) {
      ctx.moveTo(0, y + 0.5);
      ctx.lineTo(w, y + 0.5);
    }
    ctx.stroke();
    ctx.globalAlpha = 1;
  },
});

const lava = (w: number, h: number, c: ISceneColors, speed: number): IScene => {
  const r = rng(17);
  const n = 7;
  const blobs = Array.from({ length: n }, (_, i) => ({
    x: r(),
    y: r(),
    rad: 0.18 + r() * 0.16,
    sx: 0.05 + r() * 0.08,
    sy: 0.04 + r() * 0.07,
    i,
  }));
  // A soft blob per hue, drawn once and moved: no gradient is made per frame.
  const R = 64;
  const sprites = c.hues.map((hue) => {
    const s = spriteCanvas(R * 2, R * 2);
    const sctx = s?.getContext('2d') as TCtx | null | undefined;
    if (!s || !sctx) return null;
    const g = sctx.createRadialGradient(R, R, 0, R, R, R);
    g.addColorStop(0, hue);
    g.addColorStop(1, 'transparent');
    sctx.fillStyle = g;
    sctx.fillRect(0, 0, R * 2, R * 2);
    return s;
  });
  return {
    frame: (ctx, t) => {
      ctx.fillStyle = c.bg;
      ctx.fillRect(0, 0, w, h);
      ctx.globalCompositeOperation = c.mode === 'dark' ? 'lighter' : 'source-over';
      ctx.globalAlpha = c.mode === 'dark' ? 0.5 : 0.35;
      for (const b of blobs) {
        const x = (0.5 + 0.42 * Math.sin(t * b.sx * speed * 3 + b.i)) * w;
        const y = (0.5 + 0.42 * Math.cos(t * b.sy * speed * 3 + b.i * 2)) * h;
        const rad = b.rad * Math.max(w, h);
        const sprite = sprites[b.i % sprites.length];
        if (sprite) ctx.drawImage(sprite, x - rad, y - rad, rad * 2, rad * 2);
      }
      ctx.globalAlpha = 1;
      ctx.globalCompositeOperation = 'source-over';
    },
  };
};

const snow = (w: number, h: number, c: ISceneColors, speed: number, detail: number): IScene => {
  const r = rng(29);
  const n = Math.max(30, Math.round(Math.min(300, (w * h) / 5000) * detail));
  // Four depths: a flake's size and alpha are its depth's, so a depth is one fill.
  const LAYERS = 4;
  const xs = Float32Array.from({ length: n }, () => r() * w);
  const ys = Float32Array.from({ length: n }, () => r() * h);
  const layer = Uint8Array.from({ length: n }, () => Math.floor(r() * LAYERS));
  const zOf = (l: number) => 0.3 + (0.7 * (l + 0.5)) / LAYERS;
  return {
    frame: (ctx, t, dt) => {
      ctx.fillStyle = c.bg;
      ctx.fillRect(0, 0, w, h);
      for (let i = 0; i < n; i++) {
        const z = zOf(layer[i]);
        ys[i] += z * 40 * speed * dt;
        xs[i] += Math.sin(t * 0.8 + i) * 12 * z * dt;
        if (ys[i] > h) {
          ys[i] = -4;
          xs[i] = r() * w;
        }
      }
      ctx.fillStyle = c.mode === 'dark' ? c.ink : c.hues[0];
      for (let l = 0; l < LAYERS; l++) {
        const z = zOf(l);
        const s = 1 + z * 2.2;
        ctx.globalAlpha = 0.25 + 0.5 * z;
        ctx.beginPath();
        for (let i = 0; i < n; i++) if (layer[i] === l) ctx.rect(xs[i], ys[i], s, s);
        ctx.fill();
      }
      ctx.globalAlpha = 1;
    },
  };
};

const topo = (w: number, h: number, c: ISceneColors, speed: number, detail: number): IScene => {
  const centres = [
    [0.2, 0.3],
    [0.75, 0.65],
    [0.55, 0.15],
  ] as const;
  const rings = Math.max(6, Math.round(13 * detail));
  const points = detail < 0.7 ? 40 : 64;
  // The angles' sines and cosines, once.
  const cos = Float32Array.from({ length: points + 1 }, (_, a) => Math.cos((a / points) * 6.283));
  const sin = Float32Array.from({ length: points + 1 }, (_, a) => Math.sin((a / points) * 6.283));
  return {
    frame: (ctx, t) => {
      ctx.fillStyle = c.bg;
      ctx.fillRect(0, 0, w, h);
      ctx.lineWidth = 1;
      ctx.globalAlpha = c.mode === 'dark' ? 0.22 : 0.18;
      centres.forEach(([cx, cy], k) => {
        ctx.strokeStyle = c.hues[k % c.hues.length];
        ctx.beginPath();
        for (let ring = 1; ring <= rings; ring++) {
          const base = ring * Math.max(w, h) * 0.035 * (13 / rings);
          for (let a = 0; a <= points; a++) {
            const ang = (a / points) * 6.283;
            const wob =
              1 +
              0.12 * Math.sin(ang * 3 + ring * 0.6 + t * 0.15 * speed + k) +
              0.06 * Math.sin(ang * 5 - t * 0.1 * speed);
            const x = cx * w + cos[a] * base * wob;
            const y = cy * h + sin[a] * base * wob * 0.8;
            if (a === 0) ctx.moveTo(x, y);
            else ctx.lineTo(x, y);
          }
        }
        ctx.stroke();
      });
      ctx.globalAlpha = 1;
    },
  };
};

type TMaker = (w: number, h: number, c: ISceneColors, speed: number, detail: number, px: number) => IScene;

const MAKERS: Record<TScene, TMaker> = {
  matrix,
  stars,
  aurora,
  rain,
  synthwave,
  twinkle,
  paper,
  lava,
  snow,
  topo,
};

/** A scene for a canvas of w × h CSS pixels (the context is scaled by the caller); `px` is the canvas' pixels per
 *  CSS pixel, `detail` 0.35–1. */
export const makeScene = (
  name: TScene,
  w: number,
  h: number,
  colors: ISceneColors,
  speed01: number,
  detail = 1,
  px = 1,
): IScene => MAKERS[name](w, h, colors, 0.25 + speed01 * 1.75, detail, px);
