import { useEffect, useRef } from 'react';

/**
 * Calls `onHit` each time the returned element scrolls into view while `active`: the lists of the organization load
 * their next part this way. `key` makes it look again after a part was added (the element may still be in view).
 */
export const useSentinel = (active: boolean, onHit: () => void, key: unknown) => {
  const ref = useRef<HTMLDivElement>(null);
  const hit = useRef(onHit);
  useEffect(() => {
    hit.current = onHit;
  });
  useEffect(() => {
    const el = ref.current;
    if (!active || !el || typeof IntersectionObserver === 'undefined') return;
    const io = new IntersectionObserver((entries) => {
      if (entries.some((e) => e.isIntersecting)) hit.current();
    });
    io.observe(el);
    return () => io.disconnect();
  }, [active, key]);
  return ref;
};
