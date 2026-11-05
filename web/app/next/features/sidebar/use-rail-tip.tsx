import { useRef, type PointerEvent } from 'react';

/** The sidebar is folded to its rail: the labels are gone and the names come as tips. */
export const isRail = (el: Element | null) => el?.closest('.hx-app')?.getAttribute('data-side') === 'closed';

/** Folded to the rail, or open over the window only while the pointer rests on it (the peek). */
export const isFolded = (el: Element | null) => {
  const app = el?.closest('.hx-app');
  return app?.getAttribute('data-side') === 'closed' || app?.hasAttribute('data-peek') === true;
};

/** What a click on the folded sidebar means by itself; anything else there is the sidebar's empty space. */
const OWN_CLICK =
  'button, a, input, textarea, select, label, [role="button"], [role="menuitem"], [contenteditable], [data-drag], .hx-item, .hx-user, .hx-railtip';

/**
 * A click on the empty space of the folded (or peeked) sidebar opens it for good, as in ChatGPT: the whole rail is the
 * toggle, not only the small icon over the logo.
 */
export const opensRail = (target: EventTarget | null): boolean =>
  target instanceof Element && isFolded(target) && target.closest(OWN_CLICK) === null;

/**
 * On the rail, the name of the item under the pointer (its `data-tip`, and `data-kbd` for a shortcut) shows to the
 * right of it. The tip is moved and filled straight in the DOM, so hovering down the rail renders nothing.
 */
const useRailTip = () => {
  const tipRef = useRef<HTMLDivElement>(null);
  const textRef = useRef<HTMLSpanElement>(null);
  const kbdRef = useRef<HTMLElement>(null);

  const hide = () => tipRef.current?.setAttribute('data-show', 'false');

  const onPointerOver = (event: PointerEvent<HTMLElement>) => {
    const tip = tipRef.current;
    const target = (event.target as Element).closest<HTMLElement>('[data-tip]');
    if (!tip || event.pointerType !== 'mouse' || !target || !isRail(target)) {
      hide();
      return;
    }
    const box = target.getBoundingClientRect();
    const kbd = target.dataset.kbd ?? '';
    if (textRef.current) textRef.current.textContent = target.dataset.tip ?? '';
    if (kbdRef.current) kbdRef.current.textContent = kbd;
    tip.style.setProperty('--x', `${box.right + 10}px`);
    tip.style.setProperty('--y', `${box.top + box.height / 2}px`);
    tip.setAttribute('data-kbd', String(kbd !== ''));
    tip.setAttribute('data-show', 'true');
  };

  return {
    handlers: { onPointerOver, onPointerLeave: hide, onPointerDown: hide },
    tip: (
      <div ref={tipRef} className="hx-railtip" role="tooltip" aria-hidden="true" data-show="false">
        <span ref={textRef} className="hx-railtip__text" />
        <kbd ref={kbdRef} className="hx-railtip__kbd" />
      </div>
    ),
  };
};

export default useRailTip;
