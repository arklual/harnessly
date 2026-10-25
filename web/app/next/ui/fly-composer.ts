// The field «flies» between the start page and Designly (the design's flyComposer): a copy of the old field is laid
// over the screen, the route changes under it, and the copy travels and resizes into the frame of the new field while
// its content fades and the new field shows up. The copy is styled by .hx-ghost through custom properties.

/** The fields that fly: the start page's and Designly's. */
export const FLY_FIELD = { home: '.hx-stage .hx-composer', designly: '.hx-dl__composer' } as const;

const FLY_MS = 640;
const FLY_EASE = 'cubic-bezier(.2, .8, .2, 1)';
/** How long the new field is waited for after the route changes, in frames (about half a second). */
const LAND_FRAMES = 30;

const box = (r: DOMRect) => ({
  left: `${r.left}px`,
  top: `${r.top}px`,
  width: `${r.width}px`,
  height: `${r.height}px`,
});

/** The copy of the field, put where the field is now; its look is carried by custom properties. */
const ghostOf = (from: HTMLElement, r: DOMRect) => {
  const cs = getComputedStyle(from);
  const ghost = from.cloneNode(true) as HTMLElement;
  ghost.querySelectorAll('[id]').forEach((node) => node.removeAttribute('id'));
  // A clone does not carry what was typed: the text goes in by hand.
  const typed = from.querySelector('textarea')?.value ?? '';
  const area = ghost.querySelector('textarea');
  if (area) area.value = typed;
  ghost.classList.add('hx-ghost');
  const set = (name: string, value: string) => ghost.style.setProperty(name, value);
  set('--fly-x', `${r.left}px`);
  set('--fly-y', `${r.top}px`);
  set('--fly-w', `${r.width}px`);
  set('--fly-h', `${r.height}px`);
  set('--fly-bg', cs.backgroundColor);
  set('--fly-bw', cs.borderTopWidth);
  set('--fly-bc', cs.borderTopColor);
  set('--fly-r', cs.borderTopLeftRadius);
  set('--fly-shadow', cs.boxShadow);
  return ghost;
};

const travel = (ghost: HTMLElement, to: HTMLElement, r0: DOMRect, radius0: string) => {
  const r1 = to.getBoundingClientRect();
  // Designly's own entrance is swapped for the staggered one that waits for the field to land.
  const page = to.closest<HTMLElement>('.hx-dl');
  if (page) page.dataset.fly = 'true';
  ghost.animate(
    [
      { ...box(r0), borderRadius: radius0 },
      { ...box(r1), borderRadius: getComputedStyle(to).borderTopLeftRadius },
    ],
    { duration: FLY_MS, easing: FLY_EASE, fill: 'forwards' },
  );
  for (const child of ghost.children) {
    child.animate([{ opacity: 1 }, { opacity: 0 }], {
      duration: FLY_MS * 0.4,
      easing: 'ease-out',
      fill: 'forwards',
    });
  }
  to.animate([{ opacity: 0 }, { opacity: 1 }], {
    duration: 280,
    delay: FLY_MS - 220,
    easing: 'ease-out',
    fill: 'backwards',
  });
  const gone = () => (ghost.parentElement ?? ghost).remove();
  ghost
    .animate([{ opacity: 1 }, { opacity: 0 }], { duration: 200, delay: FLY_MS - 80, fill: 'forwards' })
    .finished.then(gone, gone);
};

/** Changes the route with `navigate` and flies the field `from` into the field `to` of the screen it brings. */
export const flyComposer = (from: string, to: string, navigate: () => void) => {
  const source = document.querySelector<HTMLElement>(from);
  const root = source?.closest<HTMLElement>('.hx-root');
  const r0 = source?.getBoundingClientRect();
  if (
    !source ||
    !root ||
    !r0 ||
    r0.width === 0 ||
    window.matchMedia('(prefers-reduced-motion: reduce)').matches
  ) {
    navigate();
    return;
  }
  const ghost = ghostOf(source, r0);
  const radius0 = getComputedStyle(source).borderTopLeftRadius;
  // The field's insides are styled within its dock: the copy flies in a bare dock of its own so they keep their places.
  const host = document.createElement('div');
  host.className = 'hx-dock hx-ghost-host';
  host.appendChild(ghost);
  // The copy covers the old field before it is replaced, so there is no frame without a field.
  root.appendChild(host);
  navigate();
  let frames = 0;
  const land = () => {
    const target = document.querySelector<HTMLElement>(to);
    if (target && target.getBoundingClientRect().width > 0) {
      travel(ghost, target, r0, radius0);
      return;
    }
    frames += 1;
    if (frames < LAND_FRAMES) requestAnimationFrame(land);
    else host.remove();
  };
  requestAnimationFrame(land);
};
