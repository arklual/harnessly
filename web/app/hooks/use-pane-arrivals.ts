import { useLayoutEffect, useRef, type RefObject } from 'react';
import { bornTabs, SPRING, SPRING_MS, takeArrivals } from '@/lib/hl-pane-arrival';
import type { IPaneTab } from '@/lib/hl-pane-tabs';

/** What a panel showed at its last look, kept across mounts: a panel slid away and back is not a panel of new tabs. */
const known = new Map<string, ReadonlySet<string>>();

const ENTER_WINDOW_MS = 150;
const ENTER_DELAY_MS = 220;

const reducedMotion = (): boolean =>
  typeof window.matchMedia === 'function' && window.matchMedia('(prefers-reduced-motion: reduce)').matches;

const animate = (node: Element, keyframes: Keyframe[], options: KeyframeAnimationOptions): void => {
  if (typeof node.animate === 'function') node.animate(keyframes, options);
};

/** A tab in front: it opens out of a point at its left edge, a light runs once along it, its content rises in. */
const playFront = (chip: HTMLElement, panel: HTMLElement | null, delay: number): void => {
  // A panel that slid in shows its skeleton with the slide; a panel already open lets the tab lead its contents.
  const riseAt = delay > 0 ? 40 : 90;
  animate(
    chip,
    [
      { clipPath: 'inset(0 100% 0 0 round 8px)', opacity: 0, transform: 'translateY(3px) scale(0.92)' },
      { clipPath: 'inset(0 0 0 0 round 8px)', opacity: 1, transform: 'none' },
    ],
    { duration: SPRING_MS, delay, easing: SPRING, fill: 'backwards' },
  );
  const glow = chip.querySelector('.tw-rpane-tab-glow');
  if (glow)
    animate(glow, [{ opacity: 0 }, { opacity: 1, offset: 0.3 }, { opacity: 0 }], {
      duration: 1300,
      delay: delay + 140,
      easing: 'cubic-bezier(0.33, 0, 0.2, 1)',
      fill: 'backwards',
    });
  const light = chip.querySelector('.tw-rpane-tab-light');
  if (light) {
    const timing = { duration: 950, delay: delay + 170, fill: 'backwards' } as const;
    animate(
      light,
      [{ opacity: 0 }, { opacity: 1, offset: 0.15 }, { opacity: 1, offset: 0.7 }, { opacity: 0 }],
      { ...timing, easing: 'linear' },
    );
    animate(light, [{ transform: 'translateX(-100%)' }, { transform: 'translateX(100%)' }], {
      ...timing,
      easing: 'cubic-bezier(0.45, 0, 0.2, 1)',
      pseudoElement: '::before',
    });
  }
  if (panel)
    animate(
      panel,
      [
        { opacity: 0, transform: 'translateY(12px)' },
        { opacity: 1, transform: 'none' },
      ],
      { duration: SPRING_MS, delay: riseAt, easing: SPRING, fill: 'backwards' },
    );
};

/** A tab that arrived behind the one the person reads: it takes no focus, it lights up twice so it is noticed. */
const playBehind = (chip: HTMLElement, delay: number): void => {
  animate(
    chip,
    [
      { opacity: 0, transform: 'translateY(3px) scale(0.92)' },
      { opacity: 1, transform: 'none' },
    ],
    { duration: SPRING_MS, delay, easing: SPRING, fill: 'backwards' },
  );
  const glow = chip.querySelector('.tw-rpane-tab-glow');
  if (glow)
    animate(
      glow,
      [
        { opacity: 0 },
        { opacity: 1, offset: 0.2 },
        { opacity: 0.15, offset: 0.5 },
        { opacity: 0.8, offset: 0.68 },
        { opacity: 0 },
      ],
      { duration: 1600, delay: delay + 200, easing: 'ease-in-out', fill: 'backwards' },
    );
  const ring = chip.querySelector('.tw-rpane-tab-unread');
  if (ring)
    animate(
      ring,
      [{ transform: 'scale(0)' }, { transform: 'scale(1.35)', offset: 0.45 }, { transform: 'scale(1)' }],
      { duration: 640, delay: delay + 220, easing: SPRING, fill: 'backwards' },
    );
};

/**
 * Plays the arrival of the tabs the panel did not show before: in front — born out of a point with a pass of
 * light and its content rising, behind — a soft glow twice. Neighbours slide over to make room instead of jumping.
 * Everything runs on the compositor (transform, opacity, a clip of one chip) and only for what changed.
 */
const usePaneArrivals = (
  paneKey: string | null,
  tabs: readonly IPaneTab[],
  activeId: string,
  strip: RefObject<HTMLElement | null>,
  /** The `useId` the panel builds its tab and panel ids from. */
  idBase: string,
): void => {
  const offsets = useRef(new Map<string, number>());
  // A panel that has just slid in lets the slide lead and its new tab follow.
  const mountedAt = useRef<number | null>(null);
  useLayoutEffect(() => {
    const root = strip.current;
    if (!root || !paneKey) return;
    mountedAt.current ??= performance.now();
    const enteringMs = performance.now() - mountedAt.current < ENTER_WINDOW_MS ? ENTER_DELAY_MS : 0;
    const ids = tabs.map((tab) => tab.id);
    const before = known.get(paneKey);
    known.set(paneKey, new Set(ids));
    const marked = takeArrivals(paneKey);
    const chips = new Map<string, HTMLElement>();
    root.querySelectorAll<HTMLElement>('[data-tab-id]').forEach((node) => {
      if (node.dataset.tabId) chips.set(node.dataset.tabId, node);
    });
    const left = root.getBoundingClientRect().left;
    const now = new Map<string, number>();
    chips.forEach((node, id) => now.set(id, node.getBoundingClientRect().left - left + root.scrollLeft));
    const previous = offsets.current;
    offsets.current = now;
    if (reducedMotion()) return;
    const born = new Set(bornTabs(ids, before, marked));
    if (born.size === 0) return;
    chips.forEach((node, id) => {
      if (born.has(id)) return;
      const was = previous.get(id);
      const is = now.get(id);
      if (was === undefined || is === undefined || Math.abs(was - is) < 0.5) return;
      animate(node, [{ transform: `translateX(${was - is}px)` }, { transform: 'none' }], {
        duration: SPRING_MS,
        easing: SPRING,
      });
    });
    const edge = root.parentElement?.querySelector('.tw-rpane-edge');
    if (edge && [...born].some((id) => id === activeId || marked.has(id))) {
      const timing = { duration: 1100, delay: enteringMs, fill: 'backwards' } as const;
      animate(
        edge,
        [{ opacity: 0 }, { opacity: 1, offset: 0.12 }, { opacity: 1, offset: 0.75 }, { opacity: 0 }],
        { ...timing, easing: 'linear' },
      );
      animate(edge, [{ transform: 'translateX(-100%)' }, { transform: 'translateX(100%)' }], {
        ...timing,
        easing: 'cubic-bezier(0.45, 0, 0.2, 1)',
        pseudoElement: '::before',
      });
    }
    for (const id of born) {
      const chip = chips.get(id);
      const tab = tabs.find((current) => current.id === id);
      if (!chip || !tab) continue;
      if (id === activeId) playFront(chip, document.getElementById(`${idBase}-panel-${tab.id}`), enteringMs);
      else playBehind(chip, enteringMs);
    }
  }, [paneKey, tabs, activeId, strip, idBase]);
};

export default usePaneArrivals;
