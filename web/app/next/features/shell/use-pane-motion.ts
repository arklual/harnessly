import { useLayoutEffect, useRef, type RefObject } from 'react';
import { PHONE_QUERY } from '@/next/features/shell/gestures';
import { SIDE_MOTION_EASING, SIDE_MOTION_MS } from '@/next/features/shell/use-side-motion';

/** The content's fade-in while the pane goes to its whole width and back: shorter than the edge's slide. */
const PANE_FADE_MS = 180;

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

/** How far the pane's left edge is clipped now (the running slide's frame): `inset(0 0 0 <x>px …)`. */
const clipLeft = (el: HTMLElement): number => {
  const match = /inset\(([^)]*)\)/.exec(getComputedStyle(el).clipPath);
  const parts = match?.[1]?.split(/\s+/).filter((x) => x.endsWith('px')) ?? [];
  return px(parts[3] ?? parts[1] ?? parts[0] ?? '0');
};

/** The pane's visible box: its left `left` px clipped, its corners as the pane's own. */
const inset = (left: number): string => `inset(0px 0px 0px ${left}px round 16px)`;

/**
 * «Во всю ширину» and back. What is in the pane is laid out once, at its new width, and stands where it ends
 * up from the first frame: the pane keeps the wider of the two boxes for the slide, its content right-aligned in it,
 * and only its left edge moves, by clip-path on the compositor (no width per frame, no refit of a terminal, a page or
 * a design on the way). The content comes in with a short fade, as its first frame is the new layout seen through
 * the old edge. The chat is not touched: a maximised pane covers it rather than narrows it. A toggle in the middle
 * starts from where the edge is then. The phone and reduced motion skip it.
 */
export const usePaneMax = (pane: RefObject<HTMLElement | null>, maxed: boolean, mounted: boolean): void => {
  const previous = useRef(maxed);
  const laidOut = useRef(0);
  const running = useRef<{ edge: Animation; fade: Animation | null; box: number } | null>(null);

  // The width the layout last gave the pane: where a slide starts from, as the layout is already the new one by then.
  useLayoutEffect(() => {
    const el = pane.current;
    if (!mounted || !el) return;
    laidOut.current = el.getBoundingClientRect().width;
    if (typeof ResizeObserver === 'undefined') return;
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
    const now = running.current;
    // A slide in progress: the visible width is the box less what its edge clips now.
    const from = now ? now.box - clipLeft(el) : laidOut.current;
    now?.edge.cancel();
    now?.fade?.cancel();
    running.current = null;
    el.style.removeProperty('width');
    el.removeAttribute('data-resizing');
    if (phone() || reduced() || from === 0) return;
    const to = el.getBoundingClientRect().width;
    if (Math.abs(from - to) < 1) return;
    const box = Math.max(from, to);
    el.style.setProperty('width', `${box}px`);
    el.style.setProperty('--hx-inner-w', `${to}px`);
    el.setAttribute('data-resizing', 'true');
    const timing: KeyframeAnimationOptions = { duration: SIDE_MOTION_MS, easing: SIDE_MOTION_EASING };
    // Held at its end until the box is given back, so no frame shows the wider box unclipped.
    const edge = el.animate([{ clipPath: inset(box - from) }, { clipPath: inset(box - to) }], {
      ...timing,
      fill: 'forwards',
    });
    const inner = el.querySelector<HTMLElement>(':scope > .hx-browser');
    const fade =
      inner?.animate([{ opacity: 0 }, { opacity: 1 }], { duration: PANE_FADE_MS, easing: 'ease-out' }) ??
      null;
    const slide = { edge, fade, box };
    running.current = slide;
    void edge.finished.then(
      () => {
        if (running.current !== slide) return;
        running.current = null;
        el.style.removeProperty('width');
        el.removeAttribute('data-resizing');
        edge.cancel();
        laidOut.current = to;
      },
      () => undefined,
    );
  }, [maxed, pane]);

  useLayoutEffect(
    () => () => {
      running.current?.edge.cancel();
      running.current?.fade?.cancel();
      running.current = null;
    },
    [],
  );
};
