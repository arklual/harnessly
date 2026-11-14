import { useLayoutEffect, useRef, type RefObject } from 'react';
import { AXIS, type TAxis } from '@/next/ui/use-drag';

const DURATION_MS = 280;
const EASING = 'cubic-bezier(0.16, 1, 0.3, 1)';
/** How long the renders keep quiet before the places are taken again. */
const SETTLE_MS = 400;

/**
 * Rows (or, with `axis: 'x'`, the tabs of a strip) that change place slide there instead of jumping (FLIP). Every element under `root` that carries `data-flip`
 * (an id) is measured after each render against the root's top; one whose place moved since the last render is drawn
 * from its old place back to the new one by the Web Animations API. An id is followed across a remount, so a chat that
 * goes from the loose into a group still flies there. An element inside another that moved too is left to its parent.
 * `onlyReorder`: only the render that keeps the very same ids and changes their order animates; a row that comes or goes,
 * or grows, has an opening or folding of its own, and what that moves is not a reorder.
 * `allow`: asked once a render has moved something; false places the rows without motion (a list that
 * moves only on the person's word, not when its data comes in pieces).
 *
 * a render that keeps the very same ids in the same order measures nothing (a key in the field rendered the
 * chips and measured them each time, a forced layout of the page). The places are taken again after the paint, when
 * the page is laid out anyway, so the next reorder starts from where the rows stand.
 */
const useFlip = (
  root: RefObject<HTMLElement | null>,
  onlyReorder = false,
  axis: TAxis = AXIS.y,
  allow?: () => boolean,
) => {
  const tops = useRef<ReadonlyMap<string, number>>(new Map());
  const order = useRef<string | null>(null);
  const later = useRef<number | null>(null);
  useLayoutEffect(() => {
    const el = root.current;
    if (!el) return;
    const across = axis === AXIS.x;
    /** The rows' places in the root's own content: a root that scrolls between renders moves nothing. */
    const measure = (): Map<string, number> => {
      const rootRect = el.getBoundingClientRect();
      const base = across ? rootRect.left - el.scrollLeft : rootRect.top - el.scrollTop;
      const at = new Map<string, number>();
      for (const node of el.querySelectorAll<HTMLElement>('[data-flip]')) {
        const nodeRect = node.getBoundingClientRect();
        at.set(node.dataset.flip ?? '', (across ? nodeRect.left : nodeRect.top) - base);
      }
      return at;
    };
    // The ids in their order, read without laying anything out.
    const ids = [...el.querySelectorAll<HTMLElement>('[data-flip]')]
      .map((n) => n.dataset.flip ?? '')
      .join('\u0000');
    // with `onlyReorder`, a render that brings other ids (a chat opened in the tab: its tab is another one)
    // animates nothing, so it measures nothing now either: a measure here laid out the whole page in the middle of the
    // switch (~110 ms at CPU ×4). The places are taken after the paint, as for a render that keeps the order.
    const sameSet = (): boolean => {
      if (order.current === null) return false;
      const was = order.current.split('\u0000');
      const now = ids.split('\u0000');
      const had = new Set(was);
      return was.length === now.length && now.every((id) => had.has(id));
    };
    const deferred = ids === order.current || (onlyReorder && order.current !== null && !sameSet());
    if (deferred && !el.querySelector('[data-drag-held]')) {
      order.current = ids;
      const settle = () => {
        later.current = null;
        if (root.current !== el || !el.isConnected) return;
        // A slide under way is not a place: the rows are taken once it has landed.
        const rows = [...el.querySelectorAll<HTMLElement>('[data-flip]')];
        if (rows.some((row) => typeof row.getAnimations === 'function' && row.getAnimations().length > 0))
          later.current = window.setTimeout(settle, DURATION_MS);
        else tops.current = measure();
      };
      // After the renders pause (a key every 100 ms keeps it waiting): one measure for a whole burst of typing.
      if (later.current !== null) window.clearTimeout(later.current);
      later.current = window.setTimeout(settle, SETTLE_MS);
      return;
    }
    order.current = ids;
    // A row carried by ui/use-drag is placed by it, and so are its neighbours, up to the render that brings the new
    // order: what moved then is where it was drawn already. The places are forgotten, and the next render starts anew.
    const carried = '[data-drag-held]';
    if (el.closest(carried) || el.querySelector(carried)) {
      tops.current = new Map();
      order.current = null;
      return;
    }
    const next = measure();
    const moved = new Map<HTMLElement, number>();
    for (const node of el.querySelectorAll<HTMLElement>('[data-flip]')) {
      const id = node.dataset.flip ?? '';
      const top = next.get(id) ?? 0;
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
    if (moved.size === 0) return;
    if (allow && !allow()) return;
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
  useLayoutEffect(
    () => () => {
      if (later.current !== null) window.clearTimeout(later.current);
    },
    [],
  );
};

export default useFlip;
