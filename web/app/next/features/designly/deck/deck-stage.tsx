// The stage (the board's .pr-stage and .pr-slot): the slide on screen is its board in a frame drawn at the board's own
// size and scaled to the slot, so the board lays out exactly as it will be shown, printed and recorded. Turning to
// another slide loads its board in a second frame over the first and fades it in when it has loaded, then drops the
// old one. The boards of the slides next to the one on screen are loaded ahead in frames nobody sees: a turn
// to one of them shows a frame that is already drawn, and the one just left stays loaded for the turn back. The frames
// keep their order in the page (an iframe moved in the DOM loads again), which of them is on top is the stylesheet's
// (`data-top`). The slot's width is the stylesheet's (`--sw`); a ResizeObserver hands the scale to CSS as `--deck-k`.
import { useEffect, useLayoutEffect, useRef, useState, type ReactNode, type RefObject } from 'react';
import { readDeckMessage } from '@/next/features/designly/deck/bridge';
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

/** A loaded board that does not say it is drawn (a failed script) is shown after this long, ms. The board says it
 *  only once its fonts, pictures and videos are in (client.js deckDrawn), which on a slow link takes seconds. */
const PAINT_WAIT_MS = 8000;

/** A slide that turns but is not drawn after this long shows the small spinner over the old one, ms. */
const PENDING_SHOW_MS = 300;

interface IFrame {
  src: string;
  /** The frame's document has loaded. */
  loaded: boolean;
  /** The board said it is drawn (its `ready`, after its fonts and pictures are in). */
  painted: boolean;
  /** Loaded and drawn, or did not say so in time: the frame fades in over the one under it. */
  shown: boolean;
  /** Put in the page as a neighbour of the slide on screen, before its turn. */
  ahead: boolean;
  /** What the board said when it was drawn while nobody looked (its size and steps), for its turn. */
  ready: IDeckReady | null;
}

/** The board's word that it is drawn. */
export interface IDeckReady {
  w: number;
  h: number;
  steps: number;
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
  /** The addresses of the boards next to the one on screen: loaded ahead, out of sight. */
  ahead?: readonly string[];
  /** A frame loaded ahead finished loading: the deck tells it its mode, so that it says when it is drawn. */
  onAhead?: (frame: HTMLIFrameElement, src: string) => void;
  /** A board that was drawn ahead comes on screen: what it said then, as if it said it now. */
  onReady?: (ready: IDeckReady) => void;
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
  ahead = [],
  onAhead,
  onReady,
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
  const fresh = (address: string, isAhead: boolean): IFrame => ({
    src: address,
    loaded: false,
    painted: false,
    shown: false,
    ahead: isAhead,
    ready: null,
  });
  // The slide that was on screen, which stays under the one fading in; null once that one is in.
  const [leaving, setLeaving] = useState<string | null>(null);
  const [seenSrc, setSeenSrc] = useState(src);
  const cur = src === null ? undefined : frames.find((f) => f.src === src);
  // The neighbours are loaded once the slide on screen is drawn, so that they do not take its network and processor.
  const aheadKey = ahead.join('\n');
  const onScreen = frames.some((f) => f.src === src && f.shown);
  // A new address brings its frame (or the one loaded ahead for it) on top; the one that was on top stays under it.
  let next = frames;
  if (src !== null && src !== seenSrc) {
    setSeenSrc(src);
    if (frames.find((f) => f.src === seenSrc)?.shown) setLeaving(seenSrc);
    next = cur
      ? frames.map((f) => (f.src === src ? { ...f, shown: f.loaded && f.painted } : f))
      : [...frames, fresh(src, false)];
  } else if (src !== null && !cur) {
    next = [...frames, fresh(src, false)];
  } else if (onScreen) {
    const missing = ahead.filter((a) => a !== src && !frames.some((f) => f.src === a));
    if (missing.length > 0) next = [...frames, ...missing.map((a) => fresh(a, true))];
  }
  if (next !== frames) setFrames(next);
  const top = cur;
  const settled =
    top?.shown === true && (leaving !== null || frames.some((f) => f.src !== src && !ahead.includes(f.src)));
  const showTop = () => {
    if (src !== null)
      setFrames((list) => list.map((f) => (f.src === src && f.loaded ? { ...f, shown: true } : f)));
  };
  // The board on screen said it is drawn: it shows once its document has loaded too.
  const [seenPaint, setSeenPaint] = useState(painted);
  if (seenPaint !== painted) {
    setSeenPaint(painted);
    if (top && !top.shown)
      setFrames((list) => list.map((f) => (f.src === src ? { ...f, painted: true, shown: f.loaded } : f)));
  }
  // The frames' windows by address: a board loaded ahead says it is drawn to nobody on the page's bridge.
  const elements = useRef(new Map<string, HTMLIFrameElement>());
  const srcNow = useRef(src);
  useEffect(() => {
    const listen = (event: MessageEvent) => {
      for (const [address, element] of elements.current) {
        if (event.source !== element.contentWindow) continue;
        const said = readDeckMessage(event.data);
        if (said?.k !== 'ready') return;
        const ready = { w: said.w, h: said.h, steps: said.steps };
        setFrames((list) =>
          list.map((f) =>
            f.src === address && f.src !== srcNow.current ? { ...f, painted: true, ready } : f,
          ),
        );
        return;
      }
    };
    window.addEventListener('message', listen);
    return () => window.removeEventListener('message', listen);
  }, []);
  // A slide turned to whose board was loaded and drawn ahead: its mode and size are told as the slide on screen's.
  const promoted = useRef<string | null>(null);
  useEffect(() => {
    srcNow.current = src;
    const frame = frames.find((f) => f.src === src);
    if (!frame?.ahead || !frame.loaded || promoted.current === src) return;
    promoted.current = src;
    onLoad();
    if (frame.ready) onReady?.(frame.ready);
    // The frames are read once per turn; a frame that loads after it tells its own load (below).
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [src]);
  // Nothing on screen yet (the first slide, or the slot opening): the slide's skeleton stands in.
  const blank =
    error === null && hold === null && !frames.some((f) => f.shown && (f.src === src || f.src === leaving));
  // A turn to a slide that takes a while: the old one stays, a small spinner says the new one is coming.
  const turning = error === null && hold === null && !blank && top !== undefined && !top.shown;
  // The slide (its address) whose turn has taken long enough for the spinner.
  const [slowFor, setSlowFor] = useState<string | null>(null);
  const turningSrc = turning ? top.src : null;
  useEffect(() => {
    if (turningSrc === null) return;
    const timer = setTimeout(() => setSlowFor(turningSrc), PENDING_SHOW_MS);
    return () => clearTimeout(timer);
  }, [turningSrc]);
  const slow = turningSrc !== null && slowFor === turningSrc;
  const waiting = top !== undefined && top.loaded && !top.shown;
  useEffect(() => {
    if (!waiting) return;
    const timer = setTimeout(showTop, PAINT_WAIT_MS);
    return () => clearTimeout(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [waiting, src]);
  // Once the new slide is in, the one under it goes (or, if it is a neighbour, stays loaded out of sight), and the
  // frames that are neither on screen nor a neighbour are dropped.
  useEffect(() => {
    if (!settled) return;
    const timer = setTimeout(() => {
      setLeaving(null);
      setFrames((list) => list.filter((f) => f.src === src || ahead.includes(f.src)));
    }, DECK_FADE_MS);
    return () => clearTimeout(timer);
    // `ahead` is read through its key: a new array of the same addresses is not a change.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [settled, src, aheadKey]);

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
          data-loading={blank ? true : undefined}
          aria-label={hold === null ? t('next.deck.stage.slide', { n }) : undefined}
        >
          {frames.map((frame) => {
            const isTop = frame.src === src;
            const under = frame.src === leaving && !isTop;
            return (
              <iframe
                key={frame.src}
                ref={(el) => {
                  if (el) elements.current.set(frame.src, el);
                  else elements.current.delete(frame.src);
                  if (isTop) frameRef.current = el;
                }}
                className="hx-deck__frame"
                data-on={(isTop && frame.shown) || under ? '1' : '0'}
                data-top={isTop ? '1' : '0'}
                data-quick={isTop && frame.ahead ? '1' : undefined}
                src={frame.src}
                title={t('next.deck.stage.slide', { n })}
                tabIndex={isTop ? 0 : -1}
                aria-hidden={isTop ? undefined : true}
                allow="clipboard-write"
                onLoad={(event) => {
                  // Every frame, the first too, waits for its board's word that it is drawn (or PAINT_WAIT_MS).
                  setFrames((list) =>
                    list.map((f) => (f.src === frame.src ? { ...f, loaded: true, shown: f.painted } : f)),
                  );
                  if (frame.src === srcNow.current) onLoad();
                  else onAhead?.(event.currentTarget, frame.src);
                }}
              />
            );
          })}
          {blank && (
            <div className="hx-deck__loading" role="status" aria-label={t('next.deck.stage.loading')}>
              <span className="hx-sk hx-deck__loading-sk" />
              <span className="hx-deck__spin" aria-hidden="true" />
            </div>
          )}
          {slow && (
            <span
              className="hx-deck__spin"
              data-corner="1"
              role="status"
              aria-label={t('next.deck.stage.loading')}
            />
          )}
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
