import { useLayoutEffect, useRef, type RefObject } from 'react';
import { ARRIVAL_SETTLE_MS } from '@/lib/hl-pane-arrival';

/** The attribute that keeps the panel out of the flow, over the right of the conversation, while it slides in. */
export const PANE_ENTERING_ATTR = 'data-entering';

const EASE_OUT = 'cubic-bezier(0.16, 1, 0.3, 1)';
const MAX_NODES = 4000;
const MAX_DEPTH = 64;

interface IBox {
  left: number;
  width: number;
}

const boxesUnder = (root: HTMLElement): Map<Element, IBox> => {
  const boxes = new Map<Element, IBox>();
  let level: Element[] = [...root.children];
  for (let depth = 0; depth < MAX_DEPTH && level.length > 0 && boxes.size < MAX_NODES; depth++) {
    const next: Element[] = [];
    for (const node of level) {
      if (boxes.size >= MAX_NODES) break;
      const rect = node.getBoundingClientRect();
      if (rect.width === 0 && rect.height === 0) continue;
      boxes.set(node, { left: rect.left, width: rect.width });
      next.push(...node.children);
    }
    level = next;
  }
  return boxes;
};

/**
 * The outermost nodes whose start moved: each slides as a whole and its subtree follows. One that also got narrower
 * (the column of messages on a narrow window) keeps its start too; only its right end reflows at the commit.
 */
const movedNodes = (before: Map<Element, IBox>, after: Map<Element, IBox>): Array<[Element, number]> => {
  const moved: Array<[Element, number]> = [];
  const taken = new Set<Element>();
  for (const [node, was] of before) {
    const now = after.get(node);
    if (!now) continue;
    let parent = node.parentElement;
    let inside = false;
    while (parent) {
      if (taken.has(parent)) {
        inside = true;
        break;
      }
      parent = parent.parentElement;
    }
    if (inside) continue;
    const dx = now.left - was.left;
    if (Math.abs(dx) < 0.5) continue;
    taken.add(node);
    moved.push([node, dx]);
  }
  return moved;
};

/**
 * The panel comes in without the conversation being laid out again every frame, and without anything in it
 * counting as a shift: the panel stands out of the flow over the right of the conversation, what the narrower
 * conversation will move (the header's buttons, the column of messages, the right ends of rows) glides there on
 * transforms, and once the panel stops the layout is committed in the same frame the transforms are dropped.
 */
const usePaneColumnFlip = (
  shown: boolean,
  surface: HTMLElement | null,
  host: RefObject<HTMLElement | null>,
  enabled: boolean,
): void => {
  const first = useRef(true);
  useLayoutEffect(() => {
    if (first.current) {
      first.current = false;
      return;
    }
    const node = host.current;
    if (
      !shown ||
      !enabled ||
      !surface ||
      !node ||
      window.matchMedia('(prefers-reduced-motion: reduce)').matches
    ) {
      node?.removeAttribute(PANE_ENTERING_ATTR);
      return;
    }
    node.removeAttribute(PANE_ENTERING_ATTR);
    const after = boxesUnder(surface);
    node.setAttribute(PANE_ENTERING_ATTR, '');
    const before = boxesUnder(surface);
    const moved = movedNodes(before, after);
    // What the narrower header drops (the breadcrumb) dissolves as the rest slides over its place.
    const leaving = [...before.keys()].filter(
      (target) => !after.has(target) && target.parentElement !== null && after.has(target.parentElement),
    );
    const fades = leaving.map((target) =>
      target.animate([{ opacity: 1 }, { opacity: 0 }], {
        duration: ARRIVAL_SETTLE_MS / 2,
        easing: 'ease-out',
        fill: 'forwards',
      }),
    );
    const animations = moved.map(([target, dx]) =>
      target.animate([{ transform: 'none' }, { transform: `translateX(${dx}px)` }], {
        duration: ARRIVAL_SETTLE_MS,
        easing: EASE_OUT,
        fill: 'forwards',
      }),
    );
    animations.push(
      node.animate(
        [
          { transform: 'translateX(48%)', opacity: 0 },
          { opacity: 1, offset: 0.18 },
          { transform: 'none', opacity: 1 },
        ],
        { duration: ARRIVAL_SETTLE_MS, easing: EASE_OUT },
      ),
    );
    // The shift tracker does not see a transform the compositor animates: the end of each slide is handed to an inline
    // style for a frame, and only then the layout is committed with the inline transforms dropped in the same frame.
    let frame = 0;
    const commit = () => {
      node.removeAttribute(PANE_ENTERING_ATTR);
      for (const fade of fades) fade.cancel();
      for (const [target] of moved) (target as HTMLElement).style.removeProperty('transform');
    };
    const timer = window.setTimeout(() => {
      moved.forEach(([target, dx], index) => {
        (target as HTMLElement).style.transform = `translateX(${dx}px)`;
        animations[index].cancel();
      });
      frame = window.requestAnimationFrame(() => {
        frame = window.requestAnimationFrame(commit);
      });
    }, ARRIVAL_SETTLE_MS);
    return () => {
      window.clearTimeout(timer);
      window.cancelAnimationFrame(frame);
      for (const animation of animations) animation.cancel();
      commit();
    };
  }, [shown, surface, host, enabled]);
};

export default usePaneColumnFlip;
