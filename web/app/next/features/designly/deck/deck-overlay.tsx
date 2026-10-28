// What the deck draws over the slide (the board's .pr-bx, .pr-grip, .pr-bx-reset, .pr-guide): the frame of the block
// under the pointer, its grip to drag it by, «Вернуть на место» for a block moved by hand, and the pink guides while a
// block is dragged. Positions are the board's pixels in CSS variables; the slot's `--deck-k` scales them to the
// screen, so nothing here re-renders when the stage is resized. Pins, the press ring and the pointer are laid over the
// same slot by their own parts (`children`).
import { useLayoutEffect, useRef, type PointerEvent as ReactPointerEvent, type ReactNode } from 'react';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import type { IDeckGuide, TDeckRect } from '@/next/features/designly/deck/bridge';
import { DECK_SNAP_PX, deckRound, deckShift, deckSnap } from '@/next/features/designly/deck/layout';

/** The block under the pointer, as the board said it. */
export interface IDeckHover {
  tpl: number;
  name: string;
  rect: TDeckRect;
  moved: boolean;
  movable: boolean;
  /** Its hand move so far, board px. */
  tx: number;
  ty: number;
  /** The board's fingerprint of the element: a move or reset patch carries it. */
  was: string;
  /** The block sits in a container of its own: its move is written in px. */
  px: boolean;
}

/** A block being dragged: its translate now (board px), the guides it stuck to, and what its move patch carries. */
export interface IDeckDrag {
  tpl: number;
  x: number;
  y: number;
  guides: IDeckGuide[];
  was: string;
  px: boolean;
}

export interface IDeckOverlayProps {
  hover: IDeckHover | null;
  drag: IDeckDrag | null;
  /** The board's size, px. */
  w: number;
  h: number;
  onDrag: (drag: IDeckDrag) => void;
  onDrop: (drag: IDeckDrag) => void;
  onReset: (hover: IDeckHover) => void;
  /** The pointer is on the frame's own buttons (over the board, which then says it is over nothing). */
  onHot: (hot: boolean) => void;
  children?: ReactNode;
}

/** Writes board-px CSS variables on an element, without a render per change. */
const useBoardVars = (vars: Record<string, number>) => {
  const ref = useRef<HTMLDivElement>(null);
  const key = JSON.stringify(vars);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    for (const [name, value] of Object.entries(JSON.parse(key) as Record<string, number>))
      el.style.setProperty(name, String(value));
  }, [key]);
  return ref;
};

const Guide = ({ guide }: { guide: IDeckGuide }) => {
  const ref = useBoardVars({ '--at': guide.at });
  return <div ref={ref} className="hx-deck-guide" data-v={guide.v} />;
};

const Frame = ({
  hover,
  rect,
  dragging,
  onGrip,
  onReset,
  onHot,
}: {
  hover: IDeckHover;
  rect: TDeckRect;
  dragging: boolean;
  onGrip: (event: ReactPointerEvent<HTMLButtonElement>) => void;
  onReset: () => void;
  onHot: (hot: boolean) => void;
}) => {
  const ref = useBoardVars({ '--x': rect[0], '--y': rect[1], '--w': rect[2], '--h': rect[3] });
  return (
    <div
      ref={ref}
      className="hx-deck-bx"
      onPointerEnter={() => onHot(true)}
      onPointerLeave={() => onHot(false)}
    >
      {hover.movable && (
        <button
          type="button"
          className="hx-deck-grip"
          aria-label={t('next.deck.overlay.gripLabel', { name: hover.name })}
          title={t('next.deck.overlay.grip')}
          onPointerDown={onGrip}
        >
          <Icon name="grip-horizontal" size="sm" />
        </button>
      )}
      {hover.moved && !dragging && (
        <button
          type="button"
          className="hx-deck-bx__reset"
          aria-label={t('next.deck.overlay.reset')}
          title={t('next.deck.overlay.reset')}
          onPointerDown={(event) => {
            event.preventDefault();
            event.stopPropagation();
            onReset();
          }}
        >
          <Icon name="undo-2" size="sm" />
        </button>
      )}
    </div>
  );
};

const DeckOverlay = ({ hover, drag, w, h, onDrag, onDrop, onReset, onHot, children }: IDeckOverlayProps) => {
  const live = drag !== null && hover !== null && drag.tpl === hover.tpl ? drag : null;
  // The frame follows the block while it is dragged: where it was, plus the move since the hover.
  const rect = hover && (live ? deckShift(hover.rect, live.x - hover.tx, live.y - hover.ty) : hover.rect);

  /** The host's own drag, by the grip: the board follows through `onDrag`, the end is written by `onDrop`. */
  const grip = (event: ReactPointerEvent<HTMLButtonElement>) => {
    if (!hover || event.button !== 0) return;
    event.preventDefault();
    event.stopPropagation();
    const slot = event.currentTarget.closest<HTMLElement>('.hx-deck__slot');
    const k = slot && w > 0 ? slot.getBoundingClientRect().width / w : 1;
    const start = { x: event.clientX, y: event.clientY };
    const at = hover;
    const handle = event.currentTarget;
    handle.setPointerCapture(event.pointerId);
    let last: IDeckDrag = { tpl: at.tpl, x: at.tx, y: at.ty, guides: [], was: at.was, px: at.px };
    const move = (e: PointerEvent) => {
      const snap = deckSnap(
        at.rect,
        (e.clientX - start.x) / k,
        (e.clientY - start.y) / k,
        w,
        h,
        DECK_SNAP_PX / k,
      );
      last = {
        tpl: at.tpl,
        x: deckRound(at.tx + snap.dx),
        y: deckRound(at.ty + snap.dy),
        guides: snap.guides,
        was: at.was,
        px: at.px,
      };
      onDrag(last);
    };
    const up = () => {
      handle.removeEventListener('pointermove', move);
      handle.removeEventListener('pointerup', up);
      handle.removeEventListener('pointercancel', up);
      onDrop(last);
    };
    handle.addEventListener('pointermove', move);
    handle.addEventListener('pointerup', up);
    handle.addEventListener('pointercancel', up);
    onDrag(last);
  };

  return (
    <div className="hx-deck-overlay">
      {hover && rect && (
        <Frame
          hover={hover}
          rect={rect}
          dragging={live !== null}
          onGrip={grip}
          onReset={() => onReset(hover)}
          onHot={onHot}
        />
      )}
      {drag?.guides.map((guide) => (
        <Guide key={`${guide.v}:${guide.at}`} guide={guide} />
      ))}
      {children}
    </div>
  );
};

export default DeckOverlay;
