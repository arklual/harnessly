// Chart look shared with the admin panel. Ported from
// @hl/design-system/addons/charts 0.5.0 (theme/resolve-chart-theme and
// options/base) the same way hl-tokens.css copies the design tokens: the
// package lives in the private Nexus registry, and this app is built on every
// runner, so it takes the look rather than the dependency.

import { useEffect, useState } from 'react';

export interface IChartTheme {
  dark: boolean;
  palette: string[];
  text: string;
  axis: string;
  border: string;
  surface: string;
  muted: string;
  /** Sequential ramp for heatmaps: empty cell, then light → strong purple. */
  ramp: string[];
  fontFamily: string;
}

// Design-system hues in an order validated for colour-vision deficiency
// (adjacent ΔE ≥ 11 deutan, normal-vision ≥ 23) and for each theme's surface.
const PALETTE_LIGHT = ['#6d49f4', '#00af61', '#127eee', '#ea9602', '#00a2a3', '#ec305a'];
const PALETTE_DARK = ['#7a5aff', '#00af61', '#1d84f5', '#cf7d00', '#00a2a3', '#ec305a'];
const RAMP_LIGHT = ['#ece9f6', '#c9bdfb', '#a18af8', '#7a5aff', '#5a35d6'];
const RAMP_DARK = ['#26242e', '#3d3170', '#5a43b8', '#7a5aff', '#b3a1ff'];

/** oklch() → rgb(): zrender colour math returns undefined for oklch and hovered marks vanish. */
export const oklchToRgb = (value: string): string => {
  const m = value
    .trim()
    .match(/^oklch\(\s*([\d.]+%?)\s+([\d.]+%?)\s+([\d.]+)(?:deg)?\s*(?:\/\s*([\d.]+%?))?\s*\)$/i);
  if (!m) return value.trim();
  const pct = (s: string, scale = 1) => (s.endsWith('%') ? (parseFloat(s) / 100) * scale : parseFloat(s));
  const L = pct(m[1]);
  const C = pct(m[2], 0.4);
  const H = (parseFloat(m[3]) * Math.PI) / 180;
  const alpha = m[4] ? pct(m[4]) : 1;
  const a = C * Math.cos(H);
  const b = C * Math.sin(H);
  const l = (L + 0.3963377774 * a + 0.2158037573 * b) ** 3;
  const mm = (L - 0.1055613458 * a - 0.0638541728 * b) ** 3;
  const s = (L - 0.0894841775 * a - 1.291485548 * b) ** 3;
  const lin = [
    4.0767416621 * l - 3.3077115913 * mm + 0.2309699292 * s,
    -1.2684380046 * l + 2.6097574011 * mm - 0.3413193965 * s,
    -0.0041960863 * l - 0.7034186147 * mm + 1.707614701 * s,
  ];
  const [r, g, bl] = lin.map((x) => {
    const v =
      Math.abs(x) <= 0.0031308 ? 12.92 * x : Math.sign(x) * (1.055 * Math.abs(x) ** (1 / 2.4) - 0.055);
    return Math.max(0, Math.min(255, Math.round(v * 255)));
  });
  return alpha >= 1 ? `rgb(${r}, ${g}, ${bl})` : `rgba(${r}, ${g}, ${bl}, ${+alpha.toFixed(3)})`;
};

export const withAlpha = (color: string, alpha: number): string => {
  const rgb = color.match(/^rgba?\(\s*([\d.]+)[,\s]+([\d.]+)[,\s]+([\d.]+)/i);
  if (rgb) return `rgba(${rgb[1]}, ${rgb[2]}, ${rgb[3]}, ${alpha})`;
  const hex = color.match(/^#([0-9a-f]{2})([0-9a-f]{2})([0-9a-f]{2})$/i);
  if (hex) return `rgba(${parseInt(hex[1], 16)}, ${parseInt(hex[2], 16)}, ${parseInt(hex[3], 16)}, ${alpha})`;
  return color;
};

const read = (name: string) => oklchToRgb(getComputedStyle(document.documentElement).getPropertyValue(name));

export const resolveChartTheme = (): IChartTheme => {
  const dark = document.documentElement.classList.contains('dark');
  return {
    dark,
    palette: dark ? PALETTE_DARK : PALETTE_LIGHT,
    text: read('--t-text'),
    axis: read('--t-text-secondary'),
    border: read('--t-border'),
    surface: read('--t-surface'),
    muted: dark ? '#6b6b73' : '#a3a3ad',
    ramp: dark ? RAMP_DARK : RAMP_LIGHT,
    fontFamily: 'Inter, system-ui, sans-serif',
  };
};

/** Re-resolves when next-themes flips the `dark` class on <html>. */
export const useChartTheme = (): IChartTheme | null => {
  const [theme, setTheme] = useState<IChartTheme | null>(() =>
    typeof document === 'undefined' ? null : resolveChartTheme(),
  );
  useEffect(() => {
    const observer = new MutationObserver(() => setTheme(resolveChartTheme()));
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] });
    return () => observer.disconnect();
  }, []);
  return theme;
};

export const colorAt = (t: IChartTheme, index: number): string =>
  index >= 0 && index < t.palette.length ? t.palette[index] : t.muted;

// --- option fragments (same values as the design system's options/base) ---

export const baseAnimation = () => ({
  animation: true,
  animationDuration: 750,
  animationEasing: 'cubicOut' as const,
  animationDurationUpdate: 0,
});

export const baseTooltip = (t: IChartTheme, extra: Record<string, unknown> = {}) => ({
  confine: true,
  backgroundColor: t.surface,
  borderColor: t.border,
  borderWidth: 1,
  padding: [10, 12],
  extraCssText: 'border-radius:10px;box-shadow:0 8px 28px rgba(0,0,0,.18);',
  textStyle: { color: t.text, fontFamily: t.fontFamily, fontSize: 12 },
  ...extra,
});

export const dashedAxisPointer = (t: IChartTheme) => ({
  type: 'line' as const,
  animation: false,
  lineStyle: { color: withAlpha(t.axis, 0.5), width: 1, type: 'dashed' as const },
});

export const categoryAxis = (t: IChartTheme, data: string[], extra: Record<string, unknown> = {}) => {
  const { axisLabel, ...rest } = extra as { axisLabel?: Record<string, unknown> };
  return {
    type: 'category' as const,
    data,
    boundaryGap: true,
    axisLine: { lineStyle: { color: t.border } },
    axisTick: { show: false },
    axisLabel: {
      color: t.axis,
      fontFamily: t.fontFamily,
      fontSize: 11,
      hideOverlap: true,
      interval: 'auto' as const,
      ...axisLabel,
    },
    ...rest,
  };
};

export const valueAxis = (t: IChartTheme, extra: Record<string, unknown> = {}) => ({
  type: 'value' as const,
  splitLine: { lineStyle: { color: t.border, type: 'dashed' as const, opacity: 0.6 } },
  axisLabel: { color: t.axis, fontFamily: t.fontFamily, fontSize: 11 },
  ...extra,
});

export const clickableLegend = (t: IChartTheme, extra: Record<string, unknown> = {}) => ({
  top: 0,
  right: 0,
  icon: 'roundRect',
  itemWidth: 10,
  itemHeight: 10,
  selectedMode: true,
  inactiveColor: withAlpha(t.axis, 0.45),
  textStyle: { color: t.axis, fontFamily: t.fontFamily, fontSize: 12 },
  ...extra,
});

export const verticalGradient = (color: string, top = 0.3, bottom = 0.01) => ({
  type: 'linear' as const,
  x: 0,
  y: 0,
  x2: 0,
  y2: 1,
  colorStops: [
    { offset: 0, color: withAlpha(color, top) },
    { offset: 1, color: withAlpha(color, bottom) },
  ],
});

export const itemHoverState = (glowColor?: string, blurOpacity = 0.4) => ({
  emphasis: {
    focus: 'self' as const,
    itemStyle: glowColor ? { shadowBlur: 14, shadowColor: withAlpha(glowColor, 0.6) } : undefined,
  },
  blur: { itemStyle: { opacity: blurOpacity } },
});

export const lineHoverState = () => ({
  emphasis: { focus: 'series' as const, lineStyle: { width: 3.5 } },
  blur: { lineStyle: { opacity: 0.16 }, areaStyle: { opacity: 0.04 } },
});
