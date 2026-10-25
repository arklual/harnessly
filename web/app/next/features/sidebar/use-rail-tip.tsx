import { useRef, type PointerEvent } from 'react';

/** The sidebar is folded to its rail: the labels are gone and the names come as tips. */
export const isRail = (el: Element | null) => el?.closest('.hx-app')?.getAttribute('data-side') === 'closed';

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
