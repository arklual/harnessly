// How a board's setting is drawn in the site's «Живые настройки» panel, and what may be sent back to the page for it. The
// page names an `editor` for each setting; the panel turns that, the options and the bounds into one of six controls.
import type { ITweak } from '@/next/features/designly/page-bridge';

export const TWEAK_KIND = {
  /** Few short options side by side. */
  seg: 'seg',
  /** Many or long options in a list. */
  select: 'select',
  /** Options that are all colours. */
  colors: 'colors',
  toggle: 'toggle',
  range: 'range',
  number: 'number',
  text: 'text',
} as const;
export type TTweakKind = (typeof TWEAK_KIND)[keyof typeof TWEAK_KIND];

const SEG_MAX = 4;
/** Together the labels of a row of segments must fit one line of the panel. */
const SEG_CHARS = 30;
const HEX = /^#[0-9a-f]{3}(?:[0-9a-f]{3})?$/i;

export const tweakKind = (c: ITweak): TTweakKind => {
  if (c.options.length > 0) {
    if (c.options.every((o) => typeof o.value === 'string' && HEX.test(o.value))) return TWEAK_KIND.colors;
    const chars = c.options.reduce((sum, o) => sum + o.text.length, 0);
    return c.options.length <= SEG_MAX && chars <= SEG_CHARS ? TWEAK_KIND.seg : TWEAK_KIND.select;
  }
  if (c.editor === 'boolean' || typeof c.value === 'boolean') return TWEAK_KIND.toggle;
  if (c.editor === 'number' || typeof c.value === 'number') {
    return c.min !== null && c.max !== null && c.max > c.min ? TWEAK_KIND.range : TWEAK_KIND.number;
  }
  return TWEAK_KIND.text;
};

/** The text of the option the setting stands at; the value itself when the board named no such option. */
export const tweakValueText = (c: ITweak) => {
  const at = c.options.find((o) => o.value === c.value);
  if (at) return at.text;
  return c.value === null ? '' : String(c.value);
};

/** The settings in the order the board gave them, grouped by the section each stands under (a section once). */
export const tweakGroups = (controls: readonly ITweak[]) => {
  const groups: { section: string; items: ITweak[] }[] = [];
  for (const c of controls) {
    const group = groups.find((g) => g.section === c.section);
    if (group) group.items.push(c);
    else groups.push({ section: c.section, items: [c] });
  }
  return groups;
};

/** How far along its bounds a slider stands, as a percentage. */
export const rangeShare = (value: number, min: number, max: number) =>
  `${Math.min(100, Math.max(0, ((value - min) / (max - min)) * 100)).toFixed(1)}%`;
