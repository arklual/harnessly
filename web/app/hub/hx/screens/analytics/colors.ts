// A team keeps one colour on every card of the screen: the lines of the big chart, the swatches of the peer table.
// The first three teams of the tenant take the palette of the chart library in the order of its slots (violet, teal,
// amber), everything else — the people without a team and any further team — is grey.
import type { VizColorSlot } from '@viz';

/** The chart library's slots for the first three teams: its violet, teal and amber. */
const SLOTS: readonly VizColorSlot[] = [0, 2, 1];
const HEX = ['#8968f4', '#25a897', '#c8830b'] as const;
export const OTHER_HEX = '#7c7c86';

export const WASTE_HEX = { violet: '#8968f4', rose: '#e84461', amber: '#c8830b', teal: '#25a897' } as const;

export const teamSlot = (order: readonly string[], name: string): VizColorSlot => {
  const i = order.indexOf(name);
  return SLOTS[i] ?? 'other';
};

export const teamHex = (order: readonly string[], name: string): string =>
  HEX[order.indexOf(name)] ?? OTHER_HEX;
