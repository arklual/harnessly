import { useEffect, useLayoutEffect, useRef, useState, type RefObject } from 'react';
import { clampTo, shiftsOf, slotOf, type ISpan } from '@/next/ui/drag-slots';

// A drag by hand (spec §6: no drag-and-drop library), for the sidebar's chats, the strip's tabs and the queue's
// messages. The elements say what they are in attributes: `data-drag="<kind>"` + `data-drag-id` are what can be picked
// up (`data-drag-box="parent"`: the parent is what moves, `"drop"`: the nearest `data-drop` around it); `data-drop="<kind>"`
// + `data-drop-id` are where it can go; the owner names the kinds it knows.
//
// The very element that is picked up moves with the pointer, by a transform written once a frame straight to the DOM,
// along the list's axis only, kept inside the list and held at the point it was taken by. Its neighbours that are places
// of the same list (`lined`) step aside by a transform of their own, eased by CSS (ui.css), only when the place it would
// take changes; a place elsewhere (a group, a project) lights up instead (`data-drop-over`, `data-drop-edge`). On the
// drop the element settles into its place, then the owner hears of it; the transforms go in the very render that brings
// the new order, so nothing jumps. Escape, a lost pointer or a drop nowhere eases it back. React hears of the drag twice,
// when it starts and when it ends: nothing renders while it goes.

/** Which half of a row the pointer is over: the drop goes before the row or after it. */
export const EDGE = { before: 'before', after: 'after' } as const;
export type TEdge = (typeof EDGE)[keyof typeof EDGE];

export const AXIS = { x: 'x', y: 'y' } as const;
export type TAxis = (typeof AXIS)[keyof typeof AXIS];

export interface IDragSource<K extends string = string> {
  kind: K;
  id: string;
}

export interface IDropTarget<K extends string = string> {
  kind: K;
  /** What is under the pointer; absent for a place that is one as a whole (the favorites, the recent chats). */
  id?: string;
  edge?: TEdge;
}

/** A mouse picks a row up after this many pixels; a finger after it rests this long (a swipe is the list's scroll). */
const MOUSE_SLOP = 5;
const TOUCH_SLOP = 8;
const HOLD_MS = 380;
/** The list scrolls while the pointer is within this of its top or bottom edge. */
const SCROLL_ZONE = 44;
const SCROLL_STEP = 14;
/** The let-go element eases into its place, or back to its own, this long: --hx-drag-ms in ui.css. */
export const SETTLE_MS = 180;
/** After the owner has had the drop (and its promise, when it gives one, has settled), the new order has this long to come. */
export const COMMIT_WAIT_MS = 1500;

/** The longest wait for the click a touch ends with, before what follows a hold goes on without it. */
const TOUCH_CLICK_MS = 450;

/**
 * Runs `then` after a finger that held a row has let go. The release is followed by the mouse events a browser makes of a
 * touch, and a menu opened at once would take their press for one outside it: it opens after the click that ends them
 * (or, when none comes, a moment later).
 */
export const afterTouchClick = (then: () => void) => {
  let timer = 0;
  const run = () => {
    window.removeEventListener('click', run, true);
    window.clearTimeout(timer);
    then();
  };
  window.addEventListener('click', run, { capture: true, once: true });
  timer = window.setTimeout(run, TOUCH_CLICK_MS);
};

const kindOf = <K extends string>(kinds: readonly K[], value: string | undefined): K | null =>
  kinds.find((kind) => kind === value) ?? null;

export interface IUseDrag {
  /** Something is being carried (or settling): the owner keeps its hints and menus closed meanwhile. */
  dragging: boolean;
}

interface IOptions<TDrag extends string, TDrop extends string, TPlan> {
  /** The element that holds the rows; it scrolls while the pointer is at its edge, and the carried row stays inside it. */
  listRef: RefObject<HTMLElement | null>;
  dragKinds: readonly TDrag[];
  dropKinds: readonly TDrop[];
  /** The way the rows run: down the page (default) or along it. */
  axis?: TAxis;
  /**
   * Whether a drop here is a place between rows and not the row as a whole. The neighbours of the carried row that are
   * such places step aside for it; elsewhere such a place is drawn as a line at the row's edge.
   */
  lined?: (source: IDragSource<TDrag>, target: IDropTarget<TDrop>) => boolean;
  /** What dropping `source` on `target` asks for; null: nothing, the place is not taken. */
  resolve: (source: IDragSource<TDrag>, target: IDropTarget<TDrop>) => TPlan | null;
  /** The drop; a promise is waited for before the element, if the order has not changed, goes back to its place. */
  onDrop: (plan: TPlan) => unknown;
  /**
   * A place outside the list that takes what is carried (`selector` finds it under the pointer): it lights up like a
   * place of the list, and a drop on it hands the plan to the owner while the row eases back to where it stood.
   */
  external?: { selector: string; resolve: (source: IDragSource<TDrag>) => TPlan | null };
  /** A finger that held a row and let go where it was, without carrying it: the place of the release. */
  onHoldRelease?: (source: IDragSource<TDrag>, point: { x: number; y: number }) => void;
}

/** The neighbours the carried row moves among: the places of its own list, measured when it was picked up. */
interface ISlots<TDrop extends string> {
  items: HTMLElement[];
  targets: IDropTarget<TDrop>[];
  spans: ISpan[];
  from: number;
  to: number;
}

interface ITracking<TDrag extends string, TDrop extends string, TPlan> {
  pointerId: number;
  pointerType: string;
  x0: number;
  y0: number;
  x: number;
  y: number;
  source: IDragSource<TDrag>;
  /** What moves: the row the pointer took, or the box around it (`data-drag-box`). */
  mover: HTMLElement;
  active: boolean;
  /** The pointer went farther than a finger's wobble since it was taken. */
  moved: boolean;
  hold: number | undefined;
  plan: TPlan | null;
  /** The plan is for the place outside the list. */
  outside: boolean;
  box: HTMLElement | null;
  scroll: number;
  frame: number | undefined;
  // Measured when it is picked up, in the window along the axis.
  listStart: number;
  listEnd: number;
  scroll0: number;
  start0: number;
  size: number;
  /** Where along the row the pointer took it. */
  grab: number;
  offset: number;
  slots: ISlots<TDrop> | null;
  /** The place the row stands in already: a drop on it as a whole is a move among the neighbours. */
  home: HTMLElement | null;
  hosts: HTMLElement[];
}

/** A drop made, waiting for the owner's new order: the row stands in its new place meanwhile. */
interface IHeld {
  mover: HTMLElement;
  parent: HTMLElement | null;
  index: number;
  release: () => void;
}

const clear = (box: HTMLElement | null) => {
  box?.removeAttribute('data-drop-over');
  box?.removeAttribute('data-drop-edge');
};

const reducedMotion = () =>
  typeof window.matchMedia === 'function' && window.matchMedia('(prefers-reduced-motion: reduce)').matches;

const indexIn = (el: HTMLElement) => (el.parentElement ? [...el.parentElement.children].indexOf(el) : -1);

const moverOf = (el: HTMLElement): HTMLElement => {
  if (el.dataset.dragBox === 'parent') return el.parentElement ?? el;
  if (el.dataset.dragBox === 'drop') return el.closest<HTMLElement>('[data-drop]') ?? el;
  return el;
};

/** A row's place as a target: the row itself, or the head inside it that stands for it (`data-drop-box="parent"`). */
const dropOf = (row: Element): HTMLElement | null =>
  row.matches('[data-drop]')
    ? (row as HTMLElement)
    : row.querySelector<HTMLElement>(':scope > [data-drop][data-drop-box="parent"]');

/** Picks rows of the list up with the pointer and drops them where `resolve` says they go. */
const useDrag = <TDrag extends string, TDrop extends string, TPlan>({
  listRef,
  dragKinds,
  dropKinds,
  axis = AXIS.y,
  lined,
  resolve,
  onDrop,
  onHoldRelease,
  external,
}: IOptions<TDrag, TDrop, TPlan>): IUseDrag => {
  const [dragging, setDragging] = useState(false);
  const held = useRef<IHeld | null>(null);
  const latest = useRef({ resolve, onDrop, onHoldRelease, lined, dragKinds, dropKinds, external });
  useEffect(() => {
    latest.current = { resolve, onDrop, onHoldRelease, lined, dragKinds, dropKinds, external };
  });

  // The render that brings the new order lets the dropped row go, before it is painted: it is in that place already.
  useLayoutEffect(() => {
    const h = held.current;
    if (!h) return;
    if (!h.mover.isConnected || h.mover.parentElement !== h.parent || indexIn(h.mover) !== h.index)
      h.release();
  });

  useEffect(() => {
    const list = listRef.current;
    if (!list) return;
    let track: ITracking<TDrag, TDrop, TPlan> | null = null;
    /** The drag that is settling or waiting for its order: a new one waits for it to end. */
    let busy = false;
    let timer: number | undefined;
    /** The last drag picked up, for the list that goes while it settles. */
    let last: ITracking<TDrag, TDrop, TPlan> | null = null;

    const across = axis === AXIS.x;
    const start = (rect: DOMRect) => (across ? rect.left : rect.top);
    const end = (rect: DOMRect) => (across ? rect.right : rect.bottom);
    const along = (x: number, y: number) => (across ? x : y);
    const scrollOf = () => (across ? list.scrollLeft : list.scrollTop);
    const shift = (px: number) => {
      if (px === 0) return '';
      return across ? `translate3d(${px}px, 0, 0)` : `translate3d(0, ${px}px, 0)`;
    };

    const slotsOf = (t: ITracking<TDrag, TDrop, TPlan>): ISlots<TDrop> | null => {
      const parent = t.mover.parentElement;
      const isLined = latest.current.lined;
      if (!parent || !isLined) return null;
      const items: HTMLElement[] = [];
      const targets: IDropTarget<TDrop>[] = [];
      for (const row of parent.children) {
        const drop = dropOf(row);
        const kind = kindOf(latest.current.dropKinds, drop?.dataset.drop);
        if (!drop || !kind) continue;
        const target: IDropTarget<TDrop> = { kind, id: drop.dataset.dropId };
        if (!isLined(t.source, target)) continue;
        items.push(row as HTMLElement);
        targets.push(target);
      }
      const from = items.indexOf(t.mover);
      if (from < 0 || items.length < 2) return null;
      const base = t.listStart - t.scroll0;
      const spans = items.map((item) => {
        const rect = item.getBoundingClientRect();
        return { start: start(rect) - base, end: end(rect) - base };
      });
      return { items, targets, spans, from, to: from };
    };

    const homeOf = (mover: HTMLElement): HTMLElement | null => {
      for (let at = mover.parentElement; at && at !== list; at = at.parentElement) {
        if (at.matches('[data-drop]:not([data-drop-box="parent"])')) return at;
        if (at.querySelector(':scope > [data-drop][data-drop-box="parent"]')) return at;
      }
      return null;
    };

    const moveSlots = (t: ITracking<TDrag, TDrop, TPlan>, to: number) => {
      const s = t.slots;
      if (!s || s.to === to) return;
      s.to = to;
      const offsets = shiftsOf(s.spans, s.from, to);
      s.items.forEach((item, j) => {
        if (j !== s.from) item.style.transform = shift(offsets[j] ?? 0);
      });
    };

    const light = (t: ITracking<TDrag, TDrop, TPlan>, box: HTMLElement | null, line: TEdge | null) => {
      if (box !== t.box) clear(t.box);
      t.box = box;
      if (!box) return;
      box.setAttribute('data-drop-over', 'true');
      if (line) box.setAttribute('data-drop-edge', line);
      else box.removeAttribute('data-drop-edge');
    };

    // A place elsewhere in the list under the pointer: the innermost one, by the boxes as they stand.
    const elsewhere = (t: ITracking<TDrag, TDrop, TPlan>) => {
      let found: { box: HTMLElement; plan: TPlan; line: TEdge | null } | null = null;
      for (const el of list.querySelectorAll<HTMLElement>('[data-drop]')) {
        if (t.mover.contains(el) || t.slots?.items.some((item) => item.contains(el))) continue;
        const kind = kindOf(latest.current.dropKinds, el.dataset.drop);
        const rect = el.getBoundingClientRect();
        const inside = t.x >= rect.left && t.x <= rect.right && t.y >= rect.top && t.y <= rect.bottom;
        if (!kind || !inside) continue;
        const box = el.dataset.dropBox === 'parent' ? el.parentElement : el;
        if (!box || box === t.home) {
          found = null;
          continue;
        }
        const before = across ? t.x < rect.left + rect.width / 2 : t.y < rect.top + rect.height / 2;
        const edge = before ? EDGE.before : EDGE.after;
        const target: IDropTarget<TDrop> = { kind, id: el.dataset.dropId, edge };
        const plan = latest.current.resolve(t.source, target);
        found = plan ? { box, plan, line: latest.current.lined?.(t.source, target) ? edge : null } : null;
      }
      return found;
    };

    // The outside place under the pointer, if the owner has one for what is carried.
    const outside = (t: ITracking<TDrag, TDrop, TPlan>) => {
      const spec = latest.current.external;
      if (!spec) return null;
      const box = document.elementFromPoint(t.x, t.y)?.closest<HTMLElement>(spec.selector);
      if (!box || list.contains(box)) return null;
      const plan = spec.resolve(t.source);
      return plan ? { box, plan } : null;
    };

    const hit = (t: ITracking<TDrag, TDrop, TPlan>, pointer: number, center: number) => {
      const s = t.slots;
      const far = outside(t);
      t.outside = far !== null;
      if (far) {
        if (s) moveSlots(t, s.from);
        light(t, far.box, null);
        t.plan = far.plan;
        return;
      }
      const first = s?.spans[0];
      const last = s?.spans[s.spans.length - 1];
      const among =
        first !== undefined && last !== undefined && pointer >= first.start && pointer <= last.end;
      if (!among) {
        const place = elsewhere(t);
        if (place) {
          if (s) moveSlots(t, s.from);
          light(t, place.box, place.line);
          t.plan = place.plan;
          return;
        }
      }
      light(t, null, null);
      if (!s) {
        t.plan = null;
        return;
      }
      const to = slotOf(s.spans, s.from, center);
      const target = s.targets[to];
      const plan =
        to === s.from || !target
          ? null
          : latest.current.resolve(t.source, { ...target, edge: to > s.from ? EDGE.after : EDGE.before });
      moveSlots(t, plan ? to : s.from);
      t.plan = plan;
    };

    // The row where the pointer has it, kept in the list; then where that is.
    const place = (t: ITracking<TDrag, TDrop, TPlan>) => {
      const scroll = scrollOf();
      const natural = t.start0 - (scroll - t.scroll0);
      const visual = clampTo(along(t.x, t.y) - t.grab, t.listStart, t.listEnd - t.size);
      t.offset = visual - natural;
      t.mover.style.transform = shift(t.offset);
      const base = t.listStart - scroll;
      hit(t, along(t.x, t.y) - base, visual + t.size / 2 - base);
    };

    const autoscroll = (t: ITracking<TDrag, TDrop, TPlan>) => {
      // A list that does not scroll has nothing to wait for at its edges.
      const room = across ? list.scrollWidth > list.clientWidth : list.scrollHeight > list.clientHeight;
      const at = along(t.x, t.y);
      let speed = 0;
      if (room && at < t.listStart + SCROLL_ZONE)
        speed = -SCROLL_STEP * Math.min(1, (t.listStart + SCROLL_ZONE - at) / SCROLL_ZONE);
      else if (room && at > t.listEnd - SCROLL_ZONE)
        speed = SCROLL_STEP * Math.min(1, (at - (t.listEnd - SCROLL_ZONE)) / SCROLL_ZONE);
      t.scroll = speed;
    };

    // One write a frame, however many moves the pointer made in it; the edge of a scrolling list keeps it going.
    const frame = () => {
      const t = track;
      if (!t?.active) return;
      t.frame = undefined;
      if (t.scroll !== 0) {
        if (across) list.scrollLeft += t.scroll;
        else list.scrollTop += t.scroll;
      }
      place(t);
      autoscroll(t);
      if (t.scroll !== 0) schedule(t);
    };
    const schedule = (t: ITracking<TDrag, TDrop, TPlan>) => {
      if (t.frame === undefined) t.frame = requestAnimationFrame(frame);
    };

    const keepFromScrolling = (event: TouchEvent) => {
      if (event.cancelable) event.preventDefault();
    };
    const noMenu = (event: Event) => event.preventDefault();
    const eatClick = (event: MouseEvent) => {
      event.stopPropagation();
      event.preventDefault();
    };

    // Everything the drag wrote goes, at once: no easing (the attributes that ease go first).
    const reset = (t: ITracking<TDrag, TDrop, TPlan>) => {
      window.clearTimeout(timer);
      held.current = null;
      busy = false;
      const rows = [t.mover, ...(t.slots?.items ?? [])];
      for (const row of rows) {
        row.removeAttribute('data-drag-shift');
        row.removeAttribute('data-drag-settle');
      }
      for (const row of rows) row.style.transform = '';
      t.mover.removeAttribute('data-dragging');
      for (const host of t.hosts) host.removeAttribute('data-drag-host');
      list.removeAttribute('data-dragging');
      // A FLIP of the list (ui/use-flip) sits out the render that has just come: the rows are in their places.
      requestAnimationFrame(() => {
        if (!busy) list.removeAttribute('data-drag-held');
      });
      setDragging(false);
    };

    // The row eases back to its own place, the neighbours to theirs.
    const back = (t: ITracking<TDrag, TDrop, TPlan>) => {
      held.current = null;
      window.clearTimeout(timer);
      if (reducedMotion()) {
        reset(t);
        return;
      }
      t.mover.setAttribute('data-drag-settle', 'true');
      t.mover.style.transform = '';
      for (const item of t.slots?.items ?? []) item.style.transform = '';
      timer = window.setTimeout(() => reset(t), SETTLE_MS);
    };

    // The row eases into the place it was let go over; then the owner has the drop, and the row waits there for the
    // new order (it goes back when none comes).
    const commit = (t: ITracking<TDrag, TDrop, TPlan>, plan: TPlan) => {
      const s = t.slots;
      const among = s !== null && s.to !== s.from;
      const settle = among && !reducedMotion();
      if (among) {
        t.mover.setAttribute('data-drag-settle', 'true');
        t.mover.style.transform = shift(shiftsOf(s.spans, s.from, s.to)[s.from] ?? 0);
      }
      const hand = () => {
        held.current = {
          mover: t.mover,
          parent: t.mover.parentElement,
          index: indexIn(t.mover),
          release: () => reset(t),
        };
        const wait = () => {
          window.clearTimeout(timer);
          timer = window.setTimeout(() => {
            if (held.current?.mover === t.mover) back(t);
          }, COMMIT_WAIT_MS);
        };
        wait();
        const result = latest.current.onDrop(plan);
        if (result instanceof Promise) void result.then(wait, wait);
      };
      if (settle) timer = window.setTimeout(hand, SETTLE_MS);
      else hand();
    };

    const finish = (drop: boolean) => {
      const t = track;
      track = null;
      if (!t) return;
      window.clearTimeout(t.hold);
      if (t.frame !== undefined) cancelAnimationFrame(t.frame);
      window.removeEventListener('pointermove', move);
      window.removeEventListener('pointerup', up);
      window.removeEventListener('pointercancel', cancel);
      window.removeEventListener('keydown', key, true);
      window.removeEventListener('blur', lost);
      if (!t.active) return;
      try {
        list.releasePointerCapture(t.pointerId);
      } catch {
        // The pointer is gone already (a cancel, or the list left the page).
      }
      document.removeEventListener('touchmove', keepFromScrolling);
      document.removeEventListener('contextmenu', noMenu);
      clear(t.box);
      list.removeAttribute('data-dragging');
      // The click that ends a drag on a row is not a click on it.
      window.addEventListener('click', eatClick, { capture: true, once: true });
      window.setTimeout(() => window.removeEventListener('click', eatClick, { capture: true }), 0);
      if (drop && t.pointerType === 'touch' && !t.moved && latest.current.onHoldRelease) {
        reset(t);
        latest.current.onHoldRelease(t.source, { x: t.x, y: t.y });
        return;
      }
      if (drop && t.plan && t.outside) {
        // Carried out of the list: the row is where it was, the owner has what it stood for.
        latest.current.onDrop(t.plan);
        back(t);
      } else if (drop && t.plan) commit(t, t.plan);
      else back(t);
    };

    const activate = (t: ITracking<TDrag, TDrop, TPlan>) => {
      t.active = true;
      busy = true;
      last = t;
      window.clearTimeout(t.hold);
      const listRect = list.getBoundingClientRect();
      const moverRect = t.mover.getBoundingClientRect();
      t.listStart = start(listRect);
      t.listEnd = end(listRect);
      t.scroll0 = scrollOf();
      t.start0 = start(moverRect);
      t.size = end(moverRect) - start(moverRect);
      t.grab = along(t.x0, t.y0) - t.start0;
      t.slots = slotsOf(t);
      t.home = homeOf(t.mover);
      t.mover.setAttribute('data-dragging', 'true');
      for (const item of t.slots?.items ?? [])
        if (item !== t.mover) item.setAttribute('data-drag-shift', 'true');
      // The rows around it let it out of their clip and over what follows them.
      for (let at = t.mover.parentElement; at && at !== list; at = at.parentElement) {
        at.setAttribute('data-drag-host', 'true');
        t.hosts.push(at);
      }
      list.setAttribute('data-dragging', t.source.kind);
      list.setAttribute('data-drag-held', 'true');
      document.addEventListener('touchmove', keepFromScrolling, { passive: false });
      document.addEventListener('contextmenu', noMenu);
      setDragging(true);
      if (t.pointerType === 'touch') navigator.vibrate?.(8);
      // The list keeps the pointer's events, so a pointer that leaves the window still ends the drag here.
      try {
        list.setPointerCapture(t.pointerId);
      } catch {
        // A pointer that is already gone has nothing to capture.
      }
      place(t);
    };

    function move(event: PointerEvent) {
      const t = track;
      if (!t || event.pointerId !== t.pointerId) return;
      t.x = event.clientX;
      t.y = event.clientY;
      if (!t.active) {
        const far = Math.hypot(t.x - t.x0, t.y - t.y0);
        if (t.pointerType === 'touch' && far > TOUCH_SLOP) finish(false);
        else if (t.pointerType !== 'touch' && far >= MOUSE_SLOP) activate(t);
        return;
      }
      if (!t.moved && Math.hypot(t.x - t.x0, t.y - t.y0) > TOUCH_SLOP) t.moved = true;
      schedule(t);
    }
    function up(event: PointerEvent) {
      if (!track || event.pointerId !== track.pointerId) return;
      // The last moves of the pointer count, even when their frame has not come yet.
      if (track.active) {
        track.x = event.clientX;
        track.y = event.clientY;
        place(track);
      }
      finish(true);
    }
    function cancel(event: PointerEvent) {
      if (track && event.pointerId === track.pointerId) finish(false);
    }
    // The window lost the pointer (another window, a dialog of the system): nothing stays picked up.
    function lost() {
      finish(false);
    }
    function key(event: KeyboardEvent) {
      if (event.key !== 'Escape' || !track?.active) return;
      event.stopPropagation();
      finish(false);
    }

    const down = (event: PointerEvent) => {
      if (track || busy || (event.pointerType === 'mouse' && event.button !== 0)) return;
      const target = event.target as HTMLElement;
      if (target.closest('input, textarea, [data-nodrag]')) return;
      const el = target.closest<HTMLElement>('[data-drag]');
      const kind = kindOf(latest.current.dragKinds, el?.dataset.drag);
      const id = el?.dataset.dragId;
      if (!el || !kind || !id || !list.contains(el)) return;
      const t: ITracking<TDrag, TDrop, TPlan> = {
        pointerId: event.pointerId,
        pointerType: event.pointerType,
        x0: event.clientX,
        y0: event.clientY,
        x: event.clientX,
        y: event.clientY,
        source: { kind, id },
        mover: moverOf(el),
        active: false,
        moved: false,
        hold: undefined,
        plan: null,
        outside: false,
        box: null,
        scroll: 0,
        frame: undefined,
        listStart: 0,
        listEnd: 0,
        scroll0: 0,
        start0: 0,
        size: 0,
        grab: 0,
        offset: 0,
        slots: null,
        home: null,
        hosts: [],
      };
      track = t;
      if (event.pointerType === 'touch') t.hold = window.setTimeout(() => activate(t), HOLD_MS);
      window.addEventListener('pointermove', move);
      window.addEventListener('pointerup', up);
      window.addEventListener('pointercancel', cancel);
      window.addEventListener('keydown', key, true);
      window.addEventListener('blur', lost);
    };

    list.addEventListener('pointerdown', down);
    return () => {
      list.removeEventListener('pointerdown', down);
      finish(false);
      if (busy && last) reset(last);
    };
  }, [listRef, axis]);

  return { dragging };
};

export default useDrag;
