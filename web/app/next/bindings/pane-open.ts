import { useSyncExternalStore } from 'react';

/**
 * Whether the open chat's right pane is out. The pane's own state lives in right-pane.tsx; it is mirrored here so that
 * what has to move aside for it (the column of the goal and the background work, the header) can read it without being
 * handed the pane through the shell.
 */
const listeners = new Set<() => void>();
let open = false;

/** Written by the pane's hook only. */
export const setPaneOpen = (next: boolean) => {
  if (open === next) return;
  open = next;
  for (const listener of [...listeners]) listener();
};

const subscribe = (listener: () => void) => {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
};

export const usePaneOpen = (): boolean =>
  useSyncExternalStore(
    subscribe,
    () => open,
    () => false,
  );
