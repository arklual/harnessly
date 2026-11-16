// The talk's orb: one canvas, drawn at the screen's rate while it is seen. The colours are the product's
// tokens (read again when the theme changes); a weak device gets fewer points and no second layer, not a slower frame;
// reduced motion draws one still orb in the state's colour.
import { useEffect, useRef } from 'react';
import { ease, orbLook, outline, type TOrb } from '@/next/features/voice-live/orb-model';

interface IOrbProps {
  mode: TOrb;
  /** Loudness 0…1 of whoever speaks, read each frame; null while nobody does. */
  level: (() => number) | null;
  className?: string;
}

interface IPalette {
  main: string;
  soft: string;
  light: string;
  dim: string;
}

const MAIN: Record<TOrb, string> = {
  listening: '--hx-danger',
  speaking: '--hx-accent',
  thinking: '--hx-accent',
  waiting: '--hx-fg-3',
  failed: '--hx-fg-3',
};

/** A canvas takes a colour only as a string it can parse: tokens are resolved through a probe element. */
const colorOf = (probe: HTMLElement, token: string): string => {
  probe.style.color = `var(${token})`;
  return getComputedStyle(probe).color;
};

const mixOf = (probe: HTMLElement, a: string, b: string, share: number): string => {
  probe.style.color = `color-mix(in srgb, ${a} ${Math.round(share * 100)}%, ${b})`;
  return getComputedStyle(probe).color;
};

const paletteOf = (probe: HTMLElement, mode: TOrb): IPalette => {
  const main = colorOf(probe, MAIN[mode]);
  const fg = colorOf(probe, '--hx-fg-1');
  return {
    main,
    soft: mixOf(probe, main, 'transparent', 0.35),
    light: mixOf(probe, fg, main, 0.28),
    dim: mixOf(probe, main, 'transparent', 0),
  };
};

const weak = (): boolean => {
  const nav = navigator as Navigator & { deviceMemory?: number };
  return (nav.hardwareConcurrency ?? 8) <= 4 || (nav.deviceMemory ?? 8) <= 2;
};

const VoiceOrb = ({ mode, level, className }: IOrbProps) => {
  const canvas = useRef<HTMLCanvasElement>(null);
  // The latest props are read inside the loop; the loop itself starts once.
  const live = useRef({ mode, level });
  useEffect(() => {
    live.current = { mode, level };
  });
  const redraw = useRef<(() => void) | null>(null);
  useEffect(() => {
    redraw.current?.();
  }, [mode]);

  useEffect(() => {
    const el = canvas.current;
    const ctx = el?.getContext('2d');
    if (!el || !ctx) return;
    const probe = document.createElement('span');
    probe.style.display = 'none';
    el.parentElement?.appendChild(probe);
    const still = window.matchMedia('(prefers-reduced-motion: reduce)');
    const points = weak() ? 36 : 72;
    const layers = weak() ? 1 : 2;
    let size = 0;
    let dpr = 1;
    let palette = paletteOf(probe, live.current.mode);
    let paletteMode = live.current.mode;
    const state = { amp: 0.02, scale: 0.9, alpha: 0.7, spin: 0, speed: 0.4, phase: 0, angle: 0, loud: 0 };
    let last = 0;
    let t = 0;
    let frame = 0;

    const fit = () => {
      const box = el.getBoundingClientRect();
      size = Math.max(1, Math.round(box.width));
      dpr = Math.min(2, window.devicePixelRatio || 1);
      el.width = Math.round(size * dpr);
      el.height = Math.round(size * dpr);
    };

    const blob = (radius: number, phase: number, amp: number) => {
      const r = outline(points, amp, phase);
      const c = size / 2;
      ctx.beginPath();
      for (let i = 0; i <= points; i++) {
        const k = i % points;
        const a = (k / points) * Math.PI * 2;
        const x = c + Math.cos(a) * radius * r[k];
        const y = c + Math.sin(a) * radius * r[k];
        if (i === 0) ctx.moveTo(x, y);
        else ctx.lineTo(x, y);
      }
      ctx.closePath();
    };

    const draw = () => {
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      ctx.clearRect(0, 0, size, size);
      const c = size / 2;
      const base = size * 0.3 * state.scale;
      ctx.globalAlpha = state.alpha;
      // The glow around the orb.
      const glow = ctx.createRadialGradient(c, c, base * 0.6, c, c, size * 0.5);
      glow.addColorStop(0, palette.soft);
      glow.addColorStop(1, palette.dim);
      ctx.fillStyle = glow;
      ctx.fillRect(0, 0, size, size);
      for (let layer = layers - 1; layer >= 0; layer--) {
        const back = layer > 0;
        blob(base * (back ? 1.06 : 1), state.phase + layer * 2.3, state.amp * (back ? 1.35 : 1));
        if (back) {
          ctx.globalAlpha = state.alpha * 0.45;
          ctx.fillStyle = palette.soft;
        } else {
          ctx.globalAlpha = state.alpha;
          const lx = c + Math.cos(state.angle) * base * 0.35;
          const ly = c + Math.sin(state.angle) * base * 0.35 - base * 0.1;
          const body = ctx.createRadialGradient(lx, ly, base * 0.05, c, c, base * 1.15);
          body.addColorStop(0, palette.light);
          body.addColorStop(0.55, palette.main);
          body.addColorStop(1, palette.soft);
          ctx.fillStyle = body;
        }
        ctx.fill();
      }
      ctx.globalAlpha = 1;
    };

    const refreshPalette = () => {
      palette = paletteOf(probe, live.current.mode);
      paletteMode = live.current.mode;
    };

    const step = (now: number) => {
      const dt = last ? Math.min(0.1, (now - last) / 1000) : 0.016;
      last = now;
      t += dt;
      if (paletteMode !== live.current.mode) refreshPalette();
      const raw = live.current.level ? live.current.level() : 0;
      // The loudness itself is quick up and slow down: a voice's shape, not a flicker.
      state.loud = ease(state.loud, raw, dt, raw > state.loud ? 22 : 7);
      const goal = orbLook(live.current.mode, state.loud, t);
      state.amp = ease(state.amp, goal.amp, dt, 14);
      state.scale = ease(state.scale, goal.scale, dt, 10);
      state.alpha = ease(state.alpha, goal.alpha, dt, 5);
      state.spin = ease(state.spin, goal.spin, dt, 4);
      state.speed = ease(state.speed, goal.speed, dt, 4);
      state.phase += dt * state.speed * 2.2;
      state.angle += dt * state.spin * Math.PI * 2;
      draw();
      frame = requestAnimationFrame(step);
    };

    const still1 = () => {
      refreshPalette();
      const goal = orbLook(live.current.mode, 0, 0);
      Object.assign(state, { amp: 0, scale: goal.scale, alpha: goal.alpha, angle: -Math.PI / 2 });
      draw();
    };

    const stop = () => {
      cancelAnimationFrame(frame);
      frame = 0;
      last = 0;
    };
    const run = () => {
      stop();
      if (still.matches) {
        still1();
        return;
      }
      if (!document.hidden) frame = requestAnimationFrame(step);
    };
    redraw.current = run;

    fit();
    run();
    const ro = new ResizeObserver(() => {
      fit();
      if (still.matches) still1();
    });
    ro.observe(el);
    // A hidden tab draws nothing; the theme changes the palette.
    document.addEventListener('visibilitychange', run);
    still.addEventListener('change', run);
    const theme = new MutationObserver(() => {
      refreshPalette();
      if (still.matches) still1();
    });
    theme.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ['data-theme', 'class', 'style'],
    });
    return () => {
      stop();
      redraw.current = null;
      ro.disconnect();
      theme.disconnect();
      document.removeEventListener('visibilitychange', run);
      still.removeEventListener('change', run);
      probe.remove();
    };
  }, []);

  return <canvas ref={canvas} className={className} data-orb={mode} aria-hidden="true" />;
};

export default VoiceOrb;
