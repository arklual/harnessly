// A pill floating over the canvas of a design (HRN-673, HRN-674): dragged by any part of it to any place over the canvas,
// its settings on a right click (a two-finger press on a trackpad), hidden from there and brought back from the bar's
// «…». The zoom pill (zoom-pill.tsx) and the phone's pill (phone-pill.tsx) are two of them; each keeps its own place
// (zoom-pill-pos.ts).
import { useEffect, useEffectEvent, useLayoutEffect, useRef, useState, useSyncExternalStore } from 'react';
import type { MouseEvent as ReactMouseEvent, PointerEvent as ReactPointerEvent, RefObject } from 'react';
import {
  clampPill,
  PILL_DEFAULT,
  PILL_DRAG_SLOP,
  readPill,
  writePill,
  type IPillPos,
  type IPillPrefs,
  type TPillKey,
} from '@/next/features/designly/zoom-pill-pos';
import Icon from '@/next/ui/icon';
import { usePresence, type IPresence } from '@/next/ui/presence';

const MENU_MS = 190;

/** The place and the hiding of one pill, shared by every canvas of the page and kept in the browser. */
export interface IPillStore {
  get: () => IPillPrefs;
  set: (next: IPillPrefs) => void;
  subscribe: (listener: () => void) => () => void;
}

export const pillStore = (key: TPillKey): IPillStore => {
  const listeners = new Set<() => void>();
  let prefs: IPillPrefs | null = null;
  const get = (): IPillPrefs => (prefs ??= readPill(key));
  return {
    get,
    set: (next) => {
      prefs = next;
      writePill(key, next);
      for (const listener of [...listeners]) listener();
    },
    subscribe: (listener) => {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    },
  };
};

export const usePillPrefs = (store: IPillStore): IPillPrefs =>
  useSyncExternalStore(store.subscribe, store.get, () => PILL_DEFAULT);

export const showPill = (store: IPillStore) => store.set({ ...store.get(), hidden: false });

const sizeOf = (el: Element | null) => ({ w: el?.clientWidth ?? 0, h: el?.clientHeight ?? 0 });

interface IFloatingOpts {
  /**
   * Where the pill stands while it was never dragged, measured in the canvas `area`; null leaves it to its CSS (the zoom
   * pill's corner). It may set the pill's own attributes (its axis) before it is measured.
   */
  auto?: (area: HTMLElement, pill: HTMLElement) => IPillPos | null;
  /** Calls `again` whenever the place of `auto` may have moved (the canvas panned); returns the way to stop. */
  follow?: (area: HTMLElement, again: () => void) => () => void;
  /** Anything that changes the place of `auto` or the pill's size (another device, a turn). */
  watch?: string;
  /** The pill stands where it is to stand (or was let go of): it may turn its menu and hints to the side with room. */
  placed?: (pill: HTMLElement, area: HTMLElement) => void;
}

export interface IFloatingPill {
  prefs: IPillPrefs;
  menu: IPresence<true>;
  closeMenu: () => void;
  /** The pill's own handlers: the drag, the click a drag must not be, the menu. */
  bind: {
    onPointerDown: (event: ReactPointerEvent<HTMLDivElement>) => void;
    onClickCapture: (event: ReactMouseEvent<HTMLDivElement>) => void;
    onContextMenu: (event: ReactMouseEvent<HTMLDivElement>) => void;
  };
  /** Back to its own place; hidden until «…» shows it again. */
  reset: () => void;
  hide: () => void;
}

/** The pill `ref` is on: its place, its drag and its menu. */
export const useFloatingPill = (
  ref: RefObject<HTMLDivElement | null>,
  store: IPillStore,
  opts: IFloatingOpts = {},
): IFloatingPill => {
  const prefs = usePillPrefs(store);
  const { pos, hidden } = prefs;
  const [open, setOpen] = useState(false);
  const menu = usePresence<true>(open ? true : null, MENU_MS);
  const dragged = useRef(false);
  const dragging = useRef(false);
  const autoOf = useEffectEvent((area: HTMLElement, pill: HTMLElement) => opts.auto?.(area, pill) ?? null);
  const follow = useEffectEvent((area: HTMLElement, again: () => void) => opts.follow?.(area, again));
  const placed = useEffectEvent((pill: HTMLElement, area: HTMLElement) => opts.placed?.(pill, area));

  // Its place, kept inside the canvas as the canvas changes its size (and beside what it follows, while not dragged).
  useLayoutEffect(() => {
    const el = ref.current;
    const area = el?.parentElement;
    if (!el || !area) return;
    const place = () => {
      if (dragging.current) return;
      const want = pos ?? autoOf(area, el);
      if (!want) {
        el.style.removeProperty('--pill-r');
        el.style.removeProperty('--pill-b');
      } else {
        const at = clampPill(want, sizeOf(area), { w: el.offsetWidth, h: el.offsetHeight });
        el.style.setProperty('--pill-r', `${at.right}px`);
        el.style.setProperty('--pill-b', `${at.bottom}px`);
      }
      placed(el, area);
    };
    place();
    const observer = new ResizeObserver(place);
    observer.observe(area);
    observer.observe(el);
    const stop = pos === null ? follow(area, place) : undefined;
    return () => {
      observer.disconnect();
      stop?.();
    };
  }, [ref, pos, hidden, opts.watch]);

  useEffect(() => {
    if (!open) return;
    const onDown = (event: PointerEvent) => {
      if (!ref.current?.contains(event.target as Node)) setOpen(false);
    };
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setOpen(false);
    };
    window.addEventListener('pointerdown', onDown);
    window.addEventListener('keydown', onKey);
    return () => {
      window.removeEventListener('pointerdown', onDown);
      window.removeEventListener('keydown', onKey);
    };
  }, [ref, open]);

  // A press anywhere on the pill drags it once it moves; a press that does not move is the button's click.
  const onPointerDown = (event: ReactPointerEvent<HTMLDivElement>) => {
    const el = ref.current;
    const area = el?.parentElement;
    if (event.button !== 0 || !el || !area || open) return;
    event.stopPropagation();
    dragged.current = false;
    const start = { x: event.clientX, y: event.clientY };
    const box = area.getBoundingClientRect();
    const pill = el.getBoundingClientRect();
    const from = { right: box.right - pill.right, bottom: box.bottom - pill.bottom };
    const pillSize = { w: pill.width, h: pill.height };
    let at = from;
    const onMove = (move: PointerEvent) => {
      const dx = move.clientX - start.x;
      const dy = move.clientY - start.y;
      if (!dragged.current && Math.hypot(dx, dy) < PILL_DRAG_SLOP) return;
      if (!dragged.current) {
        dragged.current = true;
        dragging.current = true;
        el.dataset.dragging = 'true';
        // A fast hand runs over the board's frame, which would take the moves for itself and drop the drag: the pill
        // keeps the pointer, and every frame of the page is deaf until the release. Taken only once it is a drag, so a
        // press that stays put is still a click on the button under it.
        try {
          el.setPointerCapture(move.pointerId);
        } catch {
          // The pointer is already gone (released between the events): the release below ends the drag anyway.
        }
        document.documentElement.dataset.pillDrag = 'true';
      }
      at = clampPill({ right: from.right - dx, bottom: from.bottom - dy }, sizeOf(area), pillSize);
      el.style.setProperty('--pill-r', `${at.right}px`);
      el.style.setProperty('--pill-b', `${at.bottom}px`);
    };
    const onUp = () => {
      window.removeEventListener('pointermove', onMove);
      window.removeEventListener('pointerup', onUp);
      window.removeEventListener('pointercancel', onUp);
      if (!dragged.current) return;
      dragging.current = false;
      delete el.dataset.dragging;
      delete document.documentElement.dataset.pillDrag;
      store.set({ ...store.get(), pos: at });
      opts.placed?.(el, area);
    };
    window.addEventListener('pointermove', onMove);
    window.addEventListener('pointerup', onUp);
    window.addEventListener('pointercancel', onUp);
  };

  return {
    prefs,
    menu,
    closeMenu: () => setOpen(false),
    bind: {
      onPointerDown,
      // The drag's release is no press on the button under it.
      onClickCapture: (event) => {
        if (!dragged.current) return;
        dragged.current = false;
        event.stopPropagation();
        event.preventDefault();
      },
      onContextMenu: (event) => {
        event.preventDefault();
        setOpen(true);
      },
    },
    reset: () => {
      setOpen(false);
      store.set({ ...store.get(), pos: null });
    },
    hide: () => {
      setOpen(false);
      store.set({ ...store.get(), hidden: true });
    },
  };
};

interface IPillMenuFootProps {
  /** The pill was moved: it can go back. */
  placed: boolean;
  onReset: () => void;
  onHide: () => void;
  /** «Вернуть в угол», «Вернуть на место». */
  resetLabel: string;
  hideLabel: string;
  hint: string;
}

/** The end of a pill's menu: back to its place (once it was moved), hide, and how to move it. */
export const PillMenuFoot = ({
  placed,
  onReset,
  onHide,
  resetLabel,
  hideLabel,
  hint,
}: IPillMenuFootProps) => (
  <>
    <div className="hx-zoomctl__sep" role="separator" />
    {placed && (
      <button type="button" role="menuitem" className="hx-option" onClick={onReset}>
        <Icon name="corner-down-right" size="sm" />
        <span className="hx-option__name">{resetLabel}</span>
      </button>
    )}
    <button type="button" role="menuitem" className="hx-option" onClick={onHide}>
      <Icon name="eye-off" size="sm" />
      <span className="hx-option__name">{hideLabel}</span>
    </button>
    <div className="hx-zoompill__hint">{hint}</div>
  </>
);
