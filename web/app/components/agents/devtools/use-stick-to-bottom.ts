import { useEffect, useLayoutEffect, useRef } from 'react';

/** Within this many pixels of the end the list counts as read to the end and follows new lines. */
const NEAR_END_PX = 24;

/**
 * A log that follows its newest lines, as Chrome's console and network panels do, until the person scrolls up to read;
 * scrolling back down to the end makes it follow again. `version` changes whenever lines were added.
 */
export const useStickToBottom = <T extends HTMLElement>(version: unknown) => {
  const ref = useRef<T>(null);
  const following = useRef(true);

  useEffect(() => {
    const element = ref.current;
    if (!element) return;
    const onScroll = () => {
      following.current = element.scrollHeight - element.scrollTop - element.clientHeight <= NEAR_END_PX;
    };
    element.addEventListener('scroll', onScroll, { passive: true });
    return () => element.removeEventListener('scroll', onScroll);
  }, []);

  useLayoutEffect(() => {
    const element = ref.current;
    if (element && following.current) element.scrollTop = element.scrollHeight;
  }, [version]);

  return ref;
};
