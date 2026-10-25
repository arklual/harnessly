/** The phone layout: the sidebar is a drawer and the right pane a sheet over the chat. */
export const PHONE_QUERY = '(max-width: 900px)';
/** Movement before a gesture is read as horizontal (a drag) or vertical (a scroll that is left alone). */
export const SLOP_PX = 8;
/** A drag wins over a scroll only when it is clearly sideways. */
export const AXIS_BIAS = 1.2;

/**
 * Under the finger is something that moves sideways itself (a field, a slider, a scrolled strip) or a popover or sheet
 * over the chat: no drawer there.
 */
export const ownsSideways = (target: EventTarget | null): boolean => {
  for (
    let el = target instanceof Element ? target : null;
    el && el !== document.body;
    el = el.parentElement
  ) {
    if (
      el.matches(
        'input, textarea, [contenteditable="true"], [role="slider"], [role="dialog"], .hx-sheet-scrim, [data-nogesture]',
      )
    )
      return true;
    if (el.scrollWidth > el.clientWidth + 2) {
      const overflow = getComputedStyle(el).overflowX;
      if (overflow === 'auto' || overflow === 'scroll') return true;
    }
  }
  return false;
};

/** The pointer's speed in px/ms from its last two samples. */
export interface ITrack {
  lastX: number;
  lastT: number;
  velocity: number;
}

export const track = (state: ITrack, x: number, time: number) => {
  const dt = time - state.lastT;
  if (dt > 0) state.velocity = (x - state.lastX) / dt;
  state.lastX = x;
  state.lastT = time;
};
