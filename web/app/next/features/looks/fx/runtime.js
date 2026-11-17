// The SDK a live effect of a look runs against (docs/looks.md, «Живые эффекты»). It is the first script of the
// sandboxed frame (fx-frame.ts): the frame is an opaque origin with no network, so all an effect knows is what the
// page posts here — the size, the pointer, the boxes of the chat's blocks, the agent's state and a few events. Never
// the chat's text. The effect draws on hx.ctx, a transparent canvas the size of the chat's stage.
(() => {
  'use strict';
  const canvas = document.createElement('canvas');
  document.body.appendChild(canvas);
  const ctx = canvas.getContext('2d');
  const handlers = Object.create(null);
  let errors = 0;
  const report = (e) => {
    if (errors++ < 5)
      parent.postMessage({ hxFx: 'err', msg: String((e && e.stack) || e).slice(0, 400) }, '*');
  };
  const emit = (name, ...args) => {
    const list = handlers[name];
    if (!list || errors > 50) return;
    for (const fn of list.slice()) {
      try {
        fn(...args);
      } catch (e) {
        report(e);
      }
    }
  };
  const hx = {
    canvas,
    ctx,
    w: 0,
    h: 0,
    dpr: 1,
    // Known before the effect's code runs (fx-frame.ts writes it on <html>): an effect picks its layer at once.
    layer: document.documentElement.dataset.layer === 'back' ? 'back' : 'front',
    power: 0.6,
    mode: 'dark',
    reduced: false,
    colors: { accent: '#8b7cf6', fg: '#fff', bg: '#111', hues: [] },
    pointer: { x: -1, y: -1, vx: 0, vy: 0, down: false, inside: false },
    blocks: [],
    agent: { state: 'idle', since: 0 },
    /** The weather where the person is («Погода» only; null until the page tells it). */
    weather: null,
    /** Subscribes fn to an event; returns the unsubscribe. */
    on(name, fn) {
      (handlers[name] = handlers[name] || []).push(fn);
      return () => {
        handlers[name] = (handlers[name] || []).filter((f) => f !== fn);
      };
    },
    /** The block under a point, or null. */
    blockAt(x, y) {
      for (let i = hx.blocks.length - 1; i >= 0; i--) {
        const b = hx.blocks[i];
        if (x >= b.x && x <= b.x + b.w && y >= b.y && y <= b.y + b.h) return b;
      }
      return null;
    },
    /** A random number in [a, b). */
    rand: (a = 0, b = 1) => a + Math.random() * (b - a),
  };
  window.hx = hx;

  const size = (m) => {
    hx.w = m.w;
    hx.h = m.h;
    hx.dpr = Math.min(2, m.dpr || 1);
    canvas.width = Math.max(1, Math.round(hx.w * hx.dpr));
    canvas.height = Math.max(1, Math.round(hx.h * hx.dpr));
    canvas.style.width = hx.w + 'px';
    canvas.style.height = hx.h + 'px';
    ctx.setTransform(hx.dpr, 0, 0, hx.dpr, 0, 0);
  };

  let started = false;
  let last = 0;
  let lastPtr = 0;
  const loop = (now) => {
    const dt = last ? Math.min(0.05, (now - last) / 1000) : 0;
    last = now;
    // The pointer's speed fades out when it stops moving.
    if (now - lastPtr > 60) {
      hx.pointer.vx *= 0.8;
      hx.pointer.vy *= 0.8;
    }
    emit('frame', now / 1000, dt);
    requestAnimationFrame(loop);
  };

  addEventListener('message', (event) => {
    if (event.source !== parent) return;
    const m = event.data;
    if (!m || typeof m !== 'object') return;
    switch (m.hx) {
      case 'init':
        hx.layer = m.layer;
        hx.power = m.power;
        hx.mode = m.mode;
        hx.colors = m.colors;
        hx.reduced = !!m.reduced;
        size(m);
        if (!started) {
          started = true;
          emit('start');
          requestAnimationFrame(loop);
        } else emit('resize');
        break;
      case 'size':
        size(m);
        emit('resize');
        break;
      case 'theme':
        hx.mode = m.mode;
        hx.colors = m.colors;
        hx.power = m.power;
        emit('theme');
        break;
      case 'ptr': {
        const p = hx.pointer;
        const now = performance.now();
        const dt = Math.max(8, now - lastPtr) / 1000;
        if (p.inside && m.inside) {
          p.vx = p.vx * 0.5 + ((m.x - p.x) / dt) * 0.5;
          p.vy = p.vy * 0.5 + ((m.y - p.y) / dt) * 0.5;
        } else {
          p.vx = 0;
          p.vy = 0;
        }
        lastPtr = now;
        p.x = m.x;
        p.y = m.y;
        p.down = !!m.down;
        p.inside = !!m.inside;
        emit('pointer', p);
        break;
      }
      case 'blocks':
        hx.blocks = m.list;
        emit('blocks', hx.blocks);
        break;
      case 'agent':
        hx.agent = { state: m.state, since: m.since };
        emit('state', hx.agent);
        break;
      case 'weather': {
        const { hx: _k, ...w } = m;
        hx.weather = w;
        emit('weather', w);
        break;
      }
      case 'ev': {
        const data = m.data || {};
        if (data.block) data.block = hx.blocks.find((b) => b.id === data.block) || null;
        emit(m.name, data);
        break;
      }
    }
  });
  addEventListener('error', (e) => report(e.error || e.message));
  parent.postMessage({ hxFx: 'ready' }, '*');
})();
