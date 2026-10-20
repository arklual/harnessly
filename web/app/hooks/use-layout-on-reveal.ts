import { useEffect, useRef, type RefObject } from 'react';

/**
 * A resizable group measured at zero size (a preloaded frame, a hidden tab) ignores its layout and settles on an even
 * split; the layout has to be applied again once the host gets a real height.
 */
const useLayoutOnReveal = (host: RefObject<HTMLElement | null>, apply: () => void) => {
  const applyRef = useRef(apply);
  const lastHeight = useRef(0);

  useEffect(() => {
    applyRef.current = apply;
  });

  useEffect(() => {
    const element = host.current;
    if (!element) return;
    let frame = 0;
    const observer = new ResizeObserver(([entry]) => {
      const height = entry.contentRect.height;
      const revealed = height > 0 && lastHeight.current === 0;
      lastHeight.current = height;
      if (!revealed) return;
      frame = requestAnimationFrame(() => applyRef.current());
    });
    observer.observe(element);
    return () => {
      observer.disconnect();
      cancelAnimationFrame(frame);
    };
  }, [host]);
};

export default useLayoutOnReveal;
