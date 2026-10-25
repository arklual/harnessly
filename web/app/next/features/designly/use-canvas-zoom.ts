// The canvas of a design on screen: ⌘/Ctrl + wheel or a trackpad's pinch zooms around the cursor, the wheel
// alone and a two-finger swipe pan, a drag of the empty area (or of anything while Space is held, or with the middle
// button) pans, two fingers pinch on a touch screen. The view is written to custom properties on the area (--zx, --zy,
// --zs: the box's transform) and on the section (--zs, --zcx, --zcy: how a point of the section moves with it, which
// the edit card follows), once a frame, without a render; only what the controls read (the scale, the fit, the lock)
// is state, and it changes when they would show something else.
//
// The page itself is a frame of another document: a wheel over it scrolls the page, as it should, and never reaches
// here. Holding Space lays a shield over the frame, so the canvas takes the wheel and the drag there too; the page's
// bridge tells Space, a drag with the middle button and a wheel the page cannot scroll by (`fromPage`), since the
// frame keeps them once it has the focus or the press.
//
// A fit (FIT) is kept: the area resized or another device keeps the box fitted the same way until the person moves the
// view. The lock leaves the view as it is: no pan, no zoom, only a fit can be picked (and is kept).
import { useEffect, useLayoutEffect, useRef, useState, type RefObject } from 'react';
import type { IPoint } from '@/next/features/designly/site-drag';
import { startView, type IViewMemory } from '@/next/features/designly/view-memory';
import {
  FIT,
  fitView,
  gestureOf,
  isFit,
  keptInView,
  panBy,
  shiftOf,
  takesPagePan,
  unzoomed,
  wheelFactor,
  zoomKeyOf,
  ZOOM_KEY,
  ZOOM,
  ZOOM_FIT,
  zoomAt,
  zoomPct,
  type IInset,
  type IZoom,
  type TFit,
} from '@/next/features/designly/site-zoom';

/** The view a design kept and the way to keep it again; without it the canvas starts at its fit and keeps nothing. */
export interface ICanvasMemory {
  start: Pick<IViewMemory, 'fit' | 'zoom' | 'box' | 'locked'>;
  onChange: (view: Pick<IViewMemory, 'fit' | 'zoom' | 'box' | 'locked'>) => void;
}

export interface ICanvasZoom {
  /** The scale the person reads, in percent. */
  pct: number;
  zoomed: boolean;
  canIn: boolean;
  canOut: boolean;
  zoomIn: () => void;
  zoomOut: () => void;
  fit: () => void;
  /** Back to the fit, as the keys do (⌘0, Shift+1). */
  reset: () => void;
  /** The scale the person reads set to this many percent around the middle of the area (smoothly). */
  zoomTo: (pct: number) => void;
  /** The fit the view keeps; null once the person moved it. */
  fitMode: TFit | null;
  /** The box fitted this way (smoothly), and kept so. */
  fitTo: (mode: TFit) => void;
  /** No pan and no zoom while locked. */
  locked: boolean;
  onLock: () => void;
  /** A point of the section picked on the zoomed page, carried back to the fit (where the edit card keeps it). */
  toFit: (at: IPoint) => IPoint;
  /** What the page's bridge tells: Space held or let go over it, a pan of the screen's pixels. */
  fromPage: { space: (down: boolean) => void; pan: (dx: number, dy: number, drag: boolean) => void };
}

/** The smooth step of the view (site.css, .hx-vparea[data-zsmooth]) and the quiet after a move that counts as settled. */
const SMOOTH_MS = 320;
const SETTLE_MS = 150;

/** What a press on the canvas must leave alone: its controls, the handles of «Резина», the pins. */
const CONTROLS = 'button, a, input, textarea, select, [role="button"], .hx-vpgrab, .hx-ovw';

const typing = (target: EventTarget | null) =>
  (target as Element | null)?.closest?.('input, textarea, select, [contenteditable]') != null;

/** What the controls read of a view: they render again only when it changes. */
interface IShown {
  s: number;
  zoomed: boolean;
}
const shownOf = (z: IZoom): IShown => ({ s: z.s, zoomed: !isFit(z) });

const insetOf = (el: HTMLElement): IInset => {
  const style = getComputedStyle(el);
  return {
    l: parseFloat(style.paddingLeft) || 0,
    t: parseFloat(style.paddingTop) || 0,
    r: parseFloat(style.paddingRight) || 0,
    b: parseFloat(style.paddingBottom) || 0,
  };
};

export const useCanvasZoom = (
  areaRef: RefObject<HTMLElement | null>,
  sectionRef: RefObject<HTMLElement | null>,
  fit: number,
  on: boolean,
  /** A new device, size or turn: the view goes back to its fit. */
  resetKey: string,
  /** The box's size at the layout's fit, which a fit counts from (the box's own size may still be on its way there). */
  box: { w: number; h: number },
  memory?: ICanvasMemory,
): ICanvasZoom => {
  const view = useRef<IZoom>(ZOOM_FIT);
  const shift = useRef<IPoint>({ x: 0, y: 0 });
  const startFit = memory ? memory.start.fit : FIT.auto;
  const startLocked = memory?.start.locked ?? false;
  const fitRef = useRef<TFit | null>(startFit);
  const lockedRef = useRef(startLocked);
  const settle = useRef(0);
  // A free view kept from before waits for the box to have its size (and the area its own) to be put back over it.
  const restore = useRef<ICanvasMemory['start'] | null>(
    memory && memory.start.fit === null ? memory.start : null,
  );
  const [shown, setShown] = useState<IShown>(shownOf(ZOOM_FIT));
  const [fitMode, setFitMode] = useState<TFit | null>(startFit);
  const [locked, setLocked] = useState(startLocked);
  const boxRef = useRef(box);
  const onChange = useRef(memory?.onChange);
  useLayoutEffect(() => {
    onChange.current = memory?.onChange;
  });
  /** Tells what the view is now (not while a kept free view is still to come back). */
  const report = () => {
    if (restore.current !== null) return;
    onChange.current?.({
      fit: fitRef.current,
      zoom: view.current,
      box: boxRef.current,
      locked: lockedRef.current,
    });
  };
  useLayoutEffect(() => {
    boxRef.current = box;
  });

  const keepFit = (mode: TFit | null) => {
    if (fitRef.current === mode) return;
    fitRef.current = mode;
    setFitMode(mode);
  };

  /** The box's corner as the layout put it, in the client's pixels (a transform does not move offsets). */
  const originOf = (area: HTMLElement): IPoint | null => {
    const el = area.querySelector<HTMLElement>('.hx-vpbox');
    if (!el) return null;
    const r = area.getBoundingClientRect();
    return { x: r.left + el.offsetLeft, y: r.top + el.offsetTop };
  };

  /** The view as the canvas can take it: the box never leaves the area entirely. */
  const bound = (z: IZoom): IZoom => {
    const area = areaRef.current;
    const el = area?.querySelector<HTMLElement>('.hx-vpbox');
    if (!area || !el) return z;
    return keptInView(
      z,
      { x: el.offsetLeft, y: el.offsetTop, w: el.offsetWidth, h: el.offsetHeight },
      { w: area.clientWidth, h: area.clientHeight },
    );
  };

  /** The view of the kept fit for the area as it is now. */
  const fitted = (mode: TFit): IZoom => {
    const area = areaRef.current;
    if (!area) return ZOOM_FIT;
    return fitView(mode, boxRef.current, { w: area.clientWidth, h: area.clientHeight }, insetOf(area));
  };

  const apply = (want: IZoom, smooth: boolean) => {
    const next = fitRef.current === null ? bound(want) : want;
    view.current = next;
    const area = areaRef.current;
    const section = sectionRef.current;
    if (!area || !section) return;
    const origin = originOf(area);
    const home = section.getBoundingClientRect();
    shift.current = origin
      ? shiftOf(next, { x: origin.x - home.left, y: origin.y - home.top })
      : { x: 0, y: 0 };
    area.dataset.zsmooth = String(smooth);
    // While the view moves the box is a layer of its own, moved without a new raster each frame; once it settles it is
    // drawn again at its scale, so a zoomed page is sharp and not a stretched picture of itself.
    area.dataset.zmoving = 'true';
    window.clearTimeout(settle.current);
    settle.current = window.setTimeout(
      () => {
        area.dataset.zmoving = 'false';
      },
      smooth ? SMOOTH_MS + SETTLE_MS : SETTLE_MS,
    );
    section.dataset.zsmooth = String(smooth);
    area.dataset.zoomed = String(!isFit(next));
    area.style.setProperty('--zx', `${next.x}px`);
    area.style.setProperty('--zy', `${next.y}px`);
    area.style.setProperty('--zs', String(next.s));
    section.style.setProperty('--zs', String(next.s));
    section.style.setProperty('--zcx', `${shift.current.x}px`);
    section.style.setProperty('--zcy', `${shift.current.y}px`);
    const nextShown = shownOf(next);
    setShown((was) => (was.s === nextShown.s && was.zoomed === nextShown.zoomed ? was : nextShown));
    report();
  };
  /** The view moved by the person: it no longer keeps a fit. */
  const move = (want: IZoom, smooth: boolean) => {
    keepFit(null);
    apply(want, smooth);
  };
  const fitTo = (mode: TFit, smooth: boolean) => {
    keepFit(mode);
    apply(fitted(mode), smooth);
  };

  const latestApply = useRef(apply);
  const latestMove = useRef(move);
  const latestFitTo = useRef(fitTo);
  const latestBound = useRef(bound);
  const latestKeepFit = useRef(keepFit);
  useLayoutEffect(() => {
    latestKeepFit.current = keepFit;
    latestApply.current = apply;
    latestMove.current = move;
    latestFitTo.current = fitTo;
    latestBound.current = bound;
  });

  // Another device or size, or the canvas is not offered: the view takes its fit again (the auto one after a free view).
  useLayoutEffect(() => {
    const kept = restore.current;
    const area = areaRef.current;
    if (kept !== null && on && area && boxRef.current.w > 0) {
      restore.current = null;
      const start = startView(kept, boxRef.current);
      fitRef.current = start.fit;
      setFitMode(start.fit);
      if (start.fit === null) {
        latestApply.current(start.zoom, false);
        return;
      }
    }
    latestFitTo.current(fitRef.current ?? FIT.auto, false);
    // The area is read when the device or the offer changes, not whenever a caller hands in a new ref object.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [resetKey, on]);

  // The kept lock is put on the area (the CSS reads it there).
  useLayoutEffect(() => {
    if (areaRef.current) areaRef.current.dataset.locked = String(lockedRef.current);
  }, [areaRef]);

  // The layout fitted the box to another size: a kept fit follows it.
  useLayoutEffect(() => {
    if (on && fitRef.current !== null && fitRef.current !== FIT.auto)
      latestFitTo.current(fitRef.current, false);
  }, [box.w, box.h, on]);

  useEffect(() => () => window.clearTimeout(settle.current), []);

  // The area changes its size (the pane is dragged wider): a kept fit is counted again, the box's corner moved, the
  // card's carry is counted again.
  useEffect(() => {
    const area = areaRef.current;
    if (!area || !on) return;
    const observer = new ResizeObserver(() => {
      if (fitRef.current !== null) latestFitTo.current(fitRef.current, false);
      else latestApply.current(view.current, false);
    });
    observer.observe(area);
    return () => observer.disconnect();
  }, [areaRef, on]);

  // Space as the frame tells it, from outside the effect below.
  const spaceRef = useRef<(down: boolean) => void>(() => {});
  const pageScheduleRef = useRef<(want: IZoom) => void>(() => {});

  useEffect(() => {
    const area = areaRef.current;
    if (!area || !on) return;
    let frame = 0;
    let pending: IZoom | null = null;
    const schedule = (want: IZoom) => {
      latestKeepFit.current(null);
      const next = latestBound.current(want);
      pending = next;
      view.current = next;
      if (frame !== 0) return;
      frame = requestAnimationFrame(() => {
        frame = 0;
        if (pending) latestApply.current(pending, false);
        pending = null;
      });
    };
    pageScheduleRef.current = schedule;
    const local = (client: IPoint): IPoint => {
      const origin = originOf(area);
      return origin ? { x: client.x - origin.x, y: client.y - origin.y } : client;
    };
    /** The middle of the area, in the box's own pixels: where the keys and the buttons zoom around. */
    const centerOf = (el: HTMLElement): IPoint => {
      const r = el.getBoundingClientRect();
      return local({ x: r.left + r.width / 2, y: r.top + r.height / 2 });
    };

    const onWheel = (event: WheelEvent) => {
      event.preventDefault();
      if (lockedRef.current) return;
      if (event.ctrlKey || event.metaKey) {
        const z = view.current;
        schedule(zoomAt(z, local({ x: event.clientX, y: event.clientY }), z.s * wheelFactor(event.deltaY)));
      } else schedule(panBy(view.current, -event.deltaX, -event.deltaY));
    };

    // Safari's pinch is a gesture, not a wheel: its scale is counted from the start, the view follows its steps.
    let gestureScale = 1;
    const onGestureStart = (event: Event) => {
      event.preventDefault();
      gestureScale = 1;
    };
    const onGestureChange = (event: Event) => {
      event.preventDefault();
      const next = gestureOf(event);
      if (!lockedRef.current) {
        const z = view.current;
        schedule(zoomAt(z, local(next.at), z.s * (next.scale / gestureScale)));
      }
      gestureScale = next.scale;
    };

    // Space held over the canvas: the shield covers the page, the cursor is a hand.
    let over = false;
    let space = false;
    const setSpace = (value: boolean) => {
      space = value && !lockedRef.current;
      area.dataset.space = String(space);
    };
    spaceRef.current = setSpace;
    const onEnter = () => {
      over = true;
    };
    const onLeave = () => {
      over = false;
    };
    const onKeyDown = (event: KeyboardEvent) => {
      const key = zoomKeyOf(event);
      if (key !== null && !typing(event.target)) {
        event.preventDefault();
        if (lockedRef.current) return;
        const z = view.current;
        if (key === ZOOM_KEY.reset) latestFitTo.current(FIT.auto, true);
        else
          latestMove.current(
            zoomAt(z, centerOf(area), z.s * (key === ZOOM_KEY.in ? ZOOM.step : 1 / ZOOM.step)),
            true,
          );
        return;
      }
      if (event.code !== 'Space' || !over || typing(event.target) || event.repeat) {
        if (event.code === 'Space' && space) event.preventDefault();
        return;
      }
      event.preventDefault();
      setSpace(true);
    };
    const onKeyUp = (event: KeyboardEvent) => {
      if (event.code === 'Space' && space) setSpace(false);
    };
    const onBlur = () => setSpace(false);

    // The pointers on the canvas: one pans, two pinch.
    const pointers = new Map<number, IPoint>();
    let pinch: { dist: number; mid: IPoint } | null = null;
    const pinchOf = (): { dist: number; mid: IPoint } | null => {
      const [a, b] = [...pointers.values()];
      if (!a || !b) return null;
      return { dist: Math.hypot(a.x - b.x, a.y - b.y), mid: { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 } };
    };
    const onDown = (event: PointerEvent) => {
      if (lockedRef.current) return;
      const target = event.target as Element;
      const free = space || event.button === 1 || pointers.size > 0;
      if (!free && (event.button !== 0 || target.closest(CONTROLS))) return;
      // A left press on the page's case (not on its empty area) is the page's: only Space or the middle button pan it.
      if (!free && event.pointerType === 'mouse' && target.closest('.hx-vpbox')) return;
      event.preventDefault();
      area.setPointerCapture(event.pointerId);
      pointers.set(event.pointerId, { x: event.clientX, y: event.clientY });
      pinch = pinchOf();
      area.dataset.panning = 'true';
    };
    const onMove = (event: PointerEvent) => {
      const was = pointers.get(event.pointerId);
      if (!was) return;
      const now = { x: event.clientX, y: event.clientY };
      pointers.set(event.pointerId, now);
      if (pointers.size >= 2 && pinch) {
        const next = pinchOf();
        if (!next) return;
        const z = view.current;
        const zoomed = pinch.dist > 0 ? zoomAt(z, local(next.mid), (z.s * next.dist) / pinch.dist) : z;
        schedule(panBy(zoomed, next.mid.x - pinch.mid.x, next.mid.y - pinch.mid.y));
        pinch = next;
        return;
      }
      schedule(panBy(view.current, now.x - was.x, now.y - was.y));
    };
    const onUp = (event: PointerEvent) => {
      if (!pointers.delete(event.pointerId)) return;
      pinch = pinchOf();
      if (pointers.size === 0) area.dataset.panning = 'false';
    };

    area.addEventListener('wheel', onWheel, { passive: false });
    area.addEventListener('gesturestart', onGestureStart);
    area.addEventListener('gesturechange', onGestureChange);
    area.addEventListener('pointerenter', onEnter);
    area.addEventListener('pointerleave', onLeave);
    area.addEventListener('pointerdown', onDown);
    area.addEventListener('pointermove', onMove);
    area.addEventListener('pointerup', onUp);
    area.addEventListener('pointercancel', onUp);
    window.addEventListener('keydown', onKeyDown);
    window.addEventListener('keyup', onKeyUp);
    window.addEventListener('blur', onBlur);
    return () => {
      cancelAnimationFrame(frame);
      spaceRef.current = () => {};
      pageScheduleRef.current = () => {};
      area.removeEventListener('wheel', onWheel);
      area.removeEventListener('gesturestart', onGestureStart);
      area.removeEventListener('gesturechange', onGestureChange);
      area.removeEventListener('pointerenter', onEnter);
      area.removeEventListener('pointerleave', onLeave);
      area.removeEventListener('pointerdown', onDown);
      area.removeEventListener('pointermove', onMove);
      area.removeEventListener('pointerup', onUp);
      area.removeEventListener('pointercancel', onUp);
      window.removeEventListener('keydown', onKeyDown);
      window.removeEventListener('keyup', onKeyUp);
      window.removeEventListener('blur', onBlur);
      area.dataset.space = 'false';
      area.dataset.panning = 'false';
    };
  }, [areaRef, on]);

  /** The view zoomed around the middle of the area, as the buttons and the menu do it: smoothly. */
  const aroundMiddle = (scale: number) => {
    const area = areaRef.current;
    const origin = area ? originOf(area) : null;
    if (!area || !origin || lockedRef.current) return;
    const r = area.getBoundingClientRect();
    const at = { x: r.left + r.width / 2 - origin.x, y: r.top + r.height / 2 - origin.y };
    move(zoomAt(view.current, at, scale), true);
  };
  const step = (factor: number) => aroundMiddle(view.current.s * factor);

  return {
    pct: zoomPct(fit, { ...ZOOM_FIT, s: shown.s }),
    zoomed: shown.zoomed,
    canIn: !locked && shown.s < ZOOM.max,
    canOut: !locked && shown.s > ZOOM.min,
    zoomIn: () => step(ZOOM.step),
    zoomOut: () => step(1 / ZOOM.step),
    fit: () => fitTo(FIT.auto, true),
    reset: () => fitTo(FIT.auto, true),
    zoomTo: (pct) => aroundMiddle(pct / 100 / fit),
    fitMode,
    fitTo: (mode) => fitTo(mode, true),
    locked,
    onLock: () => {
      lockedRef.current = !lockedRef.current;
      setLocked(lockedRef.current);
      if (areaRef.current) areaRef.current.dataset.locked = String(lockedRef.current);
      if (lockedRef.current) spaceRef.current(false);
      report();
    },
    toFit: (at) => unzoomed(view.current, shift.current, at),
    fromPage: {
      space: (down) => spaceRef.current(down),
      pan: (dx, dy, drag) => {
        const area = areaRef.current;
        if (!area) return;
        const take = takesPagePan({
          locked: lockedRef.current,
          drag,
          fill: area.dataset.dev === 'fill',
          atFit: isFit(view.current),
        });
        if (take) pageScheduleRef.current(panBy(view.current, dx, dy));
      },
    },
  };
};
