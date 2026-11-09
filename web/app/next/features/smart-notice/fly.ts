// The black shape flowing between the smart notice and a drop in the chat's column: one black layer over the window,
// as big as both places together, shows only the window of the one it leaves and springs (clip-path and its corners)
// into the window of the one it comes to. What each place holds flies inside it (copies taken by `snap`): the one it
// leaves shrinks towards the new place and goes into a blur, the one it comes to grows out of the old place and comes
// out of one, so the shape never flies empty. Only clip-path, transform, opacity and filter move. The places themselves
// stay hidden while it flies (the caller hides them) and show when it lands, under the same picture.

/** response 0.5 s, damping 0.8: 1.4 % over, 680 ms (the same family as the notice's --sn-spring). */
const FLOW =
  'linear(0, 0.05, 0.1583, 0.292, 0.4297, 0.5582, 0.6707, 0.7644, 0.8392, 0.8985, 0.9405, 0.9702, 0.9902, 1.0028, ' +
  '1.01, 1.0134, 1.0143, 1.0136, 1.012, 1.0101, 1.0082, 1.0063, 1.0047, 1.0034, 1.0023, 1.0015, 1)';
export const FLY_MS = 680;

export interface IFlyEnd {
  rect: DOMRect;
  radius: number;
  /** What the place shows, copied by `snap` (a copy, so the place may change or go while it flies). */
  view?: HTMLElement | null;
}

/** A still copy of what a place shows now, to fly with the shape. */
export const snap = (el: Element | null): HTMLElement | null => {
  if (!(el instanceof HTMLElement)) return null;
  const copy = el.cloneNode(true) as HTMLElement;
  copy.removeAttribute('data-arriving');
  copy.removeAttribute('data-ghost');
  copy.removeAttribute('id');
  for (const named of copy.querySelectorAll('[id]')) named.removeAttribute('id');
  return copy;
};

const place = (view: HTMLElement, at: DOMRect, left: number, top: number) => {
  Object.assign(view.style, {
    position: 'absolute',
    left: `${at.left - left}px`,
    top: `${at.top - top}px`,
    width: `${at.width}px`,
    height: `${at.height}px`,
    margin: '0',
    maxWidth: 'none',
    animation: 'none',
    transition: 'none',
    boxShadow: 'none',
    transformOrigin: '0 0',
    pointerEvents: 'none',
  });
};

/** Where `a` stands and how big it is, seen from `b`'s place: the transform that lays `b` over `a`. */
const over = (a: DOMRect, b: DOMRect) => {
  const scale = a.width / b.width;
  return `translate(${a.left - b.left}px, ${a.top - b.top}px) scale(${scale})`;
};

const reduced = () => window.matchMedia('(prefers-reduced-motion: reduce)').matches;

/** Flies the black shape from one place to another; resolves when it has landed (at once without motion). */
export const fly = (from: IFlyEnd, to: IFlyEnd): Promise<void> => {
  if (reduced() || from.rect.width === 0 || to.rect.width === 0) return Promise.resolve();
  const left = Math.min(from.rect.left, to.rect.left);
  const top = Math.min(from.rect.top, to.rect.top);
  const right = Math.max(from.rect.right, to.rect.right);
  const bottom = Math.max(from.rect.bottom, to.rect.bottom);
  const layer = document.createElement('div');
  layer.className = 'hx-sn-fly';
  layer.setAttribute('aria-hidden', 'true');
  Object.assign(layer.style, {
    left: `${left}px`,
    top: `${top}px`,
    width: `${right - left}px`,
    height: `${bottom - top}px`,
  });
  const inset = ({ rect, radius }: IFlyEnd) =>
    `inset(${rect.top - top}px ${right - rect.right}px ${bottom - rect.bottom}px ${rect.left - left}px round ${radius}px)`;
  const views: HTMLElement[] = [];
  if (from.view) {
    place(from.view, from.rect, left, top);
    views.push(from.view);
  }
  if (to.view) {
    place(to.view, to.rect, left, top);
    views.push(to.view);
  }
  layer.append(...views);
  document.body.append(layer);
  const timing = { duration: FLY_MS, easing: FLOW, fill: 'forwards' as const };
  // The one it leaves: goes towards the new place and into a blur in the first part of the flight.
  if (from.view) {
    from.view.animate([{ transform: 'none' }, { transform: over(to.rect, from.rect) }], timing);
    from.view.animate(
      [
        { opacity: 1, filter: 'blur(0px)' },
        { opacity: 0, filter: 'blur(6px)', offset: 0.4 },
        { opacity: 0, filter: 'blur(6px)' },
      ],
      { duration: FLY_MS, fill: 'forwards' },
    );
  }
  // The one it comes to: grows out of the old place and out of a blur while the shape opens round it.
  if (to.view) {
    to.view.animate([{ transform: over(from.rect, to.rect) }, { transform: 'none' }], timing);
    to.view.animate(
      [
        { opacity: 0, filter: 'blur(6px)' },
        { opacity: 0, filter: 'blur(6px)', offset: 0.22 },
        { opacity: 1, filter: 'blur(0px)', offset: 0.7 },
        { opacity: 1, filter: 'blur(0px)' },
      ],
      { duration: FLY_MS, fill: 'forwards' },
    );
  }
  const run = layer.animate([{ clipPath: inset(from) }, { clipPath: inset(to) }], timing);
  return run.finished
    .catch(() => undefined)
    .then(() => {
      // The place it came to shows under it now, the same picture; the layer goes in a blink.
      const out = layer.animate([{ opacity: 1 }, { opacity: 0 }], { duration: 120, fill: 'forwards' });
      void out.finished.catch(() => undefined).then(() => layer.remove());
    });
};
