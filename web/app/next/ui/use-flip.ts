import { useLayoutEffect, useRef, type RefObject } from 'react';
import { AXIS, type TAxis } from '@/next/ui/use-drag';

const DURATION_MS = 280;
const EASING = 'cubic-bezier(0.16, 1, 0.3, 1)';

/**
 * Rows (or, with `axis: 'x'`, the tabs of a strip) that change place slide there instead of jumping (FLIP). Every element under `root` that carries `data-flip`
 * (an id) is measured after each render against the root's top; one whose place moved since the last render is drawn
 * from its old place back to the new one by the Web Animations API. An id is followed across a remount, so a chat that
 * goes from the loose into a group still flies there. An element inside another that moved too is left to its parent.
 * `onlyReorder`: only the render that keeps the very same ids and changes their order animates; a row that comes or goes,
 * or grows, has an opening or folding of its own, and what that moves is not a reorder.
 */
const useFlip = (root: RefObject<HTMLElement | null>, onlyReorder = false, axis: TAxis = AXIS.y) => {
  const tops = useRef<ReadonlyMap<string, number>>(new Map());
  useLayoutEffect(() => {
    const el = root.current;
    if (!el) return;
    // A row carried by ui/use-drag is placed by it, and so are its neighbours, up to the render that brings the new
    // order: what moved then is where it was drawn already. The places are forgotten, and the next render starts anew.
    const carried = '[data-drag-held]';
    if (el.closest(carried) || el.querySelector(carried)) {
      tops.current = new Map();
      return;
    }
    // Places are measured in the root's own content, so a root that scrolls between renders moves nothing.
    const across = axis === AXIS.x;
    const rootRect = el.getBoundingClientRect();
    const base = across ? rootRect.left - el.scrollLeft : rootRect.top - el.scrollTop;
    const next = new Map<string, number>();
    const moved = new Map<HTMLElement, number>();
    for (const node of el.querySelectorAll<HTMLElement>('[data-flip]')) {
      const id = node.dataset.flip ?? '';
      const nodeRect = node.getBoundingClientRect();
      const top = (across ? nodeRect.left : nodeRect.top) - base;
      next.set(id, top);
      const was = tops.current.get(id);
      if (was !== undefined && Math.abs(was - top) > 1) moved.set(node, was - top);
    }
    const before = tops.current;
    tops.current = next;
    if (onlyReorder) {
      const was = [...before.keys()];
      const now = [...next.keys()];
      const sameIds = was.length === now.length && now.every((id) => before.has(id));
      if (!sameIds || was.every((id, i) => id === now[i])) return;
    }
    if (typeof Element.prototype.animate !== 'function') return;
    if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) return;
    for (const [node, delta] of moved) {
      const parent = node.parentElement?.closest<HTMLElement>('[data-flip]');
      if (parent && moved.has(parent)) continue;
      const shift = across ? 'translateX' : 'translateY';
      node.animate([{ transform: `${shift}(${delta}px)` }, { transform: `${shift}(0)` }], {
        duration: DURATION_MS,
        easing: EASING,
      });
    }
  });
};

export default useFlip;
