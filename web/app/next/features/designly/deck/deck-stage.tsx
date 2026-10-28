// The stage (the board's .pr-stage and .pr-slot): the slide on screen is its board in a frame drawn at the board's own
// size and scaled to the slot, so the board lays out exactly as it will be shown, printed and recorded. Turning to
// another slide loads its board in a second frame over the first and fades it in when it has loaded, then drops the
// old one. The slot's width is the stylesheet's (`--sw`); a ResizeObserver hands the scale to CSS as `--deck-k`.
import { useEffect, useLayoutEffect, useRef, useState, type ReactNode, type RefObject } from 'react';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import { PRESENCE, usePresence } from '@/next/ui/presence';
import { deckScale } from '@/next/features/designly/deck/layout';
import { DECK_HOLD, type TDeckHold } from '@/next/features/designly/deck/deck-making';

/** The crossfade, ms (the stage's 480 ms motion). */
export const DECK_FADE_MS = 480;

/** The placeholder's fade, ms. */
const HOLD_EXIT_MS = 320;

const HOLD_TEXT: Record<TDeckHold, { title: string; hint: string }> = {
  making: { title: 'next.deck.stage.making', hint: 'next.deck.stage.makingHint' },
  none: { title: 'next.deck.stage.none', hint: 'next.deck.stage.noneHint' },
  blank: { title: 'next.deck.stage.blank', hint: 'next.deck.stage.blankHint' },
};

/** The placeholder over the slot; it fades out as the slide arrives. */
const DeckHold = ({ hold }: { hold: TDeckHold | null }) => {
  const shown = usePresence(hold, HOLD_EXIT_MS);
  if (shown.value === null) return null;
  const text = HOLD_TEXT[shown.value];
  return (
    <div
      className="hx-deck-hold"
      data-hold={shown.value}
      data-closing={shown.state === PRESENCE.closing ? '1' : undefined}
      role="status"
    >
      {shown.value === DECK_HOLD.making && (
        <div className="hx-deck-hold__sk" aria-hidden="true">
          <span className="hx-sk" />
          <span className="hx-sk" />
          <span className="hx-sk" />
        </div>
      )}
      <b>{t(text.title)}</b>
      <small>{t(text.hint)}</small>
    </div>
  );
};

/** A loaded board that does not say it is ready (an old board, a failed script) is shown after this long, ms. */
const PAINT_WAIT_MS = 700;

interface IFrame {
  src: string;
  /** The frame's document has loaded. */
  loaded: boolean;
  /** The board has drawn itself (its `ready`), or did not say so in time: the frame fades in over the one under it. */
  shown: boolean;
}

export interface IDeckStageProps {
  /** The frame address of the board on screen; null while the slot opens. */
  src: string | null;
  /** The board's size, px. */
  w: number;
  h: number;
  n: number;
  /** What stands in for the slide; null shows the slide. */
  hold: TDeckHold | null;
  /** The board on screen's frame, for the bridge. */
  frameRef: RefObject<HTMLIFrameElement | null>;
  /** A frame finished loading its board. */
  onLoad: () => void;
  /** Moves each time the board on screen says it is ready (drawn): its frame fades in then, not on a blank page. */
  painted: number;
  error: string | null;
  onReload: () => void;
  /** Laid over the slide, in the slot: the block frame, the toolbar, pins. */
  overlay?: ReactNode;
  /** Under the slot: notes and pager. */
  below?: ReactNode;
  /** The stage's own pointer handlers (a phone's long press, the pointer of the show). */
  stageRef?: RefObject<HTMLDivElement | null>;
}

const DeckStage = ({
  src,
  w,
  h,
  n,
  hold,
  frameRef,
  onLoad,
  painted,
  error,
  onReload,
  overlay,
  below,
  stageRef,
}: IDeckStageProps) => {
  const slotRef = useRef<HTMLDivElement>(null);
  const [frames, setFrames] = useState<IFrame[]>([]);
  // A new address puts a new frame on top; the ones under it stay until it has loaded and faded in.
  if (src !== null && frames[frames.length - 1]?.src !== src) {
    setFrames([
      ...frames.filter((f) => f.shown && f.src !== src).slice(-1),
      { src, loaded: false, shown: false },
    ]);
  }
  const top = frames[frames.length - 1];
  const settled = frames.length > 1 && top?.shown;
  const showTop = () =>
    setFrames((list) => list.map((f, i) => (i === list.length - 1 && f.loaded ? { ...f, shown: true } : f)));
  // The board on screen said it is drawn.
  const [seenPaint, setSeenPaint] = useState(painted);
  if (seenPaint !== painted) {
    setSeenPaint(painted);
    if (top?.loaded && !top.shown) showTop();
  }
  const waiting = top !== undefined && top.loaded && !top.shown;
  useEffect(() => {
    if (!waiting) return;
    const timer = setTimeout(showTop, PAINT_WAIT_MS);
    return () => clearTimeout(timer);
  }, [waiting]);
  useEffect(() => {
    if (!settled) return;
    const timer = setTimeout(() => setFrames((list) => list.slice(-1)), DECK_FADE_MS);
    return () => clearTimeout(timer);
  }, [settled]);

  useLayoutEffect(() => {
    const slot = slotRef.current;
    if (!slot) return;
    slot.style.setProperty('--bw', String(w));
    slot.style.setProperty('--bh', String(h));
    const fit = () => slot.style.setProperty('--deck-k', String(deckScale(slot.clientWidth, w)));
    fit();
    const watch = new ResizeObserver(fit);
    watch.observe(slot);
    return () => watch.disconnect();
  }, [w, h]);

  return (
    <div ref={stageRef} className="hx-deck__stage">
      <div ref={slotRef} className="hx-deck__slot">
        <div
          className="hx-deck__slide"
          data-hold={hold ?? undefined}
          aria-label={hold === null ? t('next.deck.stage.slide', { n }) : undefined}
        >
          {frames.map((frame, i) => {
            const isTop = i === frames.length - 1;
            return (
              <iframe
                key={frame.src}
                ref={isTop ? frameRef : undefined}
                className="hx-deck__frame"
                data-on={frame.shown ? '1' : '0'}
                src={frame.src}
                title={t('next.deck.stage.slide', { n })}
                tabIndex={isTop ? 0 : -1}
                aria-hidden={isTop ? undefined : true}
                allow="clipboard-write"
                onLoad={() => {
                  // The first frame has nothing under it: it shows at once; a later one waits for its board's ready.
                  setFrames((list) =>
                    list.map((f) =>
                      f.src === frame.src ? { ...f, loaded: true, shown: list.length === 1 } : f,
                    ),
                  );
                  if (isTop) onLoad();
                }}
              />
            );
          })}
          <DeckHold hold={error === null ? hold : null} />
          {error !== null && (
            <div className="hx-deck__failed" role="alert">
              <span>{t('next.deck.stage.loadFailed')}</span>
              <small>{error}</small>
              <button type="button" className="hx-btn" data-size="sm" onClick={onReload}>
                <Icon name="rotate-ccw" size="sm" />
                <span className="hx-btn__text">{t('next.deck.stage.retry')}</span>
              </button>
            </div>
          )}
        </div>
        {overlay}
      </div>
      {below}
    </div>
  );
};

export default DeckStage;
