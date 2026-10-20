// Slot numbers of a person's preview hosts: dev servers and external sites never share a slot, so a popup of an
// external site opens on its own slot origin, never one that served a dev server (its cookies, storage, service worker).
export const SLOT_COUNT = 8;
/** First and last slot of each target kind. */
export const SLOT_RANGES = Object.freeze({
  port: Object.freeze([1, 6] as const),
  url: Object.freeze([7, 8] as const),
});
export const slotFitsKind = (slot: unknown, kind: 'port' | 'url'): boolean =>
  Number.isInteger(slot) &&
  (slot as number) >= SLOT_RANGES[kind][0] &&
  (slot as number) <= SLOT_RANGES[kind][1];
