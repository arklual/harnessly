// A look turned into the app's tokens (--hx-*): the same surfaces palettes.css tints by hand, here from the look's
// hues and intensity. The text and the states stay the base's; every colour text sits on is checked against it
// (WCAG 2: 4.5:1 for the secondary text and the accent's text, 3:1 for the faint text, the lines that matter and the
// accent as a control) and moved in lightness until it passes. Pure: the tests and the worker's colours use it too.
import type { IHue, ILook } from '@/next/features/looks/types';

export type TMode = 'light' | 'dark';

/** oklch → linear sRGB, each 0–1 (clamped). */
export const oklchToLinear = (l: number, c: number, h: number): [number, number, number] => {
  const rad = (h * Math.PI) / 180;
  const a = c * Math.cos(rad);
  const b = c * Math.sin(rad);
  const l_ = (l + 0.3963377774 * a + 0.2158037573 * b) ** 3;
  const m_ = (l - 0.1055613458 * a - 0.0638541728 * b) ** 3;
  const s_ = (l - 0.0894841775 * a - 1.291485548 * b) ** 3;
  const clamp = (x: number) => Math.min(1, Math.max(0, x));
  return [
    clamp(4.0767416621 * l_ - 3.3077115913 * m_ + 0.2309699292 * s_),
    clamp(-1.2684380046 * l_ + 2.6097574011 * m_ - 0.3413193965 * s_),
    clamp(-0.0041960863 * l_ - 0.7034186147 * m_ + 1.707614701 * s_),
  ];
};

const luminance = (l: number, c: number, h: number): number => {
  const [r, g, b] = oklchToLinear(l, c, h);
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
};

/** The WCAG contrast of two oklch colours. */
export const contrast = (
  a: readonly [number, number, number],
  b: readonly [number, number, number],
): number => {
  const ya = luminance(...a);
  const yb = luminance(...b);
  return (Math.max(ya, yb) + 0.05) / (Math.min(ya, yb) + 0.05);
};

const gamma = (x: number) => (x <= 0.0031308 ? 12.92 * x : 1.055 * x ** (1 / 2.4) - 0.055);

/** An oklch colour as rgb() for a canvas (a worker has no CSS parser for oklch everywhere). */
export const rgbOf = (l: number, c: number, h: number, alpha = 1): string => {
  const [r, g, b] = oklchToLinear(l, c, h).map((x) => Math.round(gamma(x) * 255));
  return alpha >= 1 ? `rgb(${r} ${g} ${b})` : `rgb(${r} ${g} ${b} / ${alpha})`;
};

const r3 = (x: number) => Math.round(x * 1000) / 1000;
const ok = (l: number, c: number, h: number, alpha?: number) =>
  alpha === undefined
    ? `oklch(${r3(l)} ${r3(c)} ${Math.round(h)})`
    : `oklch(${r3(l)} ${r3(c)} ${Math.round(h)} / ${alpha})`;

type TOk = [number, number, number];

/** Moves `fg`'s lightness away from `bg` until their contrast is at least `min` (or the end of the scale). */
export const ensure = (fg: TOk, bg: TOk, min: number, mode: TMode): TOk => {
  const step = mode === 'dark' ? 0.01 : -0.01;
  let [l] = fg;
  const [, c, h] = fg;
  for (let i = 0; i < 100 && contrast([l, c, h], bg) < min; i++) {
    const next = l + step;
    if (next < 0 || next > 1) break;
    l = next;
  }
  return [l, c, h];
};

/** How strongly the hues tint: 1 is the hand-made palettes' strength (midnight, forest…). */
const tintOf = (hue: IHue, intensity: number) => Math.min(2.6, (hue.s / 50) * (intensity / 50));

/** The tokens of a look in a mode, by name (without the leading --). */
export const lookTokens = (look: ILook, mode: TMode): Record<string, string> => {
  const [first, second = first, third = second] = look.hues;
  const h1 = first.h;
  const h2 = second.h;
  const h3 = third.h;
  const t = tintOf(first, look.intensity);
  const t2 = tintOf(second, look.intensity);
  const t3 = tintOf(third, look.intensity);
  const accentHue = look.accent ?? first;
  const ac = 0.04 + 0.16 * Math.min(1, accentHue.s / 70);
  const out: Record<string, string> = {};
  if (mode === 'light') {
    const app: TOk = [0.995, 0.0055 * t, h1];
    const fg2 = ensure([0.5, 0.035 * t, h1], app, 4.5, mode);
    const fg3 = ensure([0.64, 0.031 * t, h1], app, 3, mode);
    const accent = ensure([0.55, ac, accentHue.h], app, 4.5, mode);
    Object.assign(out, {
      'hx-bg-app': ok(...app),
      'hx-bg-rail': ok(0.972, 0.022 * t2, h2),
      'hx-bg-stage': ok(0.985, 0.011 * t, h1),
      'hx-bg-raised': ok(0.998, 0.0044 * t, h1),
      'hx-bg-bubble': ok(0.952, 0.0264 * t3, h3),
      'hx-bg-hover': ok(0.35, 0.066 * t, h1, 0.05),
      'hx-bg-active': ok(0.35, 0.066 * t, h1, 0.085),
      'hx-fg-2': ok(...fg2),
      'hx-fg-3': ok(...fg3),
      'hx-border': ok(0.925, 0.0264 * t, h1),
      'hx-border-strong': ok(0.875, 0.0308 * t, h1),
      'hx-accent': ok(...accent),
      'hx-accent-soft': ok(accent[0], accent[1], accent[2], 0.12),
      'hx-focus': ok(accent[0], accent[1], accent[2], 0.35),
      'hx-shell': `linear-gradient(180deg, ${ok(0.97, 0.0242 * t2, h2)} 0%, ${ok(0.94, 0.0352 * t3, h3)} 100%)`,
      'hx-shell-hover': ok(0.35, 0.066 * t2, h2, 0.05),
      'hx-shell-active': ok(0.998, 0.0044 * t2, h2),
      'hx-bg-window': ok(0.997, 0.0044 * t, h1),
      'hx-bg-canvas': ok(0.94, 0.0264 * t, h1),
    });
  } else {
    const app: TOk = [0.165, 0.022 * t, h1];
    const fg2 = ensure([0.7, 0.0264 * t, h1], app, 4.5, mode);
    const fg3 = ensure([0.56, 0.0264 * t, h1], app, 3, mode);
    const accent = ensure([0.74, Math.min(0.15, ac), accentHue.h], app, 4.5, mode);
    Object.assign(out, {
      'hx-bg-backdrop': ok(0.11, 0.022 * t2, h2),
      'hx-bg-app': ok(...app),
      'hx-bg-rail': ok(0.14, 0.022 * t2, h2),
      'hx-bg-stage': ok(0.165, 0.022 * t, h1),
      'hx-bg-raised': ok(0.2, 0.0242 * t, h1),
      'hx-bg-bubble': ok(0.23, 0.0264 * t3, h3),
      'hx-bg-hover': ok(0.9, 0.044 * t, h1, 0.055),
      'hx-bg-active': ok(0.9, 0.044 * t, h1, 0.095),
      'hx-fg-2': ok(...fg2),
      'hx-fg-3': ok(...fg3),
      'hx-border': ok(0.85, 0.044 * t, h1, 0.09),
      'hx-border-strong': ok(0.85, 0.044 * t, h1, 0.15),
      'hx-accent': ok(...accent),
      'hx-accent-soft': ok(0.59, Math.min(0.18, ac + 0.02), accentHue.h, 0.18),
      'hx-focus': ok(accent[0], accent[1], accent[2], 0.6),
      'hx-shell': ok(0.14, 0.022 * t2, h2),
      'hx-shell-hover': ok(0.9, 0.044 * t2, h2, 0.055),
      'hx-shell-active': ok(0.9, 0.044 * t2, h2, 0.085),
      'hx-bg-window': ok(0.165, 0.022 * t, h1),
      'hx-bg-canvas': ok(0.125, 0.022 * t, h1),
    });
  }
  const radius = look.radius === 'sharp' ? 0.5 : look.radius === 'round' ? 1.4 : null;
  if (radius !== null) {
    out['hx-radius-sm'] = `${Math.round(8 * radius)}px`;
    out['hx-radius-md'] = `${Math.round(12 * radius)}px`;
    out['hx-radius-lg'] = `${Math.round(18 * radius)}px`;
    out['hx-radius-xl'] = `${Math.round(24 * radius)}px`;
  }
  const font = FONT_STACKS[look.font ?? 'inter'];
  if (font) out['hx-font-ui'] = font;
  return out;
};

/** The fonts a look may pick: the ones every system has, no download. Inter is the app's own (no override). */
const FONT_STACKS: Record<string, string | null> = {
  inter: null,
  system: "system-ui, -apple-system, 'Segoe UI', Roboto, sans-serif",
  serif: "'Iowan Old Style', 'Charter', Georgia, 'Times New Roman', serif",
  mono: "'JetBrains Mono', 'SF Mono', ui-monospace, Menlo, Consolas, monospace",
  rounded: "ui-rounded, 'SF Pro Rounded', 'Nunito', 'Varela Round', system-ui, sans-serif",
};

/** The colours a scene paints with, rgb() for the canvas: the background, the hues, the text's ink. */
export interface ISceneColors {
  bg: string;
  hues: string[];
  ink: string;
  /** The stage's colour (--hx-bg-stage), for the dim over the scene. */
  stage: string;
  mode: TMode;
}

export const sceneColors = (look: ILook, mode: TMode): ISceneColors => {
  const t = Math.max(0.6, tintOf(look.hues[0], look.intensity));
  const stageTint = tintOf(look.hues[0], look.intensity);
  const dark = mode === 'dark';
  return {
    mode,
    bg: dark ? rgbOf(0.13, 0.025 * t, look.hues[0].h) : rgbOf(0.975, 0.012 * t, look.hues[0].h),
    hues: look.hues.map((hue) => rgbOf(dark ? 0.72 : 0.62, 0.05 + 0.15 * Math.min(1, hue.s / 70), hue.h)),
    ink: dark ? rgbOf(0.98, 0, 0) : rgbOf(0.2, 0, 0),
    stage: dark
      ? rgbOf(0.165, 0.022 * stageTint, look.hues[0].h)
      : rgbOf(0.985, 0.011 * stageTint, look.hues[0].h),
  };
};

/** The gradient a look's hues make for a background (kind gradient), as CSS. */
export const gradientOf = (look: ILook, mode: TMode): string => {
  const dark = mode === 'dark';
  const stop = (hue: IHue) =>
    ok(
      dark ? 0.24 : 0.9,
      (0.03 + 0.09 * Math.min(1, hue.s / 70)) * Math.min(1.4, look.intensity / 50),
      hue.h,
    );
  const [a, b = a, c = b] = look.hues;
  return `radial-gradient(120% 90% at 0% 0%, ${stop(a)} 0%, transparent 60%), radial-gradient(110% 90% at 100% 20%, ${stop(b)} 0%, transparent 62%), radial-gradient(120% 100% at 50% 110%, ${stop(c)} 0%, transparent 65%)`;
};

/** A colour background (kind color): the first hue, deep. */
export const colorOf = (look: ILook, mode: TMode): string => {
  const hue = look.hues[0];
  const c = 0.02 + 0.08 * Math.min(1, hue.s / 70) * Math.min(1.4, look.intensity / 50);
  return mode === 'dark' ? ok(0.2, c, hue.h) : ok(0.93, c * 0.6, hue.h);
};
