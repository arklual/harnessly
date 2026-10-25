import { useLayoutEffect, useRef, type RefObject } from 'react';
import { PHONE_QUERY } from '@/next/features/shell/gestures';
import { SIDE_MOTION_EASING, SIDE_MOTION_MS } from '@/next/features/shell/use-side-motion';

/** The phone's sheet leaves by its own CSS motion (styles/shell.css), this long. */
const SHEET_EXIT_MS = 300;

/** The scroll box of the chat and the field centre in the stage: they go half the way, as the window does in the sidebar's slide. */
const CENTRED = '.hx-feed, .hx-dock';
/** The column at the stage's top right keeps its distance from the pane's edge: it goes the whole way. */
const BESIDE = '.hx-side';
/** The header's buttons that stay (the bell, a new chat) stand at the pane's edge as it moves. */
const HEADER_END = '.hx-header__end';

const px = (value: string): number => Number.parseFloat(value) || 0;

const reduced = (): boolean => matchMedia('(prefers-reduced-motion: reduce)').matches;
const phone = (): boolean => matchMedia(PHONE_QUERY).matches;

/** How long the pane stays mounted once it is closed: as long as its way out. */
export const paneExitMs = (): number => {
  if (phone()) return SHEET_EXIT_MS;
  return reduced() ? 0 : SIDE_MOTION_MS;
};

/** How far the pane is now from where the layout put it (the running slide's frame, 0 at rest). */
const transformShift = (el: HTMLElement): number => {
  const transform = getComputedStyle(el).transform;
  if (!transform || transform === 'none') return 0;
  return new DOMMatrixReadOnly(transform).m41;
};

const translateShift = (el: HTMLElement): number => px(getComputedStyle(el).translate);

/** The stage's `--pane-w` is a clamp() of container units: its length is whatever an element sized by it measures. */
const paneWidth = (stage: HTMLElement): number => {
  const probe = document.createElement('div');
  probe.style.setProperty('position', 'absolute');
  probe.style.setProperty('visibility', 'hidden');
  probe.style.setProperty('width', 'var(--pane-w)');
  stage.append(probe);
  const width = probe.offsetWidth;
  probe.remove();
  return width;
};

/**
 * The right pane opens and closes on the compositor, in step with the chat, as the sidebar does (use-side-motion.ts):
 * same 240 ms, same curve. The stage takes the chat's new columns at once, so the chat is laid out once; the pane then
 * slides in by transform from beyond the stage's edge, and the chat's scroll box and field go from where they stood
 * to where they are (half of the pane's width, as they are centred), the column at the right by all of it. So the
 * chat narrows with the pane's edge and nothing is laid out or painted on the way. A toggle in the middle of a slide
 * starts from where the parts are now. The phone's sheet (own motion) and reduced motion skip it.
 */
const usePaneMotion = (pane: RefObject<HTMLElement | null>, open: boolean): void => {
  const previous = useRef(open);
  const running = useRef<Animation[]>([]);

  useLayoutEffect(() => {
    if (previous.current === open) return;
    previous.current = open;
    const paneEl = pane.current;
    const stage = paneEl?.closest<HTMLElement>('.hx-main__body');
    if (!paneEl || !stage) return;
    const slidingBefore = running.current.some((animation) => animation.playState === 'running');
    const paneFrom = transformShift(paneEl);
    const parts = Array.from(
      stage.querySelectorAll<HTMLElement>(`${CENTRED}, ${BESIDE}, ${HEADER_END}`),
      (el) => ({
        el,
        share: el.matches(CENTRED) ? 0.5 : 1,
        now: translateShift(el),
      }),
    );
    for (const animation of running.current) animation.cancel();
    running.current = [];
    if (phone() || reduced()) return;
    const gap = px(getComputedStyle(paneEl).right);
    const span = paneWidth(stage) + gap;
    const away = paneEl.offsetWidth + gap;
    const timing: KeyframeAnimationOptions = { duration: SIDE_MOTION_MS, easing: SIDE_MOTION_EASING };
    // The chat's parts stand where the new layout puts them; they come there from where the old layout had them.
    const chat = parts.map(({ el, share, now }) =>
      el.animate(
        [{ translate: `${now + (open ? 1 : -1) * span * share}px 0` }, { translate: '0px 0' }],
        timing,
      ),
    );
    const start = open && !slidingBefore ? away : paneFrom;
    const slide = paneEl.animate(
      [{ transform: `translateX(${start}px)` }, { transform: `translateX(${open ? 0 : away}px)` }],
      { ...timing, fill: open ? 'none' : 'forwards' },
    );
    // Kept after they end: the exit holds the pane out of the stage (fill) until the next toggle cancels it.
    running.current = [...chat, slide];
  }, [open, pane]);

  useLayoutEffect(
    () => () => {
      for (const animation of running.current) animation.cancel();
      running.current = [];
    },
    [],
  );
};

export default usePaneMotion;

/**
 * «Во всю ширину» and back: the pane's width goes from where it was to where it is on the same 240 ms and curve as its
 * opening, and what is in it is laid out once, at the wider of the two widths, behind the pane's edge (a terminal or a
 * page is not asked to refit on every frame). The chat is not touched: its columns follow the pane's own width, which
 * a maximised pane covers rather than narrows, so the reader's place stays. A toggle in the middle starts from the
 * width the pane has then. The phone and reduced motion skip it.
 */
export const usePaneMax = (pane: RefObject<HTMLElement | null>, maxed: boolean, mounted: boolean): void => {
  const previous = useRef(maxed);
  const laidOut = useRef(0);
  const running = useRef<Animation | null>(null);

  // The width the layout last gave the pane: where a slide starts from, as the layout is already the new one by then.
  useLayoutEffect(() => {
    const el = pane.current;
    if (!mounted || !el || typeof ResizeObserver === 'undefined') return;
    laidOut.current = el.getBoundingClientRect().width;
    const observer = new ResizeObserver(() => {
      if (!running.current) laidOut.current = el.getBoundingClientRect().width;
    });
    observer.observe(el);
    return () => observer.disconnect();
  }, [pane, mounted]);

  useLayoutEffect(() => {
    if (previous.current === maxed) return;
    previous.current = maxed;
    const el = pane.current;
    if (!el) return;
    const sliding = running.current !== null;
    // A slide in progress holds the pane at its frame: that is where the next one starts.
    const from = sliding ? el.getBoundingClientRect().width : laidOut.current;
    running.current?.cancel();
    running.current = null;
    el.removeAttribute('data-resizing');
    if (phone() || reduced() || from === 0) return;
    const to = el.getBoundingClientRect().width;
    if (Math.abs(from - to) < 1) return;
    el.style.setProperty('--hx-inner-w', `${Math.max(from, to)}px`);
    el.setAttribute('data-resizing', 'true');
    const slide = el.animate([{ width: `${from}px` }, { width: `${to}px` }], {
      duration: SIDE_MOTION_MS,
      easing: SIDE_MOTION_EASING,
    });
    running.current = slide;
    void slide.finished.then(
      () => {
        if (running.current !== slide) return;
        running.current = null;
        el.removeAttribute('data-resizing');
        laidOut.current = to;
      },
      () => undefined,
    );
  }, [maxed, pane]);

  useLayoutEffect(
    () => () => {
      running.current?.cancel();
      running.current = null;
    },
    [],
  );
};
