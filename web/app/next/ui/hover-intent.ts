// Hover intent without a library: what opens on hover stays while the pointer travels to it, and goes when the pointer
// turns away. The «safe triangle» of Amazon's mega-menu (Floating UI calls it safePolygon): when the pointer leaves the
// trigger, the zone from where it left to the far element's corners (plus the gap between the two) is safe while the
// pointer keeps getting closer; leaving the zone, turning away or resting outside both closes. Geometry and the state
// machine are plain functions over rects and points; the DOM wiring is use-hover-intent.ts.

export interface IPoint {
  x: number;
  y: number;
}

export interface IRect {
  left: number;
  top: number;
  right: number;
  bottom: number;
}

export const HOVER = {
  /** A card opens this long after the pointer comes onto its trigger; focus opens at once. */
  openDelay: 120,
  /** A hint waits longer: it is not what the person came for. */
  tipDelay: 450,
  /** The fallback: once the pointer left with no move after it, the card goes in this long. */
  closeDelay: 320,
  /** The pointer rests this long outside both elements: it is not going anywhere, the card goes. */
  restMs: 360,
  /** A hint that closed this recently makes the next one open at once (moving along a row of buttons). */
  warmMs: 400,
  /** Distance the pointer may lose on one move and still count as heading to the element (hand jitter). */
  slackPx: 1.5,
  /** The zone's edges are this generous. */
  edgePx: 1,
} as const;

// ── Geometry ────────────────────────────────────────────

export const rectOf = (el: Element): IRect => {
  const r = el.getBoundingClientRect();
  return { left: r.left, top: r.top, right: r.right, bottom: r.bottom };
};

export const cornersOf = (r: IRect): IPoint[] => [
  { x: r.left, y: r.top },
  { x: r.right, y: r.top },
  { x: r.right, y: r.bottom },
  { x: r.left, y: r.bottom },
];

export const pointInRect = (p: IPoint, r: IRect, pad = 0): boolean =>
  p.x >= r.left - pad && p.x <= r.right + pad && p.y >= r.top - pad && p.y <= r.bottom + pad;

const cross = (o: IPoint, a: IPoint, b: IPoint) => (a.x - o.x) * (b.y - o.y) - (a.y - o.y) * (b.x - o.x);

/** The convex hull of the points (monotone chain), counter-clockwise in screen terms; collinear points dropped. */
export const convexHull = (points: readonly IPoint[]): IPoint[] => {
  const sorted = [...points].sort((a, b) => a.x - b.x || a.y - b.y);
  if (sorted.length < 3) return sorted;
  const lower: IPoint[] = [];
  sorted.forEach((p) => {
    while (lower.length >= 2 && cross(lower[lower.length - 2], lower[lower.length - 1], p) <= 0) lower.pop();
    lower.push(p);
  });
  const upper: IPoint[] = [];
  [...sorted].reverse().forEach((p) => {
    while (upper.length >= 2 && cross(upper[upper.length - 2], upper[upper.length - 1], p) <= 0) upper.pop();
    upper.push(p);
  });
  return [...lower.slice(0, -1), ...upper.slice(0, -1)];
};

/**
 * Whether the point is inside a convex polygon (either winding), its edges counted inside and widened by `pad`
 * pixels. A degenerate polygon (fewer than three corners) holds nothing.
 */
export const pointInPolygon = (p: IPoint, polygon: readonly IPoint[], pad = 0): boolean => {
  if (polygon.length < 3) return false;
  let area = 0;
  polygon.forEach((a, i) => {
    const b = polygon[(i + 1) % polygon.length];
    area += a.x * b.y - b.x * a.y;
  });
  if (area === 0) return false;
  const sign = Math.sign(area);
  return polygon.every((a, i) => {
    const b = polygon[(i + 1) % polygon.length];
    const length = Math.hypot(b.x - a.x, b.y - a.y);
    if (length === 0) return true;
    // Signed distance from the edge, positive inside.
    return (sign * cross(a, b, p)) / length >= -pad;
  });
};

/** The band between two rects that are apart on one axis (the gap a pointer crosses), null if they touch or overlap. */
export const gapRect = (a: IRect, b: IRect): IRect | null => {
  const left = Math.max(a.left, b.left);
  const right = Math.min(a.right, b.right);
  const top = Math.max(a.top, b.top);
  const bottom = Math.min(a.bottom, b.bottom);
  // Apart vertically, overlapping horizontally: the band across the shared width.
  if (left < right && b.top >= a.bottom) return { left, right, top: a.bottom, bottom: b.top };
  if (left < right && a.top >= b.bottom) return { left, right, top: b.bottom, bottom: a.top };
  if (top < bottom && b.left >= a.right) return { left: a.right, right: b.left, top, bottom };
  if (top < bottom && a.left >= b.right) return { left: b.right, right: a.left, top, bottom };
  return null;
};

/** The safe zone of a pointer that left `source` at `from`, heading for `target`: the triangle (hull) and the gap. */
export const safeZone = (from: IPoint, source: IRect, target: IRect): IPoint[][] => {
  const zones = [convexHull([from, ...cornersOf(target)])];
  const gap = gapRect(source, target);
  if (gap) zones.push(cornersOf(gap));
  return zones;
};

export const inZone = (
  p: IPoint,
  zones: readonly (readonly IPoint[])[],
  pad: number = HOVER.edgePx,
): boolean => zones.some((zone) => pointInPolygon(p, zone, pad));

export const distanceToRect = (p: IPoint, r: IRect): number =>
  Math.hypot(Math.max(r.left - p.x, 0, p.x - r.right), Math.max(r.top - p.y, 0, p.y - r.bottom));

/** The move from `prev` to `next` keeps the pointer heading for the rect: it got no farther (bar the jitter). */
export const movingToward = (prev: IPoint, next: IPoint, r: IRect, slack: number = HOVER.slackPx): boolean =>
  distanceToRect(next, r) <= distanceToRect(prev, r) + slack;

// ── Placement: a hint never covers what it explains ─────

export const SIDE = { top: 'top', bottom: 'bottom' } as const;
export type TSide = (typeof SIDE)[keyof typeof SIDE];

export const ALIGN = { start: 'start', center: 'center', end: 'end' } as const;
export type TAlign = (typeof ALIGN)[keyof typeof ALIGN];

export interface IPlaceInput {
  anchor: IRect;
  size: { width: number; height: number };
  viewport: { width: number; height: number };
  side?: TSide;
  align?: TAlign;
  offset?: number;
  pad?: number;
}

export interface IPlacement {
  x: number;
  y: number;
  side: TSide;
}

const clamp = (value: number, min: number, max: number) => Math.max(min, Math.min(value, Math.max(min, max)));

/**
 * Where a floating element goes beside its anchor: on the preferred side (the top by default) when it fits there, on
 * the other one when it does not, and on the roomier one when neither fits; never over the anchor. Along the side it
 * is aligned to the anchor and kept inside the viewport.
 */
export const placeFloating = ({
  anchor,
  size,
  viewport,
  side = SIDE.top,
  align = ALIGN.center,
  offset = 10,
  pad = 8,
}: IPlaceInput): IPlacement => {
  const roomAbove = anchor.top - offset - pad;
  const roomBelow = viewport.height - anchor.bottom - offset - pad;
  const fits = { [SIDE.top]: roomAbove >= size.height, [SIDE.bottom]: roomBelow >= size.height } as const;
  const other = side === SIDE.top ? SIDE.bottom : SIDE.top;
  let chosen: TSide = side;
  if (!fits[side] && fits[other]) chosen = other;
  if (!fits[side] && !fits[other]) chosen = roomAbove >= roomBelow ? SIDE.top : SIDE.bottom;

  const xs = {
    [ALIGN.start]: anchor.left,
    [ALIGN.center]: (anchor.left + anchor.right) / 2 - size.width / 2,
    [ALIGN.end]: anchor.right - size.width,
  } as const;
  const x = clamp(xs[align], pad, viewport.width - size.width - pad);
  const y = chosen === SIDE.top ? anchor.top - offset - size.height : anchor.bottom + offset;
  return { x, y, side: chosen };
};

/** How far a laid-out element must move sideways, and how short it must be, to stay inside the viewport. */
export const fitInViewport = (
  r: IRect,
  viewport: { width: number; height: number },
  pad = 8,
): { dx: number; maxHeight: number | null } => {
  let dx = 0;
  if (r.right > viewport.width - pad) dx = viewport.width - pad - r.right;
  if (r.left + dx < pad) dx = pad - r.left;
  const height = r.bottom - r.top;
  const over = Math.max(0, pad - r.top) + Math.max(0, r.bottom - (viewport.height - pad));
  return { dx, maxHeight: over > 0 ? Math.max(0, height - over) : null };
};

// ── Warmth: a row of hints reads as one ────────────────

export interface IHoverWarmth {
  /** A hint of the group is open, or one closed a moment ago: the next one skips its delay. */
  isWarm: () => boolean;
  shown: () => void;
  hidden: () => void;
}

export const createWarmth = (ms: number = HOVER.warmMs): IHoverWarmth => {
  let open = 0;
  let closedAt = Number.NEGATIVE_INFINITY;
  return {
    isWarm: () => open > 0 || Date.now() - closedAt <= ms,
    shown: () => {
      open += 1;
    },
    hidden: () => {
      open = Math.max(0, open - 1);
      closedAt = Date.now();
    },
  };
};

/** The hints of the whole interface: the Tooltip component and the CSS hints (.hx-tipped) share it. */
export const TIP_WARMTH = createWarmth();

// ── The state machine ───────────────────────────────────

export const HOVER_STATE = {
  closed: 'closed',
  /** The pointer is on the trigger, the open delay runs. */
  opening: 'opening',
  open: 'open',
  /** The pointer left one element and is (maybe) on its way to the other. */
  leaving: 'leaving',
} as const;
export type THoverState = (typeof HOVER_STATE)[keyof typeof HOVER_STATE];

export const HOVER_PART = { trigger: 'trigger', floating: 'floating' } as const;
export type THoverPart = (typeof HOVER_PART)[keyof typeof HOVER_PART];

export interface IHoverRects {
  trigger: IRect | null;
  floating: IRect | null;
  /** Other live elements the pointer may head for and stay (a menu out of the card, a modal). */
  kept?: readonly IRect[];
}

export interface IHoverIntentOptions {
  onChange: (open: boolean) => void;
  rects: () => IHoverRects;
  openDelay?: number;
  closeDelay?: number;
  restMs?: number;
  /** False for a hint: nothing to reach inside, leaving the trigger closes at once. */
  interactive?: boolean;
  /** A group whose warmth skips the open delay. */
  warmth?: IHoverWarmth;
}

interface ITarget {
  rect: IRect;
  zones: IPoint[][];
}

export interface IHoverIntent {
  readonly state: THoverState;
  readonly open: boolean;
  readonly pinned: boolean;
  enter: (part: THoverPart) => void;
  leave: (part: THoverPart, point: IPoint) => void;
  /** A pointer move anywhere; `kept`: it is over an element that keeps the card (a menu, a modal). */
  move: (point: IPoint, kept?: boolean) => void;
  focus: () => void;
  blur: () => void;
  /** A click on the trigger: pins an open card (opens a closed one pinned), closes a pinned one. */
  toggle: () => void;
  /** Opens at once, unpinned (a retarget). */
  show: () => void;
  close: () => void;
  /** A nested card is open: this one stays while it is. */
  hold: () => void;
  release: () => void;
  destroy: () => void;
}

export const createHoverIntent = (options: IHoverIntentOptions): IHoverIntent => {
  const {
    onChange,
    rects,
    openDelay = HOVER.openDelay,
    closeDelay = HOVER.closeDelay,
    restMs = HOVER.restMs,
    interactive = true,
    warmth,
  } = options;

  let state: THoverState = HOVER_STATE.closed;
  let pinned = false;
  let focused = false;
  let holds = 0;
  const hovered = { [HOVER_PART.trigger]: false, [HOVER_PART.floating]: false };
  let timer: ReturnType<typeof setTimeout> | undefined;
  let targets: ITarget[] = [];
  let last: IPoint | null = null;

  const stop = () => {
    clearTimeout(timer);
    timer = undefined;
  };
  const isOpen = () => state === HOVER_STATE.open || state === HOVER_STATE.leaving;

  const setOpen = () => {
    stop();
    const was = isOpen();
    state = HOVER_STATE.open;
    targets = [];
    if (!was) {
      warmth?.shown();
      onChange(true);
    }
  };

  const closeNow = () => {
    stop();
    const was = isOpen();
    state = HOVER_STATE.closed;
    pinned = false;
    targets = [];
    last = null;
    if (was) {
      warmth?.hidden();
      onChange(false);
    }
  };

  /** Something still keeps it: a pin, a nested card, keyboard focus inside, the pointer on it. */
  const kept = () => pinned || holds > 0 || focused || hovered.trigger || hovered.floating;

  const arm = (ms: number) => {
    stop();
    timer = setTimeout(() => {
      if (!kept()) closeNow();
    }, ms);
  };

  return {
    get state() {
      return state;
    },
    get open() {
      return isOpen();
    },
    get pinned() {
      return pinned;
    },

    enter(part) {
      hovered[part] = true;
      if (state === HOVER_STATE.leaving) {
        setOpen();
        return;
      }
      if (state !== HOVER_STATE.closed || part !== HOVER_PART.trigger) return;
      const delay = warmth?.isWarm() ? 0 : openDelay;
      if (delay <= 0) {
        setOpen();
        return;
      }
      state = HOVER_STATE.opening;
      stop();
      timer = setTimeout(setOpen, delay);
    },

    leave(part, point) {
      hovered[part] = false;
      if (state === HOVER_STATE.opening && !hovered.trigger) {
        stop();
        state = HOVER_STATE.closed;
        return;
      }
      if (state !== HOVER_STATE.open || kept()) return;
      if (!interactive) {
        closeNow();
        return;
      }
      const all = rects();
      const source = part === HOVER_PART.trigger ? all.trigger : all.floating;
      const toward = part === HOVER_PART.trigger ? all.floating : all.trigger;
      const aims = [toward, ...(all.kept ?? [])].filter((r): r is IRect => r !== null);
      targets = source ? aims.map((rect) => ({ rect, zones: safeZone(point, source, rect) })) : [];
      last = point;
      state = HOVER_STATE.leaving;
      arm(closeDelay);
    },

    move(point, overKept = false) {
      if (state !== HOVER_STATE.leaving) return;
      const prev = last ?? point;
      last = point;
      if (overKept) {
        arm(restMs);
        return;
      }
      // A release with no way laid out: only the timer decides.
      if (targets.length === 0) return;
      const heading = targets.some((t) => inZone(point, t.zones) && movingToward(prev, point, t.rect));
      if (!heading) {
        if (!kept()) closeNow();
        return;
      }
      arm(restMs);
    },

    focus() {
      focused = true;
      setOpen();
    },

    blur() {
      focused = false;
      if (isOpen() && !kept()) closeNow();
    },

    toggle() {
      if (pinned) {
        closeNow();
        return;
      }
      setOpen();
      pinned = true;
    },

    show() {
      setOpen();
    },

    close() {
      focused = false;
      closeNow();
    },

    hold() {
      holds += 1;
    },

    release() {
      holds = Math.max(0, holds - 1);
      if (holds > 0 || !isOpen() || kept()) return;
      targets = [];
      state = HOVER_STATE.leaving;
      arm(closeDelay);
    },

    destroy() {
      stop();
    },
  };
};
