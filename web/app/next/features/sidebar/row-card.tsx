// The card that opens beside a chat row of the sidebar when the mouse rests on it: the chat's recap (or the last words
// of its turn) and how long ago, for a chat that works, waits for the person or finished what they have not read; for
// a quiet row whose title is cut, the title. One card for the whole list: it follows the pointer from row to row,
// stands to the right of the sidebar so it never covers the row, and goes on scroll, click or when the pointer leaves.
// On the shared hover intent (ui/hover-intent.ts), the mega-menu way: a pointer on its way to the card crosses other
// rows without the card jumping to them, and the card stays while the pointer reaches it; resting on a row (or turning
// away from the card) moves the card to that row.
import { useEffect, useLayoutEffect, useRef, useState, type MouseEvent } from 'react';
import { createPortal } from 'react-dom';
import { chatStatusOf, STATUS_LOOK, statusNoteOf } from '@/next/features/sidebar/status';
import { CHAT_STATUS } from '@/next/features/sidebar/types';
import { formatAgo } from '@/next/i18n/format';
import { t } from '@/next/i18n';
import type { ConvId } from '@/next/protocol/v2';
import { useConversation } from '@/next/stores/conversations';
import Icon from '@/next/ui/icon';
import { usePortalContainer } from '@/next/ui/portal';
import {
  createHoverIntent,
  HOVER_PART,
  HOVER_STATE,
  rectOf,
  type IHoverIntent,
} from '@/next/ui/hover-intent';
import { usePresence } from '@/next/ui/presence';
import '@/next/styles/row-card.css';

const OPEN_DELAY_MS = 380;
const EXIT_MS = 180;
const GAP = 10;
const EDGE = 8;

interface ITarget {
  id: ConvId;
  /** The row's top and the sidebar's right edge, in the window. */
  top: number;
  left: number;
  /** When the card was asked for: the ages on it are counted from here. */
  now: number;
}

const HOVERABLE = '(hover: hover) and (pointer: fine)';

/** The row under the pointer, and whether a card has anything to say about it. */
const rowOf = (el: EventTarget | null): HTMLElement | null =>
  el instanceof Element ? el.closest<HTMLElement>('.hx-item[data-chat]') : null;

const titleIsCut = (row: HTMLElement): boolean => {
  const title = row.querySelector<HTMLElement>('.hx-item__title');
  return title !== null && title.scrollWidth > title.clientWidth;
};

const eligible = (row: HTMLElement): boolean => {
  if (row.dataset.busy || row.dataset.menu || row.querySelector('.hx-item__rename')) return false;
  return row.dataset.status !== CHAT_STATUS.none || titleIsCut(row);
};

interface ICardProps {
  target: ITarget | null;
  boxRef: (el: HTMLDivElement | null) => void;
  onEnter: () => void;
  onLeave: (event: MouseEvent<HTMLDivElement>) => void;
}

const RowCard = ({ target, boxRef, onEnter, onLeave }: ICardProps) => {
  const container = usePortalContainer();
  const { value, state } = usePresence(target, EXIT_MS);
  const record = useConversation(value?.id ?? null);
  const ref = useRef<HTMLDivElement>(null);
  const placed = useRef(false);

  const x = value?.left ?? 0;
  const y = value?.top ?? 0;
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) {
      placed.current = false;
      return;
    }
    const top = Math.max(EDGE, Math.min(y, window.innerHeight - el.offsetHeight - EDGE));
    // The card glides from row to row, but does not fly in from a corner when it first opens.
    el.dataset.glide = String(placed.current);
    el.style.setProperty('--x', `${x}px`);
    el.style.setProperty('--y', `${top}px`);
    placed.current = true;
  }, [x, y, value, record]);

  const host = container?.current;
  if (!value || !record || !host) return null;

  const status = chatStatusOf(record, true);
  const look = status === CHAT_STATUS.none ? null : STATUS_LOOK[status];
  const waitsFor = statusNoteOf(record, status, value.now);
  const text = record.recap?.text ?? record.lastTurn?.snippet ?? '';
  const at = record.recap
    ? record.recap.at
    : (record.lastTurn?.endedAt ?? record.lastTurn?.startedAt ?? null);
  const title = record.title || t('next.shellData.untitled');

  return createPortal(
    <div ref={ref} className="hx-rowcard" data-state={state} role="tooltip">
      <div ref={boxRef} className="hx-rowcard__box" onMouseEnter={onEnter} onMouseLeave={onLeave}>
        <div className="hx-rowcard__title">{title}</div>
        {look && (
          <div className="hx-rowcard__meta" data-status={status}>
            <Icon name={look.icon} size="sm" className="hx-rowcard__ico" />
            <span>{waitsFor ?? look.label}</span>
            {at !== null && <span className="hx-rowcard__time">{formatAgo(at, value.now)}</span>}
          </div>
        )}
        {look && text && <p className="hx-rowcard__text">{text}</p>}
      </div>
    </div>,
    host,
  );
};

/** The card's pointer logic over the list, made once: which row it speaks of, which row is under the pointer. */
const createRowIntent = (setTarget: (target: ITarget | null) => void) => {
  /** The row the card speaks of, and the eligible row under the pointer (the next one, if the card moves). */
  let shown: HTMLElement | null = null;
  let under: HTMLElement | null = null;
  let box: HTMLDivElement | null = null;

  const show = (row: HTMLElement) => {
    const side = row.closest('aside')?.getBoundingClientRect();
    const rect = row.getBoundingClientRect();
    shown = row;
    setTarget({
      id: row.dataset.chat as ConvId,
      top: rect.top - 8,
      left: (side?.right ?? rect.right) + GAP,
      now: Date.now(),
    });
  };

  const intent: IHoverIntent = createHoverIntent({
    openDelay: OPEN_DELAY_MS,
    rects: () => ({ trigger: shown ? rectOf(shown) : null, floating: box ? rectOf(box) : null }),
    onChange: (open) => {
      if (open && under) {
        show(under);
        return;
      }
      if (open) return;
      // The pointer turned away from the card, or rested, over another row: the card moves there.
      if (under && under !== shown && under.isConnected) {
        intent.show();
        intent.enter(HOVER_PART.trigger);
        return;
      }
      shown = null;
      setTarget(null);
    },
  });

  const close = () => {
    under = null;
    intent.close();
  };

  const onMouseOver = (event: MouseEvent<HTMLElement>) => {
    if (!window.matchMedia(HOVERABLE).matches) return;
    const point = { x: event.clientX, y: event.clientY };
    const found = rowOf(event.target);
    const row = found && eligible(found) ? found : null;
    const was = under;
    if (row === was) return;
    under = row;
    // Off the row the card speaks of: on the way to the card it stays put, otherwise it goes or moves.
    if (was && was === shown) intent.leave(HOVER_PART.trigger, point);
    if (!row) {
      if (intent.state === HOVER_STATE.opening) intent.leave(HOVER_PART.trigger, point);
      return;
    }
    // Still heading for the card across this row: wait; the move that turns away (or a rest) moves the card.
    if (row !== shown && intent.state === HOVER_STATE.leaving) return;
    if (row !== shown && intent.open) show(row);
    intent.enter(HOVER_PART.trigger);
  };

  const onMouseLeave = (event: MouseEvent<HTMLElement>) => {
    const was = under;
    under = null;
    if (was && was === shown) intent.leave(HOVER_PART.trigger, { x: event.clientX, y: event.clientY });
    else if (intent.state !== HOVER_STATE.leaving) intent.close();
  };

  return {
    intent,
    handlers: { onMouseOver, onMouseLeave, onMouseDown: close, onScrollCapture: close },
    boxRef: (el: HTMLDivElement | null) => {
      box = el;
    },
    onCardEnter: () => intent.enter(HOVER_PART.floating),
    onCardLeave: (event: MouseEvent<HTMLDivElement>) =>
      intent.leave(HOVER_PART.floating, { x: event.clientX, y: event.clientY }),
    /** While the card is out, the pointer's way decides (it may leave the sidebar for the card). */
    listen: () => {
      const onMove = (event: PointerEvent) => {
        if (event.pointerType === 'mouse') intent.move({ x: event.clientX, y: event.clientY });
      };
      window.addEventListener('pointermove', onMove, { passive: true });
      return () => window.removeEventListener('pointermove', onMove);
    },
  };
};

/** Handlers for the sidebar's element and the card to draw once; hovering a row renders only on the card's change. */
const useRowCard = () => {
  const [target, setTarget] = useState<ITarget | null>(null);
  const [rows] = useState(() => createRowIntent(setTarget));
  const open = target !== null;
  useEffect(() => (open ? rows.listen() : undefined), [open, rows]);
  useEffect(() => () => rows.intent.destroy(), [rows]);

  return {
    handlers: rows.handlers,
    card: (
      <RowCard target={target} boxRef={rows.boxRef} onEnter={rows.onCardEnter} onLeave={rows.onCardLeave} />
    ),
  };
};

export default useRowCard;
