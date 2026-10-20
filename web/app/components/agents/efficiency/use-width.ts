import { useCallback, useState } from 'react';

/** Width of an element, followed live: charts are drawn 1:1 in its pixels so text and dots never stretch. */
const useWidth = (initial = 640): [(el: HTMLElement | null) => (() => void) | void, number] => {
  const [width, setWidth] = useState(initial);
  const ref = useCallback((el: HTMLElement | null) => {
    if (!el) return;
    setWidth(Math.max(240, Math.round(el.getBoundingClientRect().width)));
    const observer = new ResizeObserver(() =>
      setWidth(Math.max(240, Math.round(el.getBoundingClientRect().width))),
    );
    observer.observe(el);
    return () => observer.disconnect();
  }, []);
  return [ref, width];
};

export default useWidth;
