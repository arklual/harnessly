// A finger held on the slide leaves a comment at that place (phone, edit and link): the ring shows after 250 ms and fills
// in 480 ms; at 730 ms the comment opens. Moving more than 10 px cancels; a lift before is a tap. The clock is injected
// so the numbers can be tested. No React here.

export const PRESS_RING_MS = 250;
export const PRESS_FIRE_MS = 730;
export const PRESS_SLOP_PX = 10;
export const PRESS_VIBRATE_MS = 12;

export interface IPressPoint {
  /** Percent of the slide. */
  px: number;
  py: number;
}

export interface IPressClock {
  setTimeout: (fn: () => void, ms: number) => unknown;
  clearTimeout: (id: unknown) => void;
}

export interface IPressHandlers {
  /** The ring is shown at the point; null takes it away. */
  onRing: (point: IPressPoint | null) => void;
  /** 730 ms held. */
  onFire: (point: IPressPoint) => void;
  /** A lift before the ring filled; `text` says the finger was on editable text (the edit sheet's tap). */
  onTap: (text: boolean) => void;
}

export interface IPress {
  /** `x`, `y` are screen px of the finger (for the slop), `point` the place in percent of the slide. */
  down: (x: number, y: number, point: IPressPoint, text: boolean) => void;
  move: (x: number, y: number) => void;
  up: () => void;
  cancel: () => void;
}

export const createPress = (handlers: IPressHandlers, clock: IPressClock): IPress => {
  let at: { x: number; y: number; point: IPressPoint; text: boolean } | null = null;
  let ring: unknown = null;
  let fire: unknown = null;
  let shown = false;

  const stop = () => {
    if (ring !== null) clock.clearTimeout(ring);
    if (fire !== null) clock.clearTimeout(fire);
    ring = null;
    fire = null;
    if (shown) {
      shown = false;
      handlers.onRing(null);
    }
  };

  return {
    down: (x, y, point, text) => {
      stop();
      at = { x, y, point, text };
      ring = clock.setTimeout(() => {
        if (!at) return;
        shown = true;
        handlers.onRing(at.point);
      }, PRESS_RING_MS);
      fire = clock.setTimeout(() => {
        const held = at;
        at = null;
        stop();
        if (held) handlers.onFire(held.point);
      }, PRESS_FIRE_MS);
    },
    move: (x, y) => {
      if (!at) return;
      if (Math.hypot(x - at.x, y - at.y) > PRESS_SLOP_PX) {
        at = null;
        stop();
      }
    },
    up: () => {
      const tap = at;
      at = null;
      stop();
      if (tap) handlers.onTap(tap.text);
    },
    cancel: () => {
      at = null;
      stop();
    },
  };
};
