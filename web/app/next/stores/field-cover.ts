// What opens up from the chat's field (the model's picker, the «+» menu) rises where the smart notice stands over the
// field. While any of it is open the notice gives way: it hides and lets the pointer through, or a press on a row under
// it would land on the notice and close what was open.
import { useEffect, useSyncExternalStore } from 'react';

const listeners = new Set<() => void>();
let open = 0;

const tell = () => {
  for (const fn of listeners) fn();
};

/** Marks the field covered until the returned release is called (once; a second call does nothing). */
export const coverField = (): (() => void) => {
  open += 1;
  tell();
  let released = false;
  return () => {
    if (released) return;
    released = true;
    open -= 1;
    tell();
  };
};

export const isFieldCovered = (): boolean => open > 0;

const subscribe = (fn: () => void) => {
  listeners.add(fn);
  return () => {
    listeners.delete(fn);
  };
};

/** True while something opened from the field is open. */
export const useFieldCovered = (): boolean => useSyncExternalStore(subscribe, isFieldCovered, () => false);

/** Keeps the field covered while `on`. */
export const useCoverField = (on: boolean): void => {
  useEffect(() => (on ? coverField() : undefined), [on]);
};
