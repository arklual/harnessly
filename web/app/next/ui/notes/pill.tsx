// The compact button over picked words, «Annotate»: it stands over the words (under them when there is no room above or
// on a touch screen, whose own menu is over them), never leaves the screen, and leaves with an animation. A press is the
// host's: it puts a numbered mark on the words and opens the row of its comment (ui/notes/notes.tsx). It knows
// nothing of where the words come from.
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
  /** Esc or a press elsewhere. */
  onDismiss: () => void;
}

const Pill = ({
  target,
  state,
  preferBelow = false,
  onPress,
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
      if (event.key === 'Escape' && !event.defaultPrevented) onDismiss();
    };
    document.addEventListener('pointerdown', away, true);
    document.addEventListener('keydown', esc);
    return () => {
      document.removeEventListener('pointerdown', away, true);
      document.removeEventListener('keydown', esc);
    };
  }, [state, onDismiss]);

  return (
    <div ref={root} className="hx-note__pill" data-state={state} data-side={side}>
      <button
        type="button"
        className="hx-note__pillbtn"
        // The press must not take the picked words' selection away.
        onPointerDown={(event) => event.preventDefault()}
        onClick={onPress}
      >
        <Icon name="message-square-text" size="sm" />
        {t('next.annotate.button')}
      </button>
    </div>
  );
};

const AnnotatePill = (props: IAnnotatePillProps) => {
  const { value, state } = usePresence(props.target, ANNOTATE_EXIT_MS);
  if (!value) return null;
  return <Pill {...props} target={value} state={state} />;
};

export default AnnotatePill;
