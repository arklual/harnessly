/**
 * Whether a message came from the board's frame. The owner's board has the slot's origin. A guest's board is served
 * under a CSP sandbox and has an opaque origin: its messages say `null`, and only the frame's own window with exactly
 * that origin is believed (`*` is how the host addresses it, never how a message names itself).
 */
export const GUEST_ORIGIN = '*';
const OPAQUE = 'null';

export const fromFrame = (
  event: { source: unknown; origin: string },
  frame: unknown,
  origin: string,
): boolean => {
  if (frame === null || frame === undefined || event.source !== frame) return false;
  return event.origin === (origin === GUEST_ORIGIN ? OPAQUE : origin);
};
