// The snow of the live effects (snowplay, newyear, elves, weather): flakes at three depths — far specks, fluffy
// clumps and crystals in the middle, soft out-of-focus bokeh right in front — carried by gusts and turbulence, pushed
// and swirled by the pointer; and snow caps that sit on the chat's blocks like real snow on a ledge: thick and rounded,
// hugging the corners, lit from above with a blue shade underneath, grainy, glinting, with icicles. Each cap is drawn
// into a canvas of its own only when it changed, so a frame only places pictures.
/* global hx */
function Snow(opt) {
  const o = Object.assign(
    {
      density: 1, // flakes, relative
      caps: true, // snow on the blocks
      ground: true, // a drift along the bottom
      interact: true, // the pointer pushes and sweeps
      start: 0.65, // how much snow lies at the start, 0–1 of the cap's height
      capMax: 26, // px
      groundMax: 38,
      wind: () => 0, // px/s to the right
      fall: 1, // how fast flakes fall, relative
      tint: null, // rgb of the light on the snow, e.g. [255,214,170] for warm lamps
    },
    opt || {},
  );
  const COL = 3;
  const TAU = Math.PI * 2;
  const rnd = (a, b) => a + Math.random() * (b - a);
  const light = () => hx.mode === 'light';
  const mk = (w, h) => {
    const c = document.createElement('canvas');
    c.width = Math.max(1, Math.ceil(w));
    c.height = Math.max(1, Math.ceil(h));
    return c;
  };

  // ---- sprites, baked once per theme ----
  let SPR = null;
  const bake = () => {
    const tint = o.tint || [255, 255, 255];
    const shade = light() ? [150, 172, 205] : [205, 222, 245];
    const rgb = (c, a) => `rgba(${c[0]},${c[1]},${c[2]},${a})`;
    const mix = (a, b, k) => a.map((v, i) => Math.round(v + (b[i] - v) * k));
    const white = mix([255, 255, 255], tint, 0.25);
    // A fluffy clump: overlapping soft lobes, brighter on the top-left.
    const clump = (r, blur) => {
      const c = mk(r * 2 + 8, r * 2 + 8);
      const g = c.getContext('2d');
      g.translate(c.width / 2, c.height / 2);
      if (blur) g.filter = `blur(${blur}px)`;
      const n = 5 + Math.floor(r / 2);
      for (let i = 0; i < n; i++) {
        const a = rnd(0, TAU);
        const d = rnd(0, r * 0.45);
        const rr = rnd(r * 0.35, r * 0.62);
        const x = Math.cos(a) * d;
        const y = Math.sin(a) * d;
        const gr = g.createRadialGradient(x - rr * 0.3, y - rr * 0.3, 0, x, y, rr);
        gr.addColorStop(0, rgb(white, 0.95));
        gr.addColorStop(0.55, rgb(mix(white, shade, 0.35), 0.7));
        gr.addColorStop(1, rgb(shade, 0));
        g.fillStyle = gr;
        g.beginPath();
        g.arc(x, y, rr, 0, TAU);
        g.fill();
      }
      return c;
    };
    // A dendrite crystal: six arms with side branches, a little glow.
    const crystal = (r) => {
      const c = mk(r * 2 + 8, r * 2 + 8);
      const g = c.getContext('2d');
      g.translate(c.width / 2, c.height / 2);
      g.lineCap = 'round';
      g.shadowColor = rgb(white, 0.8);
      g.shadowBlur = 3;
      g.strokeStyle = rgb(white, 0.95);
      for (let k = 0; k < 6; k++) {
        g.save();
        g.rotate((k * TAU) / 6);
        g.lineWidth = Math.max(0.8, r / 9);
        g.beginPath();
        g.moveTo(0, 0);
        g.lineTo(0, -r);
        for (const f of [0.35, 0.58, 0.8]) {
          const l = r * (0.42 - f * 0.28);
          g.moveTo(0, -r * f);
          g.lineTo(l * 0.8, -r * f - l * 0.6);
          g.moveTo(0, -r * f);
          g.lineTo(-l * 0.8, -r * f - l * 0.6);
        }
        g.stroke();
        g.restore();
      }
      g.fillStyle = rgb(white, 0.9);
      g.beginPath();
      g.arc(0, 0, r * 0.12, 0, TAU);
      g.fill();
      return c;
    };
    // Bokeh: a big out-of-focus flake right before the eye — a soft disc with a faint brighter rim.
    const bokeh = (r) => {
      const c = mk(r * 2 + 4, r * 2 + 4);
      const g = c.getContext('2d');
      g.translate(c.width / 2, c.height / 2);
      const gr = g.createRadialGradient(0, 0, 0, 0, 0, r);
      gr.addColorStop(0, rgb(white, 0.16));
      gr.addColorStop(0.7, rgb(white, 0.13));
      gr.addColorStop(0.88, rgb(white, 0.17));
      gr.addColorStop(1, rgb(white, 0));
      g.fillStyle = gr;
      g.beginPath();
      g.arc(0, 0, r, 0, TAU);
      g.fill();
      return c;
    };
    // The grain of lying snow: a tile of tiny bright and blue specks.
    const grain = mk(96, 96);
    const gg = grain.getContext('2d');
    for (let i = 0; i < 520; i++) {
      gg.fillStyle = Math.random() < 0.6 ? rgb(white, rnd(0.25, 0.7)) : rgb(shade, rnd(0.15, 0.45));
      gg.fillRect(rnd(0, 96), rnd(0, 96), rnd(0.6, 1.6), rnd(0.6, 1.6));
    }
    SPR = {
      far: [clump(1.6, 0), clump(2.2, 0)],
      mid: [clump(3.5, 0), clump(4.5, 0), clump(6, 0), clump(7.5, 0.4)],
      crystal: [crystal(8), crystal(11), crystal(14)],
      near: [bokeh(22), bokeh(30), bokeh(40)],
      grain,
      white,
      shade,
    };
    for (const c of caps.values()) c.dirty = true;
  };

  // ---- flakes ----
  const flakes = [];
  const want = () =>
    Math.round((70 + 230 * hx.power) * o.density * Math.min(1.7, (hx.w * hx.h) / (1400 * 900)));
  const spawn = (top) => {
    const r = Math.random();
    let kind = 'mid';
    if (r < 0.42) kind = 'far';
    else if (r > 0.985) kind = 'near';
    else if (r > 0.9) kind = 'crystal';
    const z = { far: rnd(0.25, 0.45), mid: rnd(0.5, 0.85), crystal: rnd(0.7, 0.95), near: rnd(1.3, 1.8) }[
      kind
    ];
    const list = SPR[kind];
    const img = list[(Math.random() * list.length) | 0];
    return {
      kind,
      img,
      z,
      x: rnd(-40, hx.w + 40),
      y: top ? rnd(-60, -10) : rnd(0, hx.h),
      vx: 0,
      vy: 0,
      ph: rnd(0, TAU),
      rot: rnd(0, TAU),
      spin: rnd(-0.6, 0.6),
      tumble: rnd(0, TAU),
      a: kind === 'far' ? rnd(0.35, 0.65) : kind === 'near' ? rnd(0.5, 0.9) : rnd(0.75, 1),
      melt: 0,
    };
  };

  // ---- caps on blocks ----
  const caps = new Map(); // id -> cap
  let ground = null;
  const seeded = (id) => {
    let s = (id * 2654435761) >>> 0;
    return () => (s = (s * 1664525 + 1013904223) >>> 0) / 4294967296;
  };
  const newCols = (n, max, start, rand) => {
    const a = new Float32Array(n);
    for (let i = 0; i < n; i++) a[i] = max * start * (0.75 + 0.25 * Math.sin(i * 0.21 + rand() * 6) * rand());
    return a;
  };
  const capFor = (b) => {
    const n = Math.max(2, Math.round(b.w / COL));
    let c = caps.get(b.id);
    if (!c) {
      const rand = seeded(b.id);
      c = {
        id: b.id,
        cols: newCols(n, o.capMax, o.start, rand),
        icicles: [],
        rand,
        canvas: null,
        dirty: true,
        last: 0,
      };
      // Icicles hang where the cap is thick: near the ends and here and there along the edge.
      const k = Math.max(2, Math.round(b.w / 140));
      for (let i = 0; i < k; i++)
        c.icicles.push({
          u: i === 0 ? 0.03 : i === k - 1 ? 0.97 : rand(),
          len: 7 + rand() * 13,
          w: 1.2 + rand() * 1.6,
        });
      caps.set(b.id, c);
    } else if (c.cols.length !== n) {
      const next = new Float32Array(n);
      for (let i = 0; i < n; i++)
        next[i] = c.cols[Math.min(c.cols.length - 1, Math.round((i * c.cols.length) / n))];
      c.cols = next;
      c.dirty = true;
    }
    c.b = b;
    c.r = Math.min(18, b.h / 3, b.w / 6);
    return c;
  };

  const relax = (cols, cap, ends) => {
    for (let i = 1; i < cols.length; i++) {
      const d = cols[i] - cols[i - 1];
      if (Math.abs(d) > 1.2) {
        const m = d * 0.22;
        cols[i] -= m;
        cols[i - 1] += m;
      }
    }
    if (ends) {
      // The cap rounds off at the corners instead of standing as a wall.
      const n = cols.length;
      for (let i = 0; i < Math.min(6, n); i++) {
        const k = 0.55 + (i / 6) * 0.45;
        cols[i] = Math.min(cols[i], cap * k);
        cols[n - 1 - i] = Math.min(cols[n - 1 - i], cap * k);
      }
    }
  };

  const add = (cols, i, amount, max) => {
    for (let k = -3; k <= 3; k++) {
      const j = i + k;
      if (j >= 0 && j < cols.length) cols[j] = Math.min(max, cols[j] + amount * (1 - Math.abs(k) / 4));
    }
  };

  // The shape of a cap in its own canvas: the top follows the heights, the bottom the block's rounded top edge, and
  // over the corners the snow runs down a little and thins out.
  const PAD = 26;
  const drawCap = (c) => {
    const { b, r, cols } = c;
    const W = b.w + PAD * 2;
    const H = o.capMax + r + PAD * 2;
    if (!c.canvas || c.canvas.width !== Math.ceil(W) || c.canvas.height !== Math.ceil(H)) c.canvas = mk(W, H);
    const g = c.canvas.getContext('2d');
    g.clearRect(0, 0, c.canvas.width, c.canvas.height);
    const x0 = PAD;
    const base = PAD + o.capMax; // the block's top edge in the cap canvas
    const n = cols.length;
    const hAt = (i) => cols[Math.max(0, Math.min(n - 1, i))];
    const top = [];
    for (let i = 0; i < n; i++) top.push([x0 + (i + 0.5) * (b.w / n), base - 1.5 - hAt(i)]);
    const hl = hAt(0);
    const hr = hAt(n - 1);
    const path = () => {
      g.beginPath();
      // Left: down over the corner, thinning.
      g.moveTo(x0 + r * 0.9, base + 1);
      g.quadraticCurveTo(x0 + 1, base + 1, x0, base + r * 0.75);
      g.quadraticCurveTo(x0 - 3 - hl * 0.25, base + r * 0.4, x0 - 1 - hl * 0.3, base - hl * 0.4);
      g.quadraticCurveTo(x0 - hl * 0.15, top[0][1], top[0][0], top[0][1]);
      for (let i = 1; i < n - 1; i++) {
        const mx = (top[i][0] + top[i + 1][0]) / 2;
        const my = (top[i][1] + top[i + 1][1]) / 2;
        g.quadraticCurveTo(top[i][0], top[i][1], mx, my);
      }
      const xr = x0 + b.w;
      g.quadraticCurveTo(xr + hr * 0.15, top[n - 1][1], xr + 1 + hr * 0.3, base - hr * 0.4);
      g.quadraticCurveTo(xr + 3 + hr * 0.25, base + r * 0.4, xr, base + r * 0.75);
      g.quadraticCurveTo(xr - 1, base + 1, xr - r * 0.9, base + 1);
      g.closePath();
    };
    const S = SPR;
    // A soft shadow the snow casts on the block.
    g.save();
    g.shadowColor = light() ? 'rgba(40,60,100,0.28)' : 'rgba(0,0,0,0.45)';
    g.shadowBlur = 7;
    g.shadowOffsetY = 3;
    path();
    g.fillStyle = `rgb(${S.shade[0]},${S.shade[1]},${S.shade[2]})`;
    g.fill();
    g.restore();
    // The body: bright on top, cool blue underneath.
    path();
    const maxH = Math.max(4, ...cols);
    const gr = g.createLinearGradient(0, base - maxH, 0, base + r * 0.8);
    gr.addColorStop(0, `rgb(${S.white[0]},${S.white[1]},${S.white[2]})`);
    gr.addColorStop(0.45, light() ? 'rgb(240,245,252)' : 'rgb(236,243,252)');
    gr.addColorStop(0.8, light() ? 'rgb(196,212,234)' : 'rgb(196,214,238)');
    gr.addColorStop(1, light() ? 'rgb(160,182,214)' : 'rgb(150,175,212)');
    g.fillStyle = gr;
    g.fill();
    // Grain and glints, only on the snow.
    g.save();
    g.clip();
    g.globalAlpha = 0.55;
    g.fillStyle = g.createPattern(S.grain, 'repeat');
    g.fillRect(0, 0, W, H);
    g.globalAlpha = 1;
    // The underside in shade: a band along the bottom edge.
    const sh = g.createLinearGradient(0, base - 4, 0, base + 2);
    sh.addColorStop(0, 'rgba(120,150,200,0)');
    sh.addColorStop(1, light() ? 'rgba(110,140,190,0.55)' : 'rgba(95,130,185,0.6)');
    g.fillStyle = sh;
    g.fillRect(0, base - 4, W, 8);
    g.restore();
    // Light from above: a crisp rim along the top.
    g.save();
    g.beginPath();
    g.moveTo(top[0][0], top[0][1] + 0.6);
    for (let i = 1; i < n - 1; i++) {
      const mx = (top[i][0] + top[i + 1][0]) / 2;
      const my = (top[i][1] + top[i + 1][1]) / 2 + 0.6;
      g.quadraticCurveTo(top[i][0], top[i][1] + 0.6, mx, my);
    }
    g.strokeStyle = 'rgba(255,255,255,0.95)';
    g.lineWidth = 1.4;
    g.stroke();
    g.restore();
    // Icicles: tapered, translucent, with a bright edge.
    for (const ic of c.icicles) {
      const i = Math.round(ic.u * (n - 1));
      const thick = hAt(i);
      if (thick < o.capMax * 0.3) continue;
      const len = ic.len * Math.min(1, thick / o.capMax + 0.2);
      const x = x0 + ic.u * b.w + (ic.u < 0.1 ? -1 : ic.u > 0.9 ? 1 : 0) * 2;
      const y = base + (ic.u < 0.1 || ic.u > 0.9 ? r * 0.5 : 0.5);
      const ig = g.createLinearGradient(x - ic.w, 0, x + ic.w, 0);
      ig.addColorStop(0, 'rgba(200,225,255,0.25)');
      ig.addColorStop(0.35, 'rgba(255,255,255,0.8)');
      ig.addColorStop(0.6, 'rgba(225,238,255,0.45)');
      ig.addColorStop(1, 'rgba(170,200,240,0.2)');
      g.fillStyle = ig;
      g.beginPath();
      g.moveTo(x - ic.w, y - 1);
      g.quadraticCurveTo(x - ic.w * 0.4, y + len * 0.6, x, y + len);
      g.quadraticCurveTo(x + ic.w * 0.4, y + len * 0.6, x + ic.w, y - 1);
      g.closePath();
      g.fill();
    }
    c.dirty = false;
  };

  // ---- the ground ----
  const groundCanvas = { canvas: null, dirty: true, last: 0 };
  const drawGround = () => {
    const W = hx.w;
    const H = o.groundMax + 20;
    if (!groundCanvas.canvas || groundCanvas.canvas.width !== Math.ceil(W)) groundCanvas.canvas = mk(W, H);
    const g = groundCanvas.canvas.getContext('2d');
    g.clearRect(0, 0, W, H);
    const n = ground.length;
    g.beginPath();
    g.moveTo(0, H);
    for (let i = 0; i < n; i++) g.lineTo(i * COL, H - ground[i]);
    g.lineTo(W, H);
    g.closePath();
    const S = SPR;
    const gr = g.createLinearGradient(0, H - o.groundMax, 0, H);
    gr.addColorStop(0, `rgb(${S.white[0]},${S.white[1]},${S.white[2]})`);
    gr.addColorStop(0.6, light() ? 'rgb(222,232,246)' : 'rgb(214,226,244)');
    gr.addColorStop(1, light() ? 'rgb(176,196,224)' : 'rgb(150,174,210)');
    g.fillStyle = gr;
    g.fill();
    g.save();
    g.clip();
    g.globalAlpha = 0.5;
    g.fillStyle = g.createPattern(S.grain, 'repeat');
    g.fillRect(0, 0, W, H);
    g.restore();
    g.strokeStyle = 'rgba(255,255,255,0.9)';
    g.lineWidth = 1.2;
    g.beginPath();
    for (let i = 0; i < n; i++) g[i ? 'lineTo' : 'moveTo'](i * COL, H - ground[i] + 0.6);
    g.stroke();
    groundCanvas.dirty = false;
  };

  // ---- glints on the snow ----
  const glints = [];
  const glint = (ctx, x, y, s, a) => {
    ctx.globalAlpha = a;
    ctx.fillStyle = '#fff';
    ctx.beginPath();
    ctx.moveTo(x, y - s);
    ctx.lineTo(x + s * 0.18, y);
    ctx.lineTo(x, y + s);
    ctx.lineTo(x - s * 0.18, y);
    ctx.closePath();
    ctx.moveTo(x - s, y);
    ctx.lineTo(x, y + s * 0.18);
    ctx.lineTo(x + s, y);
    ctx.lineTo(x, y - s * 0.18);
    ctx.closePath();
    ctx.fill();
  };

  // ---- thrown snow ----
  const puffs = [];
  const throwSnow = (x, y, vx, vy, n, size) => {
    for (let i = 0; i < n; i++)
      puffs.push({
        x,
        y,
        vx: vx + rnd(-30, 30),
        vy: vy + rnd(-30, 10),
        r: rnd(0.8, size),
        life: rnd(0.8, 1.6),
      });
  };

  const settle = (x, y, amount) => {
    if (o.caps)
      for (const c of caps.values()) {
        const b = c.b;
        if (x < b.x || x > b.x + b.w) continue;
        const i = Math.floor(((x - b.x) / b.w) * c.cols.length);
        const surface = b.y - 1.5 - c.cols[i];
        if (y >= surface && y <= b.y + 4) {
          add(c.cols, i, amount, o.capMax);
          c.dirty = true;
          return true;
        }
      }
    if (o.ground && ground) {
      const gi = Math.floor(x / COL);
      if (gi >= 0 && gi < ground.length && y >= hx.h - ground[gi]) {
        add(ground, gi, amount, o.groundMax);
        groundCanvas.dirty = true;
        return true;
      }
    }
    return false;
  };

  const sweep = () => {
    const p = hx.pointer;
    const sp = Math.hypot(p.vx, p.vy);
    if (!o.interact || !p.inside || sp < 220) return;
    const R = 28;
    const dig = (cols, x0, w, base, mark) => {
      const n = cols.length;
      const from = Math.max(0, Math.floor(((p.x - R - x0) / w) * n));
      const to = Math.min(n - 1, Math.ceil(((p.x + R - x0) / w) * n));
      let took = 0;
      for (let i = from; i <= to; i++) {
        const h = cols[i];
        if (h < 0.5 || p.y < base - h - R || p.y > base + 12) continue;
        const t = h * Math.min(0.9, sp / 1600);
        cols[i] -= t;
        took += t;
        if (Math.random() < t / 3)
          throwSnow(x0 + ((i + 0.5) / n) * w, base - h, p.vx * 0.22, p.vy * 0.18 - 60, 1, 2.6);
      }
      if (took > 0) mark();
    };
    for (const c of caps.values()) dig(c.cols, c.b.x, c.b.w, c.b.y, () => (c.dirty = true));
    if (ground) dig(ground, 0, ground.length * COL, hx.h, () => (groundCanvas.dirty = true));
  };

  const api = {
    /** The caps by block id (for an effect that wants to shake one). */
    caps,
    get ground() {
      return ground;
    },
    COL,
    throwSnow,
    settle,
    markGround: () => (groundCanvas.dirty = true),
    /** Shakes a block's snow off. */
    shake(id, power = 1) {
      const c = caps.get(id);
      if (!c) return;
      const n = c.cols.length;
      for (let i = 0; i < n; i += 2) {
        const h = c.cols[i];
        if (h > 1)
          throwSnow(c.b.x + (i / n) * c.b.w, c.b.y - h, rnd(-40, 40) * power, -rnd(30, 120) * power, 1, 3);
        c.cols[i] = 0;
        if (i + 1 < n) c.cols[i + 1] = 0;
      }
      c.dirty = true;
    },
    sync() {
      if (!SPR) return;
      if (o.ground && (!ground || ground.length !== Math.ceil(hx.w / COL) + 1)) {
        const next = new Float32Array(Math.ceil(hx.w / COL) + 1);
        const rand = seeded(7);
        for (let i = 0; i < next.length; i++)
          next[i] = ground && i < ground.length ? ground[i] : o.groundMax * o.start * (0.6 + 0.4 * rand());
        ground = next;
        groundCanvas.dirty = true;
      }
      if (!o.caps) return;
      const seen = new Set();
      for (const b of hx.blocks) {
        if (b.w < 60) continue;
        seen.add(b.id);
        capFor(b);
      }
      for (const [id, c] of caps) {
        if (seen.has(id)) continue;
        // A block that went away drops its snow if it was on screen.
        if (c.b.y > 0 && c.b.y < hx.h) api.shake(id, 0.5);
        caps.delete(id);
      }
    },
    /** Moves the flakes; t, dt from hx frame. */
    update(t, dt) {
      if (!SPR) return;
      const p = hx.pointer;
      while (flakes.length < want()) flakes.push(spawn(true));
      if (flakes.length > want() + 20) flakes.length = want();
      const gust = o.wind() + Math.sin(t * 0.13) * 14 + Math.sin(t * 0.37 + 1.3) * 8;
      for (let i = flakes.length - 1; i >= 0; i--) {
        const f = flakes[i];
        const z = f.z;
        // Turbulence: the air curls a little everywhere, more for the light flakes.
        const tx =
          Math.sin(f.y * 0.012 + t * 0.9 + f.ph) * 18 + Math.sin(f.y * 0.031 - t * 1.7 + f.ph * 2) * 7;
        let ax = (gust + tx) * z - f.vx;
        let ay = (24 + 34 * z) * o.fall - f.vy;
        if (o.interact && p.inside && f.kind !== 'far') {
          const dx = f.x - p.x;
          const dy = f.y - p.y;
          const d2 = dx * dx + dy * dy;
          const R = 120;
          if (d2 < R * R && d2 > 1) {
            const d = Math.sqrt(d2);
            const k = (1 - d / R) ** 2;
            // Pushed away, swirled around, carried along by the hand's motion.
            ax += (dx / d) * k * 1400 + (-dy / d) * k * 500 + p.vx * k * 2.2;
            ay += (dy / d) * k * 1400 + (dx / d) * k * 500 + p.vy * k * 2.2;
          }
        }
        f.vx += ax * Math.min(1, dt * 2.2);
        f.vy += ay * Math.min(1, dt * 2.2);
        f.x += f.vx * dt;
        f.y += f.vy * dt;
        f.rot += f.spin * dt;
        f.tumble += dt * (0.8 + z);
        if (f.x < -60) f.x += hx.w + 120;
        else if (f.x > hx.w + 60) f.x -= hx.w + 120;
        if (f.kind === 'mid' || f.kind === 'crystal') {
          if (f.y > 0 && settle(f.x, f.y, f.kind === 'crystal' ? 0.9 : 0.35 + f.img.width / 40)) {
            flakes[i] = spawn(true);
            continue;
          }
        }
        if (f.y > hx.h + 40) flakes[i] = spawn(true);
      }
      sweep();
      for (const c of caps.values()) relax(c.cols, o.capMax, true);
      if (ground) relax(ground, o.groundMax, false);
      for (let i = puffs.length - 1; i >= 0; i--) {
        const q = puffs[i];
        q.life -= dt;
        q.vy += 380 * dt;
        q.vx *= 1 - dt * 0.9;
        q.x += q.vx * dt;
        q.y += q.vy * dt;
        if ((q.vy > 0 && settle(q.x, q.y, q.r * 0.35)) || q.life <= 0 || q.y > hx.h + 10) puffs.splice(i, 1);
      }
      if (Math.random() < dt * 6 && (caps.size || ground)) {
        const list = [...caps.values()];
        const c = list.length && Math.random() < 0.75 ? list[(Math.random() * list.length) | 0] : null;
        if (c) {
          const i = (Math.random() * c.cols.length) | 0;
          if (c.cols[i] > 3)
            glints.push({
              x: c.b.x + (i / c.cols.length) * c.b.w,
              y: c.b.y - c.cols[i] + rnd(0, 3),
              t: 0,
              s: rnd(1.8, 3.6),
            });
        } else if (ground) {
          const i = (Math.random() * ground.length) | 0;
          if (ground[i] > 3)
            glints.push({ x: i * COL, y: hx.h - ground[i] + rnd(0, 4), t: 0, s: rnd(1.8, 3.6) });
        }
      }
    },
    /** Draws the snow: which = 'flakes' | 'lying' | 'all'. */
    draw(ctx, t, which = 'all') {
      if (!SPR) return;
      const flakesToo = which !== 'lying';
      const lyingToo = which !== 'flakes';
      if (flakesToo) {
        for (const f of flakes) {
          if (f.kind === 'near') continue;
          const s = f.img;
          ctx.globalAlpha = f.a;
          if (f.kind === 'crystal') {
            ctx.save();
            ctx.translate(f.x, f.y);
            ctx.rotate(f.rot);
            ctx.scale(Math.max(0.25, Math.abs(Math.cos(f.tumble))), 1);
            ctx.drawImage(s, -s.width / 2, -s.height / 2);
            ctx.restore();
          } else ctx.drawImage(s, f.x - s.width / 2, f.y - s.height / 2);
        }
      }
      if (lyingToo) {
        ctx.globalAlpha = 1;
        const now = t;
        for (const c of caps.values()) {
          if (c.b.y < -40 || c.b.y > hx.h + 40) continue;
          if (c.dirty && now - c.last > 0.08) {
            drawCap(c);
            c.last = now;
          }
          if (c.canvas) ctx.drawImage(c.canvas, c.b.x - PAD, c.b.y - PAD - o.capMax);
        }
        if (ground) {
          if (groundCanvas.dirty && now - groundCanvas.last > 0.08) {
            drawGround();
            groundCanvas.last = now;
          }
          if (groundCanvas.canvas) ctx.drawImage(groundCanvas.canvas, 0, hx.h - groundCanvas.canvas.height);
        }
        for (let i = glints.length - 1; i >= 0; i--) {
          const g = glints[i];
          g.t += 1 / 60;
          const a = Math.sin(Math.min(1, g.t / 0.7) * Math.PI);
          if (g.t > 0.7) {
            glints.splice(i, 1);
            continue;
          }
          glint(ctx, g.x, g.y, g.s * (0.6 + a * 0.6), a * 0.95);
        }
        for (const q of puffs) {
          ctx.globalAlpha = Math.min(1, q.life);
          ctx.fillStyle = `rgb(${SPR.white[0]},${SPR.white[1]},${SPR.white[2]})`;
          ctx.beginPath();
          ctx.arc(q.x, q.y, q.r, 0, TAU);
          ctx.fill();
        }
      }
      if (flakesToo) {
        // The out-of-focus flakes come last: they are nearest.
        for (const f of flakes) {
          if (f.kind !== 'near') continue;
          ctx.globalAlpha = f.a * (0.6 + 0.4 * hx.power);
          ctx.drawImage(f.img, f.x - f.img.width / 2, f.y - f.img.height / 2);
        }
      }
      ctx.globalAlpha = 1;
    },
  };
  hx.on('theme', bake);
  // Built at once when the size is known, else on the first start.
  if (hx.w > 0) {
    bake();
    for (let i = 0; i < want(); i++) flakes.push(spawn(false));
    api.sync();
  } else
    hx.on('start', () => {
      bake();
      for (let i = 0; i < want(); i++) flakes.push(spawn(false));
      api.sync();
    });
  hx.on('blocks', () => api.sync());
  hx.on('resize', () => api.sync());
  return api;
}
