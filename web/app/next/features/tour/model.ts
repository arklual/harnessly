// The hands-on tour: a lesson is a list of steps the person does themselves in the real product, on the
// demo projects every library starts with. A step points at a real element and is counted when the product
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

/**
 * Where the card of size card stands next to target in a view of size view: the side where it covers the least of
 * what it explains (the target and the menus and panels open around it), the right first. No target: the middle.
 */
export const placeCard = (
  target: IRect | null,
  card: { w: number; h: number },
  view: { w: number; h: number },
  keep: readonly IRect[] = [],
): IPlace => {
  if (!target) return { x: (view.w - card.w) / 2, y: (view.h - card.h) / 2, side: SIDE.center };
  const midY = target.y + target.h / 2 - card.h / 2;
  const midX = target.x + target.w / 2 - card.w / 2;
  const tries: IPlace[] = [
    { x: target.x + target.w + GAP, y: midY, side: SIDE.right },
    { x: target.x - card.w - GAP, y: midY, side: SIDE.left },
    { x: midX, y: target.y + target.h + GAP, side: SIDE.below },
    { x: midX, y: target.y - card.h - GAP, side: SIDE.above },
  ];
  const shield = [target, ...keep];
  let best: IPlace | null = null;
  let bestCost = Infinity;
  for (const p of tries) {
    const x = clamp(p.x, EDGE, view.w - card.w - EDGE);
    const y = clamp(p.y, EDGE, view.h - card.h - EDGE);
    const box = { x, y, w: card.w, h: card.h };
    // What it hides of the target and the open menus, then how far it had to move from its side to fit.
    const cost =
      shield.reduce((sum, r) => sum + overlap(box, r), 0) * 10 + Math.abs(x - p.x) + Math.abs(y - p.y);
    if (cost < bestCost) {
      bestCost = cost;
      best = { x, y, side: p.side };
    }
  }
  return best ?? { x: EDGE, y: EDGE, side: SIDE.center };
};
