// Annotating a picked piece: a pill «Annotate» over the picked words that, on a press, grows into a compact card — the
// words on a soft plate, a field for the remark that grows with its text, a round arrow — and folds away on Esc or a
// press elsewhere. The pill and the card are one box whose width, height and corners move together (no jump), it
// stands over the words (under them when there is no room above) and never leaves the screen. It knows nothing of
// where the words come from or where the annotation goes: the host gives the words and their place and takes the remark.
import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import { AnnotateField, AnnotateQuote, AnnotateSend, clipQuote } from '@/next/ui/annotate/parts';
import {
  ANNOTATE_MARGIN,
  placeAnnotate,
  type IAnnotatePlaced,
  type IAnnotateSpot,
} from '@/next/ui/annotate/place';
import { usePresence } from '@/next/ui/presence';
import '@/next/styles/annotate.css';

export const ANNOTATE_EXIT_MS = 200;
const MORPH_MS = 320;
const CARD_W = 340;
const PILL_H = 36;
const QUOTE_MAX = 160;

export interface IAnnotateTarget extends IAnnotateSpot {
  /** The words annotated. */
  quote: string;
  /** The card is open (the pill has grown); a pill otherwise. */
  expanded: boolean;
}

interface IAnnotateProps {
  /** What is annotated and where it stands; null folds the box away (it leaves with an animation). */
  target: IAnnotateTarget | null;
  /** A touch screen has its own menu over the words: the pill goes under them. */
  preferBelow?: boolean;
  onExpand: () => void;
  /** The remark, or null for the words alone. */
  onSubmit: (note: string | null) => void;
  /** Esc or a press elsewhere. */
  onDismiss: () => void;
}

const Box = ({
  target,
  state,
  preferBelow,
  onExpand,
  onSubmit,
  onDismiss,
}: IAnnotateProps & { target: IAnnotateTarget; state: 'open' | 'closing' }) => {
  const root = useRef<HTMLDivElement>(null);
  const pillText = useRef<HTMLSpanElement>(null);
  const card = useRef<HTMLDivElement>(null);
  const field = useRef<HTMLTextAreaElement>(null);
  const side = useRef<IAnnotatePlaced['side'] | null>(null);
  const [pillW, setPillW] = useState(0);
  const [cardH, setCardH] = useState(0);
  const [morphing, setMorphing] = useState(false);
  const [seen, setSeen] = useState(target.expanded);
  const { expanded } = target;
  const [shownSide, setShownSide] = useState<IAnnotatePlaced['side']>('above');

  // The box changes size and corners together for a moment after it opens or folds: the transition lives only then.
  if (expanded !== seen) {
    setSeen(expanded);
    setMorphing(true);
  }
  useEffect(() => {
    if (!morphing) return;
    const timer = window.setTimeout(() => setMorphing(false), MORPH_MS);
    return () => window.clearTimeout(timer);
  }, [morphing, expanded]);

  useLayoutEffect(() => {
    if (pillText.current) setPillW(Math.ceil(pillText.current.offsetWidth) + 2 * 14 + 2);
  }, []);
  useLayoutEffect(() => {
    const el = card.current;
    if (!el) return;
    const measure = () => setCardH(el.offsetHeight);
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  // Where it stands, written into the box's custom properties.
  useLayoutEffect(() => {
    const el = root.current;
    if (!el || pillW === 0) return;
    const width = window.innerWidth;
    const placed = placeAnnotate({
      x: target.x,
      top: target.top,
      bottom: target.bottom,
      viewportW: width,
      viewportH: window.innerHeight,
      pillW,
      cardW: Math.min(CARD_W, width - 2 * ANNOTATE_MARGIN),
      cardH,
      pillH: PILL_H,
      expanded,
      preferBelow: preferBelow ?? false,
      side: expanded ? side.current : null,
    });
    side.current = placed.side;
    setShownSide(placed.side);
    el.style.setProperty('--ax-pill', `${placed.pillLeft}px`);
    el.style.setProperty('--ax-card', `${placed.cardLeft}px`);
    el.style.setProperty('--ay', `${placed.y}px`);
    el.style.setProperty('--aw-pill', `${pillW}px`);
    el.style.setProperty('--aw-card', `${Math.min(CARD_W, width - 2 * ANNOTATE_MARGIN)}px`);
    el.style.setProperty('--ah-card', `${cardH}px`);
  }, [target.x, target.top, target.bottom, expanded, pillW, cardH, preferBelow]);

  useEffect(() => {
    if (expanded) field.current?.focus({ preventScroll: true });
  }, [expanded]);

  const submit = useCallback(() => {
    const note = field.current?.value.trim() ?? '';
    onSubmit(note === '' ? null : note);
  }, [onSubmit]);

  // A press elsewhere or Esc folds it away.
  useEffect(() => {
    if (state === 'closing') return;
    const away = (event: PointerEvent) => {
      if (!root.current?.contains(event.target as Node)) onDismiss();
    };
    const esc = (event: KeyboardEvent) => {
      if (event.key !== 'Escape' || event.defaultPrevented) return;
      onDismiss();
    };
    document.addEventListener('pointerdown', away, true);
    document.addEventListener('keydown', esc);
    return () => {
      document.removeEventListener('pointerdown', away, true);
      document.removeEventListener('keydown', esc);
    };
  }, [state, onDismiss]);

  return (
    <div
      ref={root}
      className="hx-annot"
      data-state={state}
      data-expanded={expanded}
      data-morph={morphing}
      data-side={shownSide}
      role="dialog"
      aria-label={t('next.annotate.label')}
    >
      <div className="hx-annot__pill" aria-hidden={expanded} inert={expanded}>
        <button
          type="button"
          className="hx-annot__pillbtn"
          // The press must not take the picked words' selection away.
          onPointerDown={(event) => event.preventDefault()}
          onClick={onExpand}
        >
          <span ref={pillText} className="hx-annot__pilltext">
            <Icon name="message-square-text" size="sm" />
            {t('next.annotate.button')}
          </span>
        </button>
      </div>
      <div ref={card} className="hx-annot__card" aria-hidden={!expanded} inert={!expanded}>
        <AnnotateQuote text={clipQuote(target.quote, QUOTE_MAX)} />
        <div className="hx-annot__row">
          <AnnotateField
            inputRef={field}
            placeholder={t('next.annotate.placeholder')}
            onEnter={submit}
            onEscape={onDismiss}
          />
          <AnnotateSend label={t('next.annotate.send')} onClick={submit} />
        </div>
      </div>
    </div>
  );
};

const Annotate = (props: IAnnotateProps) => {
  const { value, state } = usePresence(props.target, ANNOTATE_EXIT_MS);
  if (!value) return null;
  return <Box {...props} target={value} state={state} />;
};

export default Annotate;
