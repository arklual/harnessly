// SPDX-License-Identifier: Apache-2.0
import type { VizTheme } from '../model/palette';

export interface Rgba {
  readonly r: number;
  readonly g: number;
  readonly b: number;
  readonly a: number;
}

let probe: CanvasRenderingContext2D | null | undefined;

/**
 * Любой CSS-цвет (oklch, hex, rgb) → RGBA через один пиксель. Токены ДС заданы в oklch,
 * а canvas не умеет менять прозрачность готовой строки — поэтому цвет разбирается один раз.
 */
export function resolveColor(css: string, fallback: Rgba = { r: 136, g: 136, b: 136, a: 1 }): Rgba {
  if (probe === undefined) {
    const canvas = typeof document !== 'undefined' ? document.createElement('canvas') : null;
    if (canvas) {
      canvas.width = 1;
      canvas.height = 1;
    }
    probe = canvas?.getContext('2d', { willReadFrequently: true }) ?? null;
  }
  if (!probe || !css) return fallback;
  probe.clearRect(0, 0, 1, 1);
  probe.fillStyle = '#000';
  probe.fillStyle = css;
  probe.fillRect(0, 0, 1, 1);
  const d = probe.getImageData(0, 0, 1, 1).data;
  return { r: d[0], g: d[1], b: d[2], a: d[3] / 255 };
}

export function rgba(c: Rgba, alpha = 1): string {
  return `rgba(${c.r},${c.g},${c.b},${Math.max(0, Math.min(1, c.a * alpha)).toFixed(3)})`;
}

/** Цвета холста из токенов ДС, прочитанные у элемента графика. */
export interface VizCanvasColors {
  readonly text: Rgba;
  readonly text2: Rgba;
  readonly muted: Rgba;
  readonly border: Rgba;
  readonly surface: Rgba;
  readonly input: Rgba;
  readonly raised: Rgba;
}

export function readCanvasColors(element: Element): VizCanvasColors {
  const cs = getComputedStyle(element);
  const token = (name: string, fallback: string) =>
    resolveColor(cs.getPropertyValue(name).trim() || fallback);
  return {
    text: token('--t-text', '#f5f5f5'),
    text2: token('--t-text-secondary', '#8a8a8a'),
    muted: token('--t-text-muted', '#666'),
    border: token('--t-border-subtle', 'rgba(255,255,255,.08)'),
    surface: token('--t-surface', '#1c1c1c'),
    input: token('--t-surface-input', '#262626'),
    raised: token('--t-surface-raised', '#2a2a2a'),
  };
}

/** Атрибуты `<html>`, по которым читается тема: за ними следят и холст, и хуки. */
export const VIZ_THEME_ATTRIBUTES = ['class', 'data-theme', 'data-hx-theme'] as const;

/**
 * Тема документа: `data-theme` или `data-hx-theme` со значением `light`/`dark`, либо класс
 * `light`/`dark`. Без явной отметки — светлая, как и токены в `tokens.css`.
 */
export function documentTheme(): VizTheme {
  if (typeof document === 'undefined') return 'light';
  const root = document.documentElement;
  const mark = root.dataset.theme ?? root.dataset.hxTheme;
  if (mark === 'dark' || (!mark && root.classList.contains('dark'))) return 'dark';
  return 'light';
}

/** Семейство шрифта интерфейса для подписей на холсте. */
export function canvasFont(element: Element): string {
  return (
    getComputedStyle(element).getPropertyValue('--t-font-family-ui').trim() || 'Inter, system-ui, sans-serif'
  );
}
