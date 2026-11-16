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
    const px = (value: string) => parseFloat(value) || 0;
    const frame =
      px(style.paddingLeft) + px(style.paddingRight) + px(style.borderLeftWidth) + px(style.borderRightWidth);
    // The laid-out width, not getBoundingClientRect: the content enters with a scale (hx-net-swap), and a rect taken
    // mid-animation is a few pixels short, which no resize ever corrects — the button at the end was cut in half.
    const width = parseFloat(getComputedStyle(content).width) || content.offsetWidth;
    pill.style.setProperty('--hx-pill-w', `${Math.ceil(width + frame)}px`);
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
