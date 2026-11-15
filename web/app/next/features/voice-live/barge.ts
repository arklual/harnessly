// Barge-in (15.11: «я заговорил — он продолжает говорить»): the page hears the person start speaking over the
// voice and silences the voice's sound at once, without waiting for the server's turn detection. Pure: the call's frame
// loop feeds it the two loudness levels every frame.

/** The person's level that counts as speech over the voice. */
export const BARGE_LEVEL = 0.16;
/** The person's level must also stand this far over the voice's own (what the echo canceller leaves of it). */
export const ECHO_RATIO = 0.9;
/** Speech this long, not a click or a cough. */
export const BARGE_MS = 150;
/** The voice silent this long after a barge-in: its cut answer is over, the next one may be heard. */
export const QUIET_MS = 600;
/** Never silent longer than this: the server did not stop, the person hears it again. */
export const DUCK_MAX_MS = 8000;

export interface IBarge {
  /** Since when the person speaks over the voice; null when not. */
  over: number | null;
  /** Since when the voice is silenced; null when it is heard. */
  ducked: number | null;
  /** When the voice was last loud. */
  loudAt: number;
}

export const BARGE_START: IBarge = { over: null, ducked: null, loudAt: -Infinity };

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
    return [{ over: null, ducked: over ? null : s.ducked, loudAt }, false];
  }
  const speech = speaking && me >= BARGE_LEVEL && me >= them * ECHO_RATIO;
  if (!speech) return [{ ...s, over: null, loudAt }, false];
  const since = s.over ?? now;
  if (now - since >= BARGE_MS) return [{ over: null, ducked: now, loudAt }, true];
  return [{ ...s, over: since, loudAt }, false];
};
