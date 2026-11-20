// The right column scrolls by itself (.hx-col, styles/column.css), over a long island, many widgets and the notices.
// What this reads off the scrolling box is written back onto it as attributes, no state and no render per frame:
// `data-up` / `data-down` say there is more to scroll that way (the stylesheet fades that edge only then),
// `data-scrolling` stays for a moment after a scroll (the thin scrollbar shows only then).
import { useEffect } from 'react';

const SCROLLING_MS = 900;
const EDGE_PX = 2;

export const useColumnScroll = (col: HTMLElement | null): void => {
  useEffect(() => {
    if (!col) return;
    let frame = 0;
    let timer = 0;
    const read = () => {
      frame = 0;
      const up = col.scrollTop > EDGE_PX;
      const down = col.scrollTop + col.clientHeight < col.scrollHeight - EDGE_PX;
      col.toggleAttribute('data-up', up);
      col.toggleAttribute('data-down', down);
    };
    const ask = () => {
      if (frame === 0) frame = requestAnimationFrame(read);
    };
    const onScroll = () => {
      col.setAttribute('data-scrolling', '');
      window.clearTimeout(timer);
      timer = window.setTimeout(() => col.removeAttribute('data-scrolling'), SCROLLING_MS);
      ask();
    };
    read();
    col.addEventListener('scroll', onScroll, { passive: true });
    // What the column holds grows and shrinks (a widget added, a card gone): the edges are read again.
    const observer = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(ask);
    observer?.observe(col);
    const watch = (el: Element) => observer?.observe(el);
    Array.from(col.children).forEach(watch);
    const mutations = new MutationObserver(() => {
      Array.from(col.children).forEach(watch);
      ask();
    });
    mutations.observe(col, { childList: true });
    return () => {
      col.removeEventListener('scroll', onScroll);
      observer?.disconnect();
      mutations.disconnect();
      window.clearTimeout(timer);
      if (frame !== 0) cancelAnimationFrame(frame);
    };
  }, [col]);
};
