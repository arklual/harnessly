// A note's pin stays on its block: the page's bridge follows the pinned blocks (`tk:anchors`) and tells, on every scroll,
// resize or change, where each shows (`tk:anchored`): the whole box, what its scrollers leave, what is on screen and
// whether the page covers it. The view keeps the last word in a store; a pin listens to it and writes its place on its
// own element in the same frame (no render per scroll). A page whose bridge does not tell keeps the box of the pick.
import { createContext, useContext, useLayoutEffect, type RefObject } from 'react';
import type { IAnchorBox, IAnchorGeo, IAnchorReport } from '@/next/features/designly/page-bridge';

/** The key of the block a new note is being written for. */
export const DRAFT_ANCHOR = '~draft';

/** A block to follow, as the bridge reads it. */
export interface IAnchorItem {
  key: string;
  pick: number | null;
  selector: string;
}

interface IAnchored {
  id: string;
  picked: { id: number | null; selector: string };
}

/** The blocks of the notes on the page and of the card being written, for the bridge. */
export const anchorItems = (
  notes: readonly IAnchored[],
  draft: { id?: string; picked: IAnchored['picked'] } | null,
): IAnchorItem[] => {
  const items = notes.map((note) => ({ key: note.id, pick: note.picked.id, selector: note.picked.selector }));
  if (draft && draft.id === undefined)
    items.push({ key: DRAFT_ANCHOR, pick: draft.picked.id, selector: draft.picked.selector });
  return items;
};

/** The last word of the page; null before it has said anything (or after it went away). */
export interface IAnchorStore {
  get: () => IAnchorReport | null;
  set: (report: IAnchorReport | null) => void;
  subscribe: (listener: () => void) => () => void;
}

export const createAnchorStore = (): IAnchorStore => {
  let report: IAnchorReport | null = null;
  const listeners = new Set<() => void>();
  return {
    get: () => report,
    set: (next) => {
      report = next;
      for (const listener of listeners) listener();
    },
    subscribe: (listener) => {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
  };
};

export const AnchorContext = createContext<IAnchorStore | null>(null);

export const PIN_PLACE = { shown: 'shown', hidden: 'hidden' } as const;
export type TPinPlace = (typeof PIN_PLACE)[keyof typeof PIN_PLACE];

/** How far a cut side's frame is taken in: an uncut side keeps its outline (drawn outside the box). */
const OPEN_SIDE = -6;
/** The number stays this far inside the viewport, so it is whole. */
const EDGE = 14;

/** Where a pin is drawn, in the page's pixels: the whole box, how much of each side is cut off, its number's spot
 *  (from the box's corner). */
export interface IPinAt {
  place: TPinPlace;
  box: IAnchorBox;
  cut: { t: number; r: number; b: number; l: number };
  mark: { x: number; y: number };
}

const UNCUT = { t: OPEN_SIDE, r: OPEN_SIDE, b: OPEN_SIDE, l: OPEN_SIDE } as const;

const clamp = (v: number, min: number, max: number) => Math.max(min, Math.min(Math.max(min, max), v));

/** The box of the pick, from a page that does not follow its blocks. */
export const fixedPin = (rect: { x: number; y: number; width: number; height: number }): IPinAt => ({
  place: PIN_PLACE.shown,
  box: { x: rect.x, y: rect.y, w: rect.width, h: rect.height },
  cut: UNCUT,
  mark: { x: 0, y: 0 },
});

/**
 * The pin of a followed block: the frame is cut to what is on screen and the number stands at its top left corner (kept
 * whole inside the viewport). Hidden when the page has no such block, a scroller or the viewport hides it, or the page
 * lays something over it.
 */
export const placePin = (geo: IAnchorGeo, vw: number, vh: number): IPinAt => {
  const { box, shown } = geo;
  if (!box || !shown || geo.covered)
    return {
      place: PIN_PLACE.hidden,
      box: box ?? { x: 0, y: 0, w: 0, h: 0 },
      cut: UNCUT,
      mark: { x: 0, y: 0 },
    };
  const side = (cut: number) => (cut > 0.5 ? cut : OPEN_SIDE);
  return {
    place: PIN_PLACE.shown,
    box,
    cut: {
      t: side(shown.y - box.y),
      r: side(box.x + box.w - (shown.x + shown.w)),
      b: side(box.y + box.h - (shown.y + shown.h)),
      l: side(shown.x - box.x),
    },
    mark: {
      x: clamp(shown.x, Math.min(EDGE, shown.x + shown.w), vw - EDGE) - box.x,
      y: clamp(shown.y, Math.min(EDGE, shown.y + shown.h), vh - EDGE) - box.y,
    },
  };
};

const px = (v: number) => `${Math.round(v * 100) / 100}px`;

/** The place on the pin's element: its box, the frame's cut and the number's spot go to its CSS. */
export const writePin = (el: HTMLElement, at: IPinAt) => {
  el.dataset.place = at.place;
  el.style.setProperty('--x', px(at.box.x));
  el.style.setProperty('--y', px(at.box.y));
  el.style.setProperty('--w', px(at.box.w));
  el.style.setProperty('--h', px(at.box.h));
  el.style.setProperty('--ct', px(at.cut.t));
  el.style.setProperty('--cr', px(at.cut.r));
  el.style.setProperty('--cb', px(at.cut.b));
  el.style.setProperty('--cl', px(at.cut.l));
  el.style.setProperty('--mkx', px(at.mark.x));
  el.style.setProperty('--mky', px(at.mark.y));
};

/** Keeps a pin's element on its block: from the page's word when it gives one, else at the box of the pick. */
export const useNoteAnchor = (
  ref: RefObject<HTMLElement | null>,
  key: string,
  rect: { x: number; y: number; width: number; height: number } | null,
) => {
  const store = useContext(AnchorContext);
  const x = rect?.x;
  const y = rect?.y;
  const width = rect?.width;
  const height = rect?.height;
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el || x === undefined || y === undefined || width === undefined || height === undefined) return;
    const draw = () => {
      const report = store?.get() ?? null;
      const geo = report?.items.get(key);
      writePin(el, geo && report ? placePin(geo, report.vw, report.vh) : fixedPin({ x, y, width, height }));
    };
    draw();
    return store?.subscribe(draw);
  }, [ref, store, key, x, y, width, height]);
};
