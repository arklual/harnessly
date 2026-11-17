// «Погода»: the sky over a lake valley as it is where the person is, now. The sun's height and bearing come from the
// time and the place (the page tells the place and the weather: hx.weather; without them the clock and the time zone),
// so the sky goes through dawn, day, sunset and night with stars and the moon; the clouds, rain, snow, fog, wind and
// thunderstorms follow the weather. Under the chat (layer back) the sky and the land, relit by the hour; over it (layer
// front) drops run down the glass in the rain and snow settles on the blocks in a snowfall. A double click on an empty
// place shows the variations one by one (dawn, day, clouds, rain, snow, sunset, night, fog, storm) and back to live.
/* global hx, Snow, ART_WEATHER */
const TAU = Math.PI * 2;
const D2R = Math.PI / 180;
const clamp = (v, a, b) => Math.max(a, Math.min(b, v));
const lerp = (a, b, k) => a + (b - a) * k;
const mixc = (a, b, k) => [lerp(a[0], b[0], k), lerp(a[1], b[1], k), lerp(a[2], b[2], k)];
const css = (c, a = 1) => `rgba(${c[0] | 0},${c[1] | 0},${c[2] | 0},${a})`;

// ---- the sun ----
const sunAt = (ms, lat, lon) => {
  const d = ms / 86400000 - 10957.5; // days since J2000
  const g = (357.529 + 0.98560028 * d) * D2R;
  const q = 280.459 + 0.98564736 * d;
  const L = (q + 1.915 * Math.sin(g) + 0.02 * Math.sin(2 * g)) * D2R;
  const e = (23.439 - 0.00000036 * d) * D2R;
  const ra = Math.atan2(Math.cos(e) * Math.sin(L), Math.cos(L));
  const dec = Math.asin(Math.sin(e) * Math.sin(L));
  const gmst = (18.697374558 + 24.06570982441908 * d) % 24;
  const H = ((gmst * 15 + lon) * D2R - ra) % TAU;
  const la = lat * D2R;
  const alt = Math.asin(Math.sin(la) * Math.sin(dec) + Math.cos(la) * Math.cos(dec) * Math.cos(H));
  const az = Math.atan2(-Math.sin(H), Math.tan(dec) * Math.cos(la) - Math.sin(la) * Math.cos(H));
  return { alt: alt / D2R, az: (az / D2R + 360) % 360 };
};

// ---- the variations a double click shows ----
const DEMO = [
  null,
  { name: 'рассвет', alt: 1, az: 120, code: 1, cloud: 30 },
  { name: 'ясный день', alt: 35, az: 170, code: 0, cloud: 5 },
  { name: 'облачно', alt: 28, az: 200, code: 3, cloud: 70, wind: 6 },
  { name: 'дождь', alt: 20, az: 210, code: 63, cloud: 95, rain: 2.5, wind: 5 },
  { name: 'снегопад', alt: 10, az: 200, code: 73, cloud: 90, snowfall: 1, wind: 3, temp: -4 },
  { name: 'закат', alt: -1, az: 245, code: 1, cloud: 35 },
  { name: 'ясная ночь', alt: -30, az: 0, code: 0, cloud: 0 },
  { name: 'туман', alt: 8, az: 150, code: 45, cloud: 100 },
  { name: 'гроза', alt: 15, az: 220, code: 95, cloud: 100, rain: 6, wind: 11 },
];
let demo = 0;
let demoLabel = 0;

const state = () => {
  const w = hx.weather || {};
  const now = Date.now();
  const lat = typeof w.lat === 'number' ? w.lat : 55.7;
  const lon = typeof w.lon === 'number' ? w.lon : -new Date().getTimezoneOffset() / 4;
  const sun = sunAt(now, lat, lon);
  const s = {
    alt: sun.alt,
    az: sun.az,
    code: w.code || 0,
    cloud: typeof w.cloud === 'number' ? w.cloud : 15,
    wind: typeof w.wind === 'number' ? w.wind : 3,
    windDir: typeof w.windDir === 'number' ? w.windDir : 270,
    rain: w.rain || 0,
    snowfall: w.snowfall || 0,
    temp:
      typeof w.temp === 'number' ? w.temp : new Date().getMonth() > 9 || new Date().getMonth() < 3 ? -3 : 15,
  };
  const d = DEMO[demo];
  if (d) Object.assign(s, { rain: 0, snowfall: 0, wind: 3, windDir: 270 }, d);
  const c = s.code;
  s.raining = s.rain > 0.05 || (c >= 51 && c <= 67) || (c >= 80 && c <= 82) || c >= 95;
  s.snowing = s.snowfall > 0.02 || (c >= 71 && c <= 77) || c === 85 || c === 86;
  if (s.snowing) s.raining = false;
  s.fog = c === 45 || c === 48;
  s.storm = c >= 95;
  s.winter = s.temp <= 2 || s.snowing;
  // The wind: m/s to px/s along x; it blows FROM windDir.
  s.vx = -Math.sin(s.windDir * D2R) * s.wind * 9;
  return s;
};

// ---- the sky's colours by the sun's height ----
const SKY = [
  // alt, zenith, horizon, light on the land, sun colour
  [-18, [3, 6, 18], [8, 14, 32], [30, 40, 70], [255, 120, 60]],
  [-10, [8, 14, 40], [28, 34, 72], [50, 58, 95], [255, 120, 60]],
  [-4, [22, 36, 84], [120, 82, 120], [110, 95, 130], [255, 110, 60]],
  [0, [42, 66, 128], [242, 138, 84], [200, 140, 120], [255, 120, 50]],
  [5, [56, 100, 170], [250, 190, 130], [240, 200, 170], [255, 190, 110]],
  [15, [48, 112, 200], [168, 206, 236], [250, 244, 236], [255, 240, 210]],
  [45, [30, 96, 205], [150, 196, 238], [255, 255, 255], [255, 252, 240]],
];
const skyAt = (alt) => {
  if (alt <= SKY[0][0]) return SKY[0].slice(1);
  for (let i = 1; i < SKY.length; i++)
    if (alt <= SKY[i][0]) {
      const k = (alt - SKY[i - 1][0]) / (SKY[i][0] - SKY[i - 1][0]);
      return [1, 2, 3, 4].map((j) => mixc(SKY[i - 1][j], SKY[i][j], k));
    }
  return SKY[SKY.length - 1].slice(1);
};

if (hx.layer === 'back') {
  const load = (src) => {
    const i = new Image();
    i.src = src;
    return i;
  };
  const LAND = { winter: load(ART_WEATHER.winter.src), summer: load(ART_WEATHER.summer.src) };
  const snow = Snow({
    density: 0.9,
    caps: false,
    ground: false,
    interact: true,
    wind: () => cur.vx,
    fall: 1,
  });
  let cur = state();
  let stars = null;
  let clouds = [];
  let cloudSprites = null;
  let cloudKey = '';
  let land = null;
  let landKey = '';
  let flash = 0;
  let bolt = null;
  const drops = [];
  let fogPh = 0;

  const bakeStars = () => {
    const c = document.createElement('canvas');
    c.width = Math.max(1, hx.w);
    c.height = Math.max(1, hx.h);
    const g = c.getContext('2d');
    for (let i = 0; i < (hx.w * hx.h) / 1800; i++) {
      const r = Math.random() < 0.92 ? Math.random() * 0.8 + 0.3 : Math.random() * 1.2 + 0.9;
      g.globalAlpha = 0.3 + Math.random() * 0.7;
      g.fillStyle = Math.random() < 0.15 ? '#cfe0ff' : Math.random() < 0.1 ? '#ffe7c8' : '#fff';
      g.beginPath();
      g.arc(Math.random() * hx.w, Math.random() * hx.h * 0.75, r, 0, TAU);
      g.fill();
    }
    // A faint band of the Milky Way.
    g.globalAlpha = 0.07;
    for (let i = 0; i < 400; i++) {
      const u = Math.random();
      const x = u * hx.w;
      const y = hx.h * (0.1 + u * 0.35) + (Math.random() - 0.5) * hx.h * 0.12;
      const gr = g.createRadialGradient(x, y, 0, x, y, 30);
      gr.addColorStop(0, 'rgba(200,210,255,0.6)');
      gr.addColorStop(1, 'rgba(200,210,255,0)');
      g.fillStyle = gr;
      g.fillRect(x - 30, y - 30, 60, 60);
    }
    stars = c;
  };

  // Cumulus clouds: soft lobes lit from the sun's side, shaded below, tinted by the hour.
  const bakeClouds = (lit, shade) => {
    const list = [];
    for (let k = 0; k < 9; k++) {
      const W = 260 + Math.random() * 300;
      const H = W * (0.32 + Math.random() * 0.14);
      const c = document.createElement('canvas');
      c.width = Math.ceil(W);
      c.height = Math.ceil(H);
      const g = c.getContext('2d');
      // Many small lobes, softly blurred, in two passes: a body and a lit crown — a cloud, not a bunch of balls.
      g.filter = 'blur(5px)';
      const n = 70 + ((Math.random() * 40) | 0);
      for (let pass = 0; pass < 2; pass++)
        for (let i = 0; i < (pass ? n * 0.5 : n); i++) {
          const u = 0.08 + Math.random() * 0.84;
          const top = Math.pow(Math.sin(u * Math.PI), 0.8);
          const x = u * W + (Math.random() - 0.5) * 10;
          const y = H * (0.8 - top * (0.15 + Math.random() * 0.45)) - (pass ? H * 0.06 : 0);
          const r = H * (0.06 + Math.random() * 0.12) * (0.5 + top * 0.7);
          const gr = g.createRadialGradient(x - r * 0.3, y - r * 0.45, r * 0.05, x, y, r);
          gr.addColorStop(0, css(lit, pass ? 0.55 : 0.75));
          gr.addColorStop(0.55, css(mixc(lit, shade, 0.5), pass ? 0.25 : 0.55));
          gr.addColorStop(1, css(shade, 0));
          g.fillStyle = gr;
          g.beginPath();
          g.arc(x, y, r, 0, TAU);
          g.fill();
        }
      g.filter = 'none';
      // A flat, darker base.
      g.globalCompositeOperation = 'source-atop';
      const base = g.createLinearGradient(0, H * 0.55, 0, H);
      base.addColorStop(0, css(shade, 0));
      base.addColorStop(1, css(shade, 0.55));
      g.fillStyle = base;
      g.fillRect(0, 0, W, H);
      list.push(c);
    }
    return list;
  };

  const bakeLand = (img, tint, dark) => {
    const w = hx.w;
    const h = Math.round((img.naturalHeight * w) / img.naturalWidth);
    const c = document.createElement('canvas');
    c.width = Math.max(1, w);
    c.height = Math.max(1, h);
    const g = c.getContext('2d');
    g.drawImage(img, 0, 0, w, h);
    g.globalCompositeOperation = 'multiply';
    g.fillStyle = css(tint);
    g.fillRect(0, 0, w, h);
    g.globalCompositeOperation = 'destination-in';
    g.drawImage(img, 0, 0, w, h);
    g.globalCompositeOperation = 'source-over';
    // Lit windows of the cabin after dusk.
    if (dark > 0.3 && img === LAND.winter) {
      for (const [u, v] of [
        [0.848, 0.62],
        [0.905, 0.62],
      ]) {
        const x = u * w;
        const y = v * h;
        const gr = g.createRadialGradient(x, y, 0, x, y, w * 0.02);
        gr.addColorStop(0, `rgba(255,190,110,${0.9 * dark})`);
        gr.addColorStop(1, 'rgba(255,190,110,0)');
        g.fillStyle = gr;
        g.fillRect(x - w * 0.02, y - w * 0.02, w * 0.04, w * 0.04);
      }
    }
    return c;
  };

  hx.on('weather', () => (cur = state()));
  hx.on('resize', () => {
    stars = null;
    landKey = '';
  });
  hx.on('dblclick', (e) => {
    if (e.block) return;
    demo = (demo + 1) % DEMO.length;
    demoLabel = 2.5;
    cur = state();
    cloudKey = '';
    landKey = '';
  });

  let since = 0;
  hx.on('frame', (t, dt) => {
    const ctx = hx.ctx;
    const W = hx.w;
    const H = hx.h;
    since += dt;
    if (since > 20) {
      since = 0;
      cur = state();
    }
    const s = cur;
    const [zen, hor, lightOn, sunC] = skyAt(s.alt);
    const over = clamp((s.cloud - 40) / 60, 0, 1) * (s.fog ? 1 : 0.85);
    const grey = s.alt > 0 ? [150, 156, 166] : [24, 28, 36];
    const z = mixc(zen, mixc(grey, zen, 0.3), over);
    const h = mixc(hor, grey, over * 0.85);
    const horizonY = H * 0.62;

    // The sky.
    const sky = ctx.createLinearGradient(0, 0, 0, horizonY + H * 0.1);
    sky.addColorStop(0, css(z));
    sky.addColorStop(0.65, css(mixc(z, h, 0.55)));
    sky.addColorStop(1, css(h));
    ctx.fillStyle = sky;
    ctx.fillRect(0, 0, W, H);

    // Stars and the moon at night.
    const night = clamp((-s.alt - 4) / 10, 0, 1) * (1 - over);
    if (night > 0.01) {
      if (!stars) bakeStars();
      ctx.globalAlpha = night * (0.85 + 0.15 * Math.sin(t * 0.7));
      ctx.drawImage(stars, 0, 0);
      ctx.globalAlpha = 1;
      const mx = W * 0.72;
      const my = H * 0.18;
      const mg = ctx.createRadialGradient(mx, my, 0, mx, my, 90);
      mg.addColorStop(0, `rgba(220,230,255,${0.35 * night})`);
      mg.addColorStop(1, 'rgba(220,230,255,0)');
      ctx.fillStyle = mg;
      ctx.fillRect(mx - 90, my - 90, 180, 180);
      ctx.fillStyle = `rgba(240,244,255,${0.95 * night})`;
      ctx.beginPath();
      ctx.arc(mx, my, 13, 0, TAU);
      ctx.fill();
      ctx.fillStyle = `rgba(190,200,225,${0.35 * night})`;
      for (const [dx, dy, r] of [
        [-4, -3, 3],
        [3, 4, 2.2],
        [5, -5, 1.6],
      ]) {
        ctx.beginPath();
        ctx.arc(mx + dx, my + dy, r, 0, TAU);
        ctx.fill();
      }
    }

    // The sun: its bearing across the screen (east on the left), its height above the horizon.
    const sx = W * clamp(0.5 + ((s.az - 180) / 140) * 0.45, -0.1, 1.1);
    const sy = horizonY - (s.alt / 60) * horizonY * 0.95;
    const sunVis = clamp((s.alt + 3) / 6, 0, 1) * (1 - clamp(s.cloud / 100, 0, 1) * 0.85);
    if (sunVis > 0.01) {
      ctx.globalCompositeOperation = 'lighter';
      const R = H * 0.45;
      const sg = ctx.createRadialGradient(sx, sy, 0, sx, sy, R);
      sg.addColorStop(0, css(sunC, 0.55 * sunVis));
      sg.addColorStop(0.08, css(sunC, 0.25 * sunVis));
      sg.addColorStop(0.4, css(sunC, 0.07 * sunVis));
      sg.addColorStop(1, css(sunC, 0));
      ctx.fillStyle = sg;
      ctx.fillRect(sx - R, sy - R, R * 2, R * 2);
      ctx.globalCompositeOperation = 'source-over';
      ctx.fillStyle = css(mixc(sunC, [255, 255, 255], 0.6), sunVis);
      ctx.beginPath();
      ctx.arc(sx, sy, 16, 0, TAU);
      ctx.fill();
    }

    // Clouds, lit by the hour; more and darker with the cover.
    const lit = mixc(mixc(lightOn, sunC, clamp(1 - Math.abs(s.alt) / 8, 0, 1) * 0.5), grey, over * 0.5);
    const shade = mixc(mixc(lit, z, 0.55), [40, 44, 56], over * 0.5 + (s.storm ? 0.3 : 0));
    const key = `${Math.round(s.alt / 3)}|${Math.round(s.cloud / 10)}|${s.storm}`;
    if (key !== cloudKey) {
      cloudKey = key;
      cloudSprites = bakeClouds(lit, shade);
      const want = Math.round((s.cloud / 100) * 22);
      while (clouds.length < want)
        clouds.push({
          x: Math.random() * W,
          y: Math.random() * horizonY * 0.75,
          s: 0.5 + Math.random() * 0.9,
          i: (Math.random() * 9) | 0,
        });
      clouds.length = want;
    }
    for (const c of clouds) {
      c.x += (s.vx * 0.35 + 3) * c.s * dt;
      const img = cloudSprites[c.i];
      const w = img.width * c.s * (W / 1400);
      const hh = img.height * c.s * (W / 1400);
      if (c.x > W + w) c.x = -w;
      if (c.x < -w * 1.1) c.x = W + w * 0.5;
      ctx.globalAlpha = clamp(0.55 + c.s * 0.4, 0, 1);
      ctx.drawImage(img, c.x - w / 2, c.y, w, hh);
    }
    ctx.globalAlpha = 1;
    if (over > 0) {
      // An overcast deck: a low, even grey ceiling.
      const deck = ctx.createLinearGradient(0, 0, 0, horizonY);
      deck.addColorStop(0, css(shade, 0.75 * over));
      deck.addColorStop(1, css(shade, 0.25 * over));
      ctx.fillStyle = deck;
      ctx.fillRect(0, 0, W, horizonY);
    }

    // The land, relit by the hour.
    const img = s.winter ? LAND.winter : LAND.summer;
    if (img.complete && img.naturalWidth) {
      const dark = clamp((-s.alt + 2) / 12, 0, 1);
      const tint = mixc(mixc(lightOn, grey, over * 0.5), [255, 255, 255], 0);
      const lk = `${s.winter}|${W}|${Math.round(s.alt / 2)}|${Math.round(s.cloud / 10)}`;
      if (lk !== landKey) {
        landKey = lk;
        land = bakeLand(img, tint, dark);
      }
      ctx.drawImage(land, 0, H - land.height);
    }

    // Fog: soft bands drifting across the valley.
    if (s.fog || s.raining) {
      fogPh += dt * (0.02 + s.wind * 0.004);
      const k = s.fog ? 0.75 : 0.25;
      for (let i = 0; i < 4; i++) {
        const y = horizonY + H * (0.02 + i * 0.07);
        const g = ctx.createLinearGradient(0, y - 60, 0, y + 60);
        const fc = mixc(lit, grey, 0.4);
        g.addColorStop(0, css(fc, 0));
        g.addColorStop(0.5, css(fc, k * (0.5 + 0.2 * Math.sin(fogPh * 3 + i))));
        g.addColorStop(1, css(fc, 0));
        ctx.fillStyle = g;
        ctx.fillRect(0, y - 60, W, 120);
      }
      if (s.fog) {
        ctx.fillStyle = css(mixc(lit, grey, 0.4), 0.35);
        ctx.fillRect(0, 0, W, H);
      }
    }

    // Rain: long thin streaks slanted by the wind, at three depths.
    if (s.raining) {
      const n = Math.round((clamp(80 + (s.rain || 1.5) * 120, 80, 700) * (W * H)) / (1400 * 900));
      while (drops.length < n)
        drops.push({ x: Math.random() * W, y: Math.random() * H, z: 0.4 + Math.random() * 0.8 });
      drops.length = n;
      ctx.strokeStyle = css(mixc(lit, [200, 210, 225], 0.5), 0.32);
      ctx.lineCap = 'round';
      ctx.beginPath();
      for (const d of drops) {
        const vy = 900 * d.z;
        const vx = s.vx * 2 * d.z;
        d.y += vy * dt;
        d.x += vx * dt;
        if (d.y > H) {
          d.y = -20;
          d.x = Math.random() * W;
        }
        const l = 0.022 * d.z;
        ctx.moveTo(d.x, d.y);
        ctx.lineTo(d.x - vx * l, d.y - vy * l);
      }
      ctx.lineWidth = 1;
      ctx.stroke();
    }

    // Snow.
    if (s.snowing) {
      snow.update(t, dt);
      snow.draw(ctx, t, 'flakes');
    }

    // Lightning: a flash of the whole sky, now and then a bolt.
    if (s.storm) {
      if (flash <= 0 && Math.random() < dt * 0.25) {
        flash = 1;
        let x = W * (0.2 + Math.random() * 0.6);
        let y = 0;
        bolt = [[x, y]];
        while (y < horizonY) {
          y += 18 + Math.random() * 30;
          x += (Math.random() - 0.5) * 50;
          bolt.push([x, y]);
        }
      }
      if (flash > 0) {
        flash -= dt * 2.2;
        const a = Math.max(0, flash) * (0.6 + 0.4 * Math.sin(t * 60));
        ctx.fillStyle = `rgba(220,225,255,${0.35 * a})`;
        ctx.fillRect(0, 0, W, H);
        if (bolt && flash > 0.5) {
          ctx.strokeStyle = `rgba(240,240,255,${a})`;
          ctx.lineWidth = 2;
          ctx.shadowColor = 'rgba(180,190,255,1)';
          ctx.shadowBlur = 18;
          ctx.beginPath();
          bolt.forEach(([x, y], i) => ctx[i ? 'lineTo' : 'moveTo'](x, y));
          ctx.stroke();
          ctx.shadowBlur = 0;
        }
      }
    }

    // Keeps the chat readable: a veil over a bright sky in the dark theme, over a dark one in the light theme.
    const lum = (0.2126 * z[0] + 0.7152 * z[1] + 0.0722 * z[2]) / 255;
    if (hx.mode === 'dark') {
      ctx.fillStyle = `rgba(6,10,20,${clamp(0.18 + lum * 0.55, 0, 0.62)})`;
    } else {
      ctx.fillStyle = `rgba(255,255,255,${clamp(0.2 + (1 - lum) * 0.45, 0, 0.6)})`;
    }
    ctx.fillRect(0, 0, W, H);

    if (demoLabel > 0) {
      demoLabel -= dt;
      const d = DEMO[demo];
      ctx.globalAlpha = clamp(demoLabel, 0, 1);
      ctx.font = '600 15px system-ui, sans-serif';
      ctx.textAlign = 'center';
      ctx.fillStyle = hx.mode === 'dark' ? '#fff' : '#111';
      ctx.fillText(d ? `Показ: ${d.name}` : 'Погода сейчас', W / 2, 40);
      ctx.globalAlpha = 1;
    }
  });
} else {
  // Over the chat: in a snowfall the snow settles on the blocks; in the rain drops run down the glass.
  const snow = Snow({
    density: 0.35,
    caps: true,
    ground: false,
    start: 0.15,
    capMax: 16,
    wind: () => cur.vx,
  });
  let cur = state();
  const glass = [];
  hx.on('weather', () => (cur = state()));
  hx.on('dblclick', (e) => {
    if (e.block) return snow.shake(e.block.id, 1);
    demo = (demo + 1) % DEMO.length;
    cur = state();
  });
  hx.on('frame', (t, dt) => {
    const ctx = hx.ctx;
    ctx.clearRect(0, 0, hx.w, hx.h);
    if (cur.snowing || cur.winter) {
      if (cur.snowing) snow.update(t, dt);
      snow.draw(ctx, t, cur.snowing ? 'all' : 'lying');
    }
    if (cur.raining) {
      if (glass.length < 70 && Math.random() < dt * 14)
        glass.push({
          x: Math.random() * hx.w,
          y: Math.random() * hx.h * 0.8,
          r: 1.5 + Math.random() * 3.5,
          v: 0,
          still: 1 + Math.random() * 4,
        });
      for (let i = glass.length - 1; i >= 0; i--) {
        const d = glass[i];
        d.still -= dt;
        if (d.still < 0) d.v = Math.min(260, d.v + 300 * dt * (d.r / 4));
        d.y += d.v * dt;
        d.x += Math.sin(d.y * 0.05) * 0.2;
        if (d.y > hx.h + 10) {
          glass.splice(i, 1);
          continue;
        }
        // A drop: a dark rim, a lens of light, a bright spark on top.
        const g = ctx.createRadialGradient(d.x - d.r * 0.3, d.y - d.r * 0.4, 0, d.x, d.y, d.r);
        g.addColorStop(0, 'rgba(255,255,255,0.55)');
        g.addColorStop(0.5, 'rgba(200,215,235,0.12)');
        g.addColorStop(0.9, 'rgba(20,30,45,0.25)');
        g.addColorStop(1, 'rgba(20,30,45,0)');
        ctx.fillStyle = g;
        ctx.beginPath();
        ctx.ellipse(d.x, d.y, d.r * 0.85, d.r * (d.v > 0 ? 1.25 : 1), 0, 0, TAU);
        ctx.fill();
        if (d.v > 0) {
          ctx.strokeStyle = 'rgba(200,215,235,0.12)';
          ctx.lineWidth = d.r * 0.5;
          ctx.beginPath();
          ctx.moveTo(d.x, d.y - d.r);
          ctx.lineTo(d.x, d.y - d.r - Math.min(80, d.v * 0.3));
          ctx.stroke();
        }
      }
    } else glass.length = 0;
  });
}
