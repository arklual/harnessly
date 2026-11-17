// «Мастерская»: a cozy New Year workshop around the chat. Under it (layer back) the picture lives: the garland's bulbs
// twinkle, the desk lamp's filament breathes, the lantern flickers, steam rises from the cocoa, snow falls behind the
// window, the snow globe sparkles. Over it (layer front) snow lies on the blocks, warm from the lamps, and every finished
// answer gets a red bow. The pictures are ART_NEWYEAR (art-newyear.js) and ART_BOW (art-bow.js).
/* global hx, Snow, SceneLib, ART_NEWYEAR, ART_BOW */
const L = SceneLib;
if (hx.layer === 'back') {
  const img = L.load(ART_NEWYEAR.bg);
  const cover = L.Cover(img, 0.5, 0.35);
  let bulbs = null;
  const steam = L.Steam();
  const winSnow = L.WindowSnow(90);
  const WARM = [255, 196, 120];
  hx.on('frame', (t, dt) => {
    const ctx = hx.ctx;
    ctx.clearRect(0, 0, hx.w, hx.h);
    if (!L.ready(img)) return;
    cover.draw(ctx);
    if (!bulbs) bulbs = L.lights(img, [0, 0, 1, 0.07], 36);
    const k = cover.len(1) / 1600;
    // Behind the window: snow.
    const [wx, wy] = cover.at(0.02, 0.07);
    const [wx2, wy2] = cover.at(0.18, 0.47);
    winSnow(ctx, wx, wy, wx2 - wx, wy2 - wy, t, dt);
    ctx.globalCompositeOperation = 'lighter';
    // The garland's bulbs, each on its own slow beat.
    for (const b of bulbs) {
      const [x, y] = cover.at(b.u, b.v);
      const a = 0.25 + 0.35 * (0.5 + 0.5 * Math.sin(t * b.sp + b.ph));
      L.glow(ctx, x, y, 16 * k, WARM, a * hx.power);
    }
    // The desk lamp, the lantern, the moon, the snow globe.
    const lamp = L.flicker(t, 0.3);
    L.glow(ctx, ...cover.at(0.114, 0.57), 170 * k, [255, 200, 120], 0.22 * lamp * (0.6 + hx.power * 0.6));
    const fl = L.flicker(t * 1.6, 2.1);
    L.glow(ctx, ...cover.at(0.96, 0.125), 70 * k, [255, 170, 80], 0.38 * fl);
    L.glow(ctx, ...cover.at(0.142, 0.165), 46 * k, [210, 225, 255], 0.12 + 0.03 * Math.sin(t * 0.5));
    const sg = 0.5 + 0.5 * Math.sin(t * 2.2);
    L.glow(ctx, ...cover.at(0.948, 0.33), 30 * k, [255, 240, 210], 0.12 * sg);
    ctx.globalCompositeOperation = 'source-over';
    // Steam over the cocoa.
    const [mx, my] = cover.at(0.233, 0.665);
    steam(ctx, mx, my, k, dt);
  });
} else {
  const snow = Snow({
    density: 0,
    caps: true,
    ground: false,
    start: 0.75,
    capMax: 18,
    tint: [255, 214, 170],
  });
  const bows = L.Bows(L.load(ART_BOW.bow), L.load(ART_BOW.tag));
  hx.on('answer-end', (e) => e.block && bows.tie(e.block.id, true));
  hx.on('dblclick', (e) => e.block && snow.shake(e.block.id, 1));
  hx.on('frame', (t, dt) => {
    const ctx = hx.ctx;
    ctx.clearRect(0, 0, hx.w, hx.h);
    snow.update(t, dt);
    snow.draw(ctx, t, 'all');
    bows.draw(ctx, dt, 50);
  });
}
