import { useSyncExternalStore } from 'react';

/** Narrow screens get the phone layout. One shared MediaQueryList per width, its value updated by its own events. */
const lists = new Map<number, { value: boolean; listeners: Set<() => void> }>();

const watch = (width: number) => {
  let entry = lists.get(width);
  if (entry) return entry;
  const list = window.matchMedia(`(max-width: ${width - 1}px)`);
  const created = { value: list.matches, listeners: new Set<() => void>() };
  list.addEventListener('change', (event) => {
    created.value = event.matches;
    created.listeners.forEach((fn) => fn());
  });
  lists.set(width, created);
  entry = created;
  return entry;
};

export default function useIsMobile(width = 768): boolean {
  return useSyncExternalStore(
    (fn) => {
      if (typeof window === 'undefined') return () => {};
      const entry = watch(width);
      entry.listeners.add(fn);
      return () => entry.listeners.delete(fn);
    },
    () => (typeof window === 'undefined' ? false : watch(width).value),
    () => false,
  );
}
