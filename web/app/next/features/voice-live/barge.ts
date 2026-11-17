// Barge-in (15.11: «я заговорил — он продолжает говорить»): the page hears the person start speaking over the
// voice and silences the voice's sound at once, without waiting for the server's turn detection. Pure: the call's frame
// loop feeds it the two loudness levels every frame.

/** The person's level that counts as speech over the voice. */
export const BARGE_LEVEL = 0.16;
/** The person's level must also stand this far over the voice's own (what the echo canceller leaves of it). */
export const ECHO_RATIO = 0.9;
/** Speech this long, not a click or a cough (a few frames' dips of a word's edge allowed, DIP_MS). */
export const BARGE_MS = 180;
/** A dip in the person's level shorter than this does not end their speech. */
export const DIP_MS = 64;
/** The voice silent this long after a barge-in: its cut answer is over, the next one may be heard. */
export const QUIET_MS = 600;
/** Never silent longer than this: the server did not stop, the person hears it again. */
export const DUCK_MAX_MS = 8000;
/** The room's own sound (a fan, a street, music, another person far off) raises the bar: speech over the voice must
 *  stand this many times over the room's level, plus FLOOR_GAP («фоновая активность его постоянно
 *  перебивает»). The level is learnt all the time no one barges: up slowly, down quicker. */
export const FLOOR_RATIO = 2.2;
export const FLOOR_GAP = 0.05;
const FLOOR_UP = 0.02;
const FLOOR_DOWN = 0.08;

export interface IBarge {
  /** Since when the person speaks over the voice; null when not. */
  over: number | null;
  /** When the person's level last stood over the bar during `over`. */
  heardAt: number;
  /** Since when the voice is silenced; null when it is heard. */
  ducked: number | null;
  /** When the voice was last loud. */
  loudAt: number;
  /** The room's level as learnt so far. */
  floor: number;
}

export const BARGE_START: IBarge = {
  over: null,
  heardAt: -Infinity,
  ducked: null,
  loudAt: -Infinity,
  floor: 0,
};

/** The person's level that counts as speech over the voice in this room. */
export const bargeBar = (s: IBarge): number => Math.max(BARGE_LEVEL, s.floor * FLOOR_RATIO + FLOOR_GAP);

const learn = (floor: number, me: number): number =>
  floor + (me - floor) * (me > floor ? FLOOR_UP : FLOOR_DOWN);

/** The next state, and whether the barge-in happened on this frame (the moment to cancel the answer). */
export const bargeStep = (
  s: IBarge,
  me: number,
  them: number,
  now: number,
  speaking: boolean,
): [IBarge, boolean] => {
  const loudAt = them > 0.04 ? now : s.loudAt;
  if (s.ducked !== null) {
    const over = now - loudAt >= QUIET_MS || now - s.ducked >= DUCK_MAX_MS;
    return [{ ...s, over: null, ducked: over ? null : s.ducked, loudAt }, false];
  }
  const loud = me >= bargeBar(s) && me >= them * ECHO_RATIO;
  if (!speaking || (!loud && (s.over === null || now - s.heardAt > DIP_MS))) {
    // Not speech over the voice: what the microphone hears now is the room (the voice's echo is learnt too, so a
    // loud speaker raises the bar as it should).
    return [{ ...s, over: null, loudAt, floor: learn(s.floor, me) }, false];
  }
  const since = s.over ?? now;
  const heardAt = loud ? now : s.heardAt;
  if (now - since >= BARGE_MS) return [{ ...s, over: null, heardAt, ducked: now, loudAt }, true];
  return [{ ...s, over: since, heardAt, loudAt }, false];
};
