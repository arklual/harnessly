// The phone panel's grab: what a release of the finger means. A tap toggles the tall sheet; a drag up of more than 40 px
// or a flick (v in px/ms) makes it tall; a drag down of more than 60 px or a flick down brings a tall sheet back to
// its height when it went less than 220 px, and closes it otherwise. The numbers are the board's. No React here.

export const PANEL_TAP_PX = 6;
export const PANEL_UP_PX = -40;
export const PANEL_DOWN_PX = 60;
export const PANEL_FLICK = 0.5;
export const PANEL_TALL_BACK_PX = 220;

export const PANEL_DROP = {
  tap: 'tap',
  tall: 'tall',
  normal: 'normal',
  close: 'close',
  stay: 'stay',
} as const;
export type TPanelDrop = (typeof PANEL_DROP)[keyof typeof PANEL_DROP];

/** `dy` is the finger's travel down (negative up), `ms` how long it took. */
export const panelDrop = (dy: number, ms: number, tall: boolean): TPanelDrop => {
  const v = dy / Math.max(1, ms);
  if (Math.abs(dy) < PANEL_TAP_PX) return PANEL_DROP.tap;
  if (dy < PANEL_UP_PX || v < -PANEL_FLICK) return PANEL_DROP.tall;
  if (dy > PANEL_DOWN_PX || v > PANEL_FLICK) {
    return tall && dy < PANEL_TALL_BACK_PX ? PANEL_DROP.normal : PANEL_DROP.close;
  }
  return PANEL_DROP.stay;
};

/** How far the sheet follows the finger: down freely when tall, up as far as the tall height (42 vh less the margin). */
export const panelFollow = (dy: number, tall: boolean, viewportH: number): number =>
  tall ? Math.max(0, dy) : Math.max(-(viewportH * 0.42 - 52), dy);
