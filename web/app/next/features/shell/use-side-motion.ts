import { useLayoutEffect, useRef, type RefObject } from 'react';
import { PHONE_QUERY } from '@/next/features/shell/gestures';

/** The slide's length and curve, easing out (`--hx-side-dur`, `--hx-side-ease` in styles/shell.css). */
export const SIDE_MOTION_MS = 240;
export const SIDE_MOTION_EASING = 'cubic-bezier(0.22, 1, 0.36, 1)';
const DURATION_MS = SIDE_MOTION_MS;
const EASING = SIDE_MOTION_EASING;
/** The window's corner (`.hx-main`). */
const RADIUS_PX = 22;

export interface ISideMotionRefs {
  app: RefObject<HTMLElement | null>;
  /** The window: its box is the clip of the motion. */
  main: RefObject<HTMLElement | null>;
  /** What is inside the window: it slides with the window's edge. */
  body: RefObject<HTMLElement | null>;
  /** The strip of tabs over the window: it slides with the window's edge. */
  strip?: RefObject<HTMLElement | null>;
}

/** What hangs on the window's right edge stays where the layout put it: the header's buttons and the right pane. */
const GLUE_END = '.hx-header__end, .hx-pane, .hx-side';
/** What is centred in the window goes half the way, as it would with the window growing from its middle. */
const GLUE_MID = '.hx-feed__inner, .hx-dock';
/** Positioned against the screen: a sliding body would carry them off their place, so the slide is skipped. */
const SCREEN_BOUND = '.hx-sidechat, .hx-sidesnap, .hx-selbar, .hx-gallery, .hx-imgview, .hx-ghost-host';

const px = (value: string): number => Number.parseFloat(value) || 0;

/** How far the body is now from where the layout put it, in px (the running slide's frame, 0 at rest). */
const currentShift = (body: HTMLElement): number => {
  const transform = getComputedStyle(body).transform;
  if (!transform || transform === 'none') return 0;
  return new DOMMatrixReadOnly(transform).m41;
};

/**
 * The sidebar folds to its rail and opens again on the compositor. The grid takes the new columns at once, so what is
 * inside the window is laid out once; the window then slides from where its edge was to where it is: its content by
 * transform, its frame by clip-path (rounded as the window is), both from one start and one curve, so the chat moves
 * with the sidebar's edge and nothing is laid out or painted on the way. What hangs on the window's right edge is held
 * in place and what is centred goes half the way, as in the design where the window grows with its content re-flowing.
 * A toggle in the middle of a slide starts from where the edge is now. Reduced motion and the phone's drawer (own
 * motion) skip it.
 */
const useSideMotion = ({ app, main, body, strip }: ISideMotionRefs, rail: boolean): void => {
  const previous = useRef(rail);
  const running = useRef<Animation[]>([]);

  useLayoutEffect(() => {
    if (previous.current === rail) return;
    previous.current = rail;
    const appEl = app.current;
    const mainEl = main.current;
    const bodyEl = body.current;
    if (!appEl || !mainEl || !bodyEl) return;
    const shift = currentShift(bodyEl);
    for (const animation of running.current) animation.cancel();
    running.current = [];
    appEl.removeAttribute('data-move');
    if (matchMedia(PHONE_QUERY).matches || matchMedia('(prefers-reduced-motion: reduce)').matches) return;
    const styles = getComputedStyle(appEl);
    const span = px(styles.getPropertyValue('--hx-sidebar-w')) - px(styles.getPropertyValue('--hx-rail-w'));
    // The edge was `shift` off the old place; the new place is `span` to the left (folded) or right (opened).
    const from = shift + (rail ? span : -span);
    if (from === 0 || bodyEl.querySelector(SCREEN_BOUND)) return;
    const timing: KeyframeAnimationOptions = { duration: DURATION_MS, easing: EASING };
    const glue = (selector: string, share: number): Animation[] =>
      Array.from(bodyEl.querySelectorAll<HTMLElement>(selector), (el) =>
        el.animate([{ translate: `${-from * share}px 0` }, { translate: '0px 0' }], timing),
      );
    const stripEl = strip?.current;
    const slides = [
      ...(stripEl
        ? [
            stripEl.animate(
              [{ transform: `translateX(${from}px)` }, { transform: 'translateX(0px)' }],
              timing,
            ),
          ]
        : []),
      ...glue(GLUE_END, 1),
      ...glue(GLUE_MID, 0.5),
      bodyEl.animate([{ transform: `translateX(${from}px)` }, { transform: 'translateX(0px)' }], timing),
      mainEl.animate(
        [
          { clipPath: `inset(0 0 0 ${from}px round ${RADIUS_PX}px)` },
          { clipPath: `inset(0 0 0 0px round ${RADIUS_PX}px)` },
        ],
        timing,
      ),
    ];
    running.current = slides;
    appEl.setAttribute('data-move', 'on');
    void Promise.allSettled(slides.map((slide) => slide.finished)).then(() => {
      if (running.current !== slides) return;
      running.current = [];
      appEl.removeAttribute('data-move');
    });
  }, [rail, app, main, body, strip]);

  useLayoutEffect(
    () => () => {
      for (const animation of running.current) animation.cancel();
      running.current = [];
    },
    [],
  );
};

export default useSideMotion;
