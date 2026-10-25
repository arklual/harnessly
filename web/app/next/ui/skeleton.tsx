// A placeholder bar while something loads. It appears only after 150 ms (spec §6): a load that ends sooner shows
// nothing, so a fast answer does not flash.
import { useEffect, useState, type ReactNode } from 'react';

export const SKELETON_DELAY_MS = 150;

/** True once `on` has held for `delayMs`. */
export const useDelayed = (on: boolean, delayMs = SKELETON_DELAY_MS): boolean => {
  const [held, setHeld] = useState(false);
  useEffect(() => {
    if (!on) return;
    const timer = setTimeout(() => setHeld(true), delayMs);
    return () => {
      clearTimeout(timer);
      setHeld(false);
    };
  }, [on, delayMs]);
  return on && held;
};

export const Skeleton = ({ className }: { className?: string }) => (
  <span className={className ? `hx-sk ${className}` : 'hx-sk'} aria-hidden="true" />
);

/** Draws its children (skeleton bars) only after the delay. */
export const Delayed = ({ children, delayMs }: { children: ReactNode; delayMs?: number }) =>
  useDelayed(true, delayMs) ? <>{children}</> : null;
