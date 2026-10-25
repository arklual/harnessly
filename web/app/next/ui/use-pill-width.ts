// A pill that changes what it says eases to its new width instead of jumping: the content is measured as it is laid
// out, and the pill's width follows it through a CSS transition on --hx-pill-w.
import { useCallback, useRef } from 'react';

export const usePillWidth = () => {
  const outerRef = useRef<HTMLElement | null>(null);
  const observer = useRef<ResizeObserver | null>(null);

  const follow = (content: HTMLElement) => {
    const pill = outerRef.current;
    if (!pill) return;
    const style = getComputedStyle(pill);
    const frame =
      parseFloat(style.paddingLeft) +
      parseFloat(style.paddingRight) +
      parseFloat(style.borderLeftWidth) +
      parseFloat(style.borderRightWidth);
    pill.style.setProperty('--hx-pill-w', `${Math.ceil(content.getBoundingClientRect().width + frame)}px`);
  };

  const outer = useCallback((node: HTMLElement | null) => {
    outerRef.current = node;
  }, []);

  const inner = useCallback((node: HTMLElement | null) => {
    observer.current?.disconnect();
    observer.current = null;
    if (!node) return;
    follow(node);
    observer.current = new ResizeObserver(() => follow(node));
    observer.current.observe(node);
  }, []);

  return { outer, inner };
};
