// The hands-on tour (HRN-792): a lesson is a list of steps the person does themselves in the real product, on the
// demo projects every library starts with (HRN-813). A step points at a real element and is counted when the product
// shows it was done, not when a «Next» is pressed; a step the product cannot see ends with the person's «Done».

/** What a step can read to tell whether it was done: the page and what the features said happened. */
export interface ITourEnv {
  /** The address of the page: its path and query. */
  path: string;
  /** Finds a real element of the page; null when it is not there. */
  find: (selector: string) => Element | null;
  /** How many elements of the page match. */
  count: (selector: string) => number;
  /** The signals the features gave since the step began (see tourSignal). */
  signals: ReadonlySet<string>;
  /** What the features know for the whole page (tourFact): `agent:none` when no agent can answer here. */
  facts: ReadonlySet<string>;
}

/** A step that needs one before it: while the test fails, the tour goes back to that step and says why. */
export interface ITourNeed {
  test: (env: ITourEnv) => boolean;
  /** The id of the step that gives it. */
  back: string;
}

/** What the person has to do before the step itself can be done, when that takes more than the step's own click. */
export interface ITourStage {
  /** Its text: `next.tour.stage.<id>` (and `.action` for the button). */
  id: string;
  /** The element to ring now. */
  target: readonly string[];
  /** A button of the card that does it for the person. */
  action?: 'clear' | 'restoreDemo';
  /** Words for the text. */
  vars?: Record<string, string>;
}

export interface ITourStep {
  /** Its texts: `<track>.<id>.title` and `<track>.<id>.text` in next-tour.json. */
  id: string;
  /** The element the step is about; the first selector found is used. Empty: the card stands in the middle. */
  target: readonly string[];
  /** Whether the person did it; absent, the card asks for «Done». */
  done?: (env: ITourEnv) => boolean;
  /** The product may not see it (a board without the page's bridge): «Done» stays offered beside the check. */
  manual?: boolean;
  /** Nothing to do, only to see: the card offers «Next» instead of «Done». */
  look?: boolean;
  /** Text the step puts into the design chat's field, so the person only sends it. */
  prompt?: string;
  /** Text the step puts into the field of a picked block's note. */
  note?: string;
  /** An element inside the design's page (a selector there): its bridge tells where it is and the ring goes on it. */
  frame?: string;
  /** Where the step is done (a test of the path): elsewhere the card says so and offers the way back. */
  where?: RegExp;
  /** The path the way back goes to; absent, the person gets there by the step before (the card says which). */
  back?: string;
  /** The step makes no sense here (no agent to answer): it is passed over. */
  skip?: (env: ITourEnv) => boolean;
  needs?: ITourNeed;
  /** The way to the step's element when the product hides it (a folder, a filter): null when it is in sight. */
  stage?: (env: ITourEnv) => ITourStage | null;
  /** What the card must not cover beside the ring (the whole field whose button is ringed). */
  avoid?: readonly string[];
  /** A request of the step goes on the cheapest model the machine has (Haiku), unless the person picked one. */
  cheap?: boolean;
  /** The design's chat is docked beside the page while the step is on, not folded over it. */
  sideChat?: boolean;
}

export interface ITourTrack {
  id: string;
  steps: readonly ITourStep[];
}

export interface IRect {
  x: number;
  y: number;
  w: number;
  h: number;
}

export interface IPlace {
  x: number;
  y: number;
  side: TSide;
}

export const SIDE = {
  right: 'right',
  left: 'left',
  below: 'below',
  above: 'above',
  center: 'center',
} as const;
export type TSide = (typeof SIDE)[keyof typeof SIDE];

const GAP = 14;
const EDGE = 12;

const overlap = (a: IRect, b: IRect) =>
  Math.max(0, Math.min(a.x + a.w, b.x + b.w) - Math.max(a.x, b.x)) *
  Math.max(0, Math.min(a.y + a.h, b.y + b.h) - Math.max(a.y, b.y));

const clamp = (v: number, lo: number, hi: number) => Math.min(Math.max(v, lo), Math.max(lo, hi));

const union = (rs: readonly IRect[]): IRect => {
  const x = Math.min(...rs.map((r) => r.x));
  const y = Math.min(...rs.map((r) => r.y));
  return {
    x,
    y,
    w: Math.max(...rs.map((r) => r.x + r.w)) - x,
    h: Math.max(...rs.map((r) => r.y + r.h)) - y,
  };
};

/** The four places beside a box, the right first. */
const besides = (r: IRect, card: { w: number; h: number }): IPlace[] => {
  const midY = r.y + r.h / 2 - card.h / 2;
  const midX = r.x + r.w / 2 - card.w / 2;
  return [
    { x: r.x + r.w + GAP, y: midY, side: SIDE.right },
    { x: r.x - card.w - GAP, y: midY, side: SIDE.left },
    { x: midX, y: r.y + r.h + GAP, side: SIDE.below },
    { x: midX, y: r.y - card.h - GAP, side: SIDE.above },
  ];
};

/**
 * Where the card of size card stands next to target in a view of size view: never over what it explains when there is
 * any room, then off the menus and panels open around it, then as close to its side as it fits. Beside the target
 * first, then beside the target with its menus, then the corners of the view. No target: the middle.
 */
export const placeCard = (
  target: IRect | null,
  card: { w: number; h: number },
  view: { w: number; h: number },
  keep: readonly IRect[] = [],
): IPlace => {
  if (!target) return { x: (view.w - card.w) / 2, y: (view.h - card.h) / 2, side: SIDE.center };
  const corners: IPlace[] = [
    { x: view.w - card.w - EDGE, y: view.h - card.h - EDGE, side: SIDE.center },
    { x: EDGE, y: view.h - card.h - EDGE, side: SIDE.center },
    { x: view.w - card.w - EDGE, y: EDGE, side: SIDE.center },
    { x: EDGE, y: EDGE, side: SIDE.center },
  ];
  const tries = [
    ...besides(target, card),
    ...(keep.length ? besides(union([target, ...keep]), card) : []),
    ...corners,
  ];
  let best: IPlace | null = null;
  let bestCost = Infinity;
  tries.forEach((p, i) => {
    const x = clamp(p.x, EDGE, view.w - card.w - EDGE);
    const y = clamp(p.y, EDGE, view.h - card.h - EDGE);
    const box = { x, y, w: card.w, h: card.h };
    // The target hidden weighs most, the open menus next, then how far it moved from its side and how far it is.
    const cost =
      overlap(box, target) * 1000 +
      keep.reduce((sum, r) => sum + overlap(box, r), 0) * 10 +
      Math.abs(x - p.x) +
      Math.abs(y - p.y) +
      i * 4;
    if (cost < bestCost) {
      bestCost = cost;
      best = { x, y, side: p.side };
    }
  });
  return best ?? { x: EDGE, y: EDGE, side: SIDE.center };
};
