// What a screen has not got yet (another chat's context window, its limits a moment after the change) is not drawn as
// nothing: the last value stays for a short while, and the new one takes its place when it comes — a ring goes over
// from the old share to the new, a chip changes its words, nothing vanishes and comes back. A value that stays away
// longer than `holdMs` lets the old one go.
import { useEffect, useState } from 'react';

export const HOLD_MS = 600;

interface IKept<T> {
  key: string;
  value: T;
}

/**
 * The value, or for `holdMs` after it went away the last one it was (`held` then), then null. The last one is kept
 * by `keyOf` — what it means, not its object: a screen hands a new object on every render (the pill's props), and
 * keeping each would draw the field twice for each. A kept value is redrawn only when its meaning changes.
 */
export const useHeld = <T>(
  value: T | null,
  keyOf: (value: T) => string,
  holdMs = HOLD_MS,
): { value: T | null; held: boolean } => {
  const now: IKept<T> | null = value === null ? null : { key: keyOf(value), value };
  const [kept, setKept] = useState<IKept<T> | null>(now);
  const [expired, setExpired] = useState(false);
  // State adjusted during render, not in an effect: only when the meaning changes or the value comes back.
  if (now !== null && now.key !== kept?.key) setKept(now);
  const away = value === null;
  if (!away && expired) setExpired(false);

  useEffect(() => {
    if (!away) return;
    const timer = setTimeout(() => setExpired(true), holdMs);
    return () => clearTimeout(timer);
  }, [away, holdMs]);

  if (!away) return { value, held: false };
  if (expired || kept === null) return { value: null, held: false };
  return { value: kept.value, held: true };
};
