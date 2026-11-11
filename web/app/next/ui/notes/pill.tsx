// The compact button over picked words, «Annotate» (and, where the host has it, «Ask aside» beside it): it stands over the words (under them when there is no room above or
// on a touch screen, whose own menu is over them), never leaves the screen, and leaves with an animation. A press is the
// host's: it puts a numbered mark on the words and opens the row of its comment (ui/notes/notes.tsx). It knows
// nothing of where the words come from. With `keys`, A presses «Annotate» and S «Ask aside» while it is up.
import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import { placeAnnotate, type IAnnotatePlaced, type IAnnotateSpot } from '@/next/ui/notes/place';
import { usePresence } from '@/next/ui/presence';
import '@/next/styles/notes-shared.css';

export const ANNOTATE_EXIT_MS = 160;
const PILL_H = 32;

interface IAnnotatePillProps {
  /** Where the picked words stand; null folds the button away. */
  target: IAnnotateSpot | null;
  /** A touch screen has its own menu over the words: the button goes under them. */
  preferBelow?: boolean;
  onPress: () => void;
  /** «Ask aside»: the words go to the side chat as a question to write; absent, the button is not there. */
  onAside?: () => void;
  /** A and S press the buttons while the pill is up (the host's words are not in a field). */
  keys?: boolean;
  /** Esc or a press elsewhere. */
  onDismiss: () => void;
}

const KEY_ANNOTATE = 'KeyA';
const KEY_ASIDE = 'KeyS';

/** A bare letter typed where words are typed is a letter, not a press of the pill. */
const typing = (target: EventTarget | null): boolean =>
  target instanceof Element && target.closest('input, textarea, select, [contenteditable="true"]') !== null;

const Pill = ({
  target,
  state,
  preferBelow = false,
  onPress,
  onAside,
  keys = false,
  onDismiss,
}: IAnnotatePillProps & { target: IAnnotateSpot; state: 'open' | 'closing' }) => {
  const root = useRef<HTMLDivElement>(null);
  const [side, setSide] = useState<IAnnotatePlaced['side']>('above');

  useLayoutEffect(() => {
    const el = root.current;
    if (!el) return;
    const width = el.offsetWidth;
    const placed = placeAnnotate({
      ...target,
      viewportW: window.innerWidth,
      viewportH: window.innerHeight,
      pillW: width,
      cardW: width,
      cardH: PILL_H,
      pillH: PILL_H,
      expanded: false,
      preferBelow,
      side: null,
    });
    setSide(placed.side);
    el.style.setProperty('--ax-pill', `${placed.pillLeft}px`);
    el.style.setProperty('--ay', `${placed.y}px`);
  }, [target, preferBelow]);

  // A press elsewhere or Esc folds it away.
  useEffect(() => {
    if (state === 'closing') return;
    const away = (event: PointerEvent) => {
      if (!root.current?.contains(event.target as Node)) onDismiss();
    };
    const esc = (event: KeyboardEvent) => {
      if (event.defaultPrevented) return;
      if (event.key === 'Escape') {
        onDismiss();
        return;
      }
      if (!keys || event.altKey || event.ctrlKey || event.metaKey || event.shiftKey || typing(event.target))
        return;
      // By the key's place, so a Russian layout presses them too.
      const presses: Record<string, (() => void) | undefined> = {
        [KEY_ANNOTATE]: onPress,
        [KEY_ASIDE]: onAside,
      };
      const press = presses[event.code];
      if (!press) return;
      event.preventDefault();
      press();
    };
    document.addEventListener('pointerdown', away, true);
    document.addEventListener('keydown', esc);
    return () => {
      document.removeEventListener('pointerdown', away, true);
      document.removeEventListener('keydown', esc);
    };
  }, [state, onDismiss, onPress, onAside, keys]);

  return (
    <div ref={root} className="hx-note__pill" data-state={state} data-side={side}>
      <button
        type="button"
        className="hx-note__pillbtn"
        // The press must not take the picked words' selection away.
        aria-keyshortcuts={keys ? 'A' : undefined}
        onPointerDown={(event) => event.preventDefault()}
        onClick={onPress}
      >
        <Icon name="message-square-text" size="sm" />
        {t('next.annotate.button')}
        {keys && <kbd className="hx-note__pillkey">A</kbd>}
      </button>
      {onAside && (
        <button
          type="button"
          className="hx-note__pillbtn"
          aria-keyshortcuts={keys ? 'S' : undefined}
          onPointerDown={(event) => event.preventDefault()}
          onClick={onAside}
        >
          <Icon name="messages-square" size="sm" />
          {t('next.annotate.aside')}
          {keys && <kbd className="hx-note__pillkey">S</kbd>}
        </button>
      )}
    </div>
  );
};

const AnnotatePill = (props: IAnnotatePillProps) => {
  const { value, state } = usePresence(props.target, ANNOTATE_EXIT_MS);
  if (!value) return null;
  return <Pill {...props} target={value} state={state} />;
};

export default AnnotatePill;
