// A sent message travels instead of appearing: the text leaves the field and the bubble grows out of the place
// it left, in the feed or in the queue's row, and a message handed over from the queue flies from its row. The pieces
// that do not know each other meet here: the field says where the text was (launchFrom), the dock binds it to the
// message's client id (bindLaunch), a queue row says where it was when it left (noteExit), the feed claims the place of
// a bubble that is new (claimOrigin), and the field claims the place of a text that came back from the queue (claimEdit).
// A place is only valid for a moment: nothing that comes later flies from a stale one. Motion is WAAPI on transform and
// opacity only; with «reduce motion» or without WAAPI nothing here moves anything.

export const FLIGHT_MS = 320;
/** The design's ease-out (--hx-ease-out) for what is made in script. */
export const FLIGHT_EASE = 'cubic-bezier(0.16, 1, 0.3, 1)';
/** How long a recorded place may wait for its bubble. */
export const ORIGIN_TTL_MS = 2000;
/** How long a text taken out of the queue may wait for the field to show it (the runtime answers first). */
export const EDIT_TTL_MS = 5000;

/** A rectangle in the viewport. */
export interface IBox {
  left: number;
  top: number;
  width: number;
  height: number;
}

export const boxOf = (element: Element): IBox => {
  const rect = element.getBoundingClientRect();
  return { left: rect.left, top: rect.top, width: rect.width, height: rect.height };
};

/** False for «reduce motion»: nothing flies, the thing is simply there. */
export const motionOn = (): boolean =>
  typeof window !== 'undefined' &&
  typeof window.matchMedia === 'function' &&
  !window.matchMedia('(prefers-reduced-motion: reduce)').matches;

/** The element can be animated from script and the person has not asked for stillness. */
export const canAnimate = (element: Element): boolean => typeof element.animate === 'function' && motionOn();

interface IPlace {
  box: IBox;
  at: number;
  /** The words that were there. */
  text: string;
}

const places = new Map<string, IPlace>();
/** The messages with a row in the queue's stack right now (rows that are leaving count: they are still there). */
const standing = new Set<string>();
let launch: IPlace | null = null;
let edit: IPlace | null = null;

const fresh = (place: IPlace, ttl: number): boolean => Date.now() - place.at < ttl;

const prune = () => {
  for (const [id, place] of places) if (!fresh(place, ORIGIN_TTL_MS)) places.delete(id);
};

/** The field has handed its text over: where it stood. The dock binds it to the message it made (or nothing does). */
export const launchFrom = (box: IBox, text: string) => {
  launch = { box, at: Date.now(), text };
};

/** The message the last launch made: its bubble will fly from where the text was. */
export const bindLaunch = (clientId: string) => {
  if (launch && fresh(launch, ORIGIN_TTL_MS)) places.set(clientId, launch);
  launch = null;
  prune();
};

/** A queue row that goes (handed over, sent now): where it stood, for the bubble that takes its place in the feed. */
export const noteExit = (clientId: string, box: IBox, text: string) => {
  places.set(clientId, { box, at: Date.now(), text });
  prune();
};

/** The message has a row in the queue's stack: if its bubble comes to the feed it comes from that row. */
export const rowStands = (clientId: string): (() => void) => {
  standing.add(clientId);
  return () => void standing.delete(clientId);
};

/** Whether a bubble of this message will fly (its place is known, or it comes from the queue). Uses nothing up. */
export const hasOrigin = (clientId: string): boolean => {
  const place = places.get(clientId);
  return standing.has(clientId) || (!!place && fresh(place, ORIGIN_TTL_MS));
};

/** The place the message's bubble flies from; used up. */
export const claimOrigin = (clientId: string): IBox | null => {
  const place = places.get(clientId);
  places.delete(clientId);
  return place && fresh(place, ORIGIN_TTL_MS) ? place.box : null;
};

/** The pencil of a queue row: where its text stood, for the field that is about to get it back. */
export const markEdit = (box: IBox, text: string) => {
  edit = { box, at: Date.now(), text };
};

/** Where the text that came back from the queue was, if the field's new content is it; used up. */
export const claimEdit = (value: string): IBox | null => {
  const place = edit;
  if (!place || !fresh(place, EDIT_TTL_MS)) {
    edit = null;
    return null;
  }
  // Attachments only: there are no words to look for, the first thing to arrive is it.
  if (place.text.trim() && !value.includes(place.text.trim())) return null;
  edit = null;
  return place.box;
};

/** Drops every place (a test's, and a chat that is left). */
export const forgetFlights = () => {
  places.clear();
  standing.clear();
  launch = null;
  edit = null;
};

/**
 * Where a copy that stands at `to` starts when it leaves `from` and how it gets there: the text is where it was (left
 * edges, centres) and the bubble gathers itself around it, a little smaller at first. The last frame has no transform:
 * the copy ends exactly on `to`, and the ease-out never carries it past.
 */
export const flightFrames = (from: IBox, to: IBox): Keyframe[] => {
  const dx = from.left - to.left;
  const dy = from.top + from.height / 2 - (to.top + to.height / 2);
  return [
    { transform: `translate(${dx}px, ${dy}px) scale(0.96)`, opacity: 0, offset: 0 },
    { opacity: 1, offset: 0.35 },
    { transform: 'none', opacity: 1, offset: 1 },
  ];
};

/** The copies on their way: each one looks again where its bubble stands. */
const following = new Set<() => void>();

/**
 * The layout under the copies changed (the field shrank once its text left, a picture took its size, the feed kept its
 * bottom): they aim at where their bubbles stand now, in this frame, before it is painted.
 */
export const retargetFlights = () => {
  for (const follow of following) follow();
};

const SAME_PX = 0.5;
const differs = (a: IBox, b: IBox): boolean =>
  Math.abs(a.left - b.left) > SAME_PX ||
  Math.abs(a.top - b.top) > SAME_PX ||
  Math.abs(a.width - b.width) > SAME_PX ||
  Math.abs(a.height - b.height) > SAME_PX;

const standAt = (copy: HTMLElement, to: IBox) => {
  copy.style.setProperty('--fly-x', `${to.left}px`);
  copy.style.setProperty('--fly-y', `${to.top}px`);
  copy.style.setProperty('--fly-w', `${to.width}px`);
};

/**
 * A copy of `source` flies over the screen from `from` to where the real one stands, while the real one waits
 * invisible in that place and is there when the copy lands. `place` says where that is; it is asked again every frame
 * and whenever the feed's layout changes, because the place moves while the copy is on its way (the field shrinks
 * after its text has left and the feed keeps its bottom, a picture in the bubble loads): a place measured once is
 * stale by the time the copy arrives. Returns false when nothing flies.
 */
export const flyCopy = (source: HTMLElement, from: IBox, place: () => IBox): boolean => {
  const root = source.closest<HTMLElement>('.hx-root');
  let to = place();
  if (!root || !canAnimate(source) || to.width === 0) return false;
  const copy = source.cloneNode(true) as HTMLElement;
  copy.querySelectorAll('[id]').forEach((node) => node.removeAttribute('id'));
  copy.setAttribute('aria-hidden', 'true');
  copy.classList.add('hx-flight');
  standAt(copy, to);
  root.appendChild(copy);

  const flight = copy.animate(flightFrames(from, to), { duration: FLIGHT_MS, easing: FLIGHT_EASE });
  // The real bubble is not drawn while the copy is on its way.
  source.animate([{ opacity: 0 }, { opacity: 0 }], { duration: FLIGHT_MS });

  let frame = 0;
  const follow = () => {
    if (!source.isConnected) return;
    const next = place();
    if (next.width === 0 || !differs(next, to)) return;
    to = next;
    standAt(copy, to);
    (flight.effect as KeyframeEffect | null)?.setKeyframes(flightFrames(from, to));
  };
  const tick = () => {
    follow();
    frame = requestAnimationFrame(tick);
  };
  frame = requestAnimationFrame(tick);
  following.add(follow);
  const land = () => {
    cancelAnimationFrame(frame);
    following.delete(follow);
    copy.remove();
  };
  flight.finished.then(land, land);
  return true;
};

/**
 * The content of a scroller moved up by `delta` at once (a bubble came to the bottom, the live line appeared); it slides
 * down from there to its place instead. Several of these add up.
 */
export const slideFrom = (element: HTMLElement, delta: number) => {
  if (!canAnimate(element)) return;
  element.animate([{ transform: `translateY(${delta}px)` }, { transform: 'translateY(0)' }], {
    duration: FLIGHT_MS,
    easing: FLIGHT_EASE,
    composite: 'add',
  });
};
