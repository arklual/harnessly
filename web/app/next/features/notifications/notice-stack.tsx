// The notices at the top of the window, as a pile (after the admin's notifications): the newest in front, two more
// peeking from under it; a pointer over the pile, a focus in it or a tap on it spreads them into a list, with
// «Скрыть все» under it. A notice comes down from the top, the pile closes up as one leaves, a swipe sideways or ×
// takes one away. One implementation for every width: on the phone it is as wide as the screen, under the safe area.
import '@/next/styles/notices.css';

import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type MouseEvent,
  type PointerEvent,
} from 'react';
import { t } from '@/next/i18n';
import { formatAgo } from '@/next/i18n/format';
import Icon from '@/next/ui/icon';
import { useLeavingList } from '@/next/ui/presence';
import { NOTICE_KIND, type INotice, type TNoticeKind } from '@/next/stores/notices';

/** How long a notice takes to leave, ms (styles/notices.css has the same). */
export const NOTICE_EXIT_MS = 320;
/** The spread list shows this many; the older ones are in the notification centre. */
const MAX_SHOWN = 5;
/** The folded pile shows the front one and this many under it. */
const PEEKS = 2;
const GAP = 8;
const PEEK_PX = 10;
const SCALE_STEP = 0.05;
/** A drag sideways past this, or a quick flick, takes a notice away: px, px/ms. */
const SWIPE_PX = 80;
const SWIPE_VELOCITY = 0.45;
const DRAG_START_PX = 6;

const TONE: Record<TNoticeKind, string> = {
  [NOTICE_KIND.waiting]: 'warn',
  [NOTICE_KIND.done]: 'ok',
  [NOTICE_KIND.failed]: 'bad',
  [NOTICE_KIND.limit]: 'warn',
};

export interface INoticeStackProps {
  /** Newest first. */
  notices: readonly INotice[];
  now: number;
  onOpen: (notice: INotice) => void;
  onDismiss: (key: string) => void;
  onDismissAll: () => void;
}

/** Places the cards: folded, each under the front one, as tall as it and a little smaller; spread, one under another. */
const layout = (list: HTMLOListElement | null, spread: boolean) => {
  if (!list) return;
  const slots = [...list.querySelectorAll<HTMLElement>(':scope > .hx-notice-slot:not([data-leaving])')];
  const heights = slots.map((slot) => (slot.firstElementChild as HTMLElement | null)?.offsetHeight ?? 0);
  const front = heights[0] ?? 0;
  let y = 0;
  slots.forEach((slot, index) => {
    const depth = Math.min(index, PEEKS);
    slot.style.setProperty('--hx-n-z', String(slots.length - index));
    if (spread) {
      slot.style.setProperty('--hx-n-y', `${y}px`);
      slot.style.setProperty('--hx-n-s', '1');
      slot.style.setProperty('--hx-n-h', `${heights[index]}px`);
      y += heights[index] + GAP;
    } else {
      slot.style.setProperty('--hx-n-y', `${depth * PEEK_PX}px`);
      slot.style.setProperty('--hx-n-s', String(1 - depth * SCALE_STEP));
      slot.style.setProperty('--hx-n-h', `${front}px`);
    }
    slot.dataset.depth = spread ? '0' : String(Math.min(index, PEEKS + 1));
  });
  const height = spread ? Math.max(0, y - GAP) : front + Math.min(slots.length - 1, PEEKS) * PEEK_PX;
  list.parentElement?.style.setProperty('--hx-n-stack', `${height}px`);
};

const NoticeStack = ({ notices, now, onOpen, onDismiss, onDismissAll }: INoticeStackProps) => {
  // The same array while the list is the same: the leaving list tells a change by it.
  const shown = useMemo(() => notices.slice(0, MAX_SHOWN), [notices]);
  const rows = useLeavingList(shown, (n) => n.key, NOTICE_EXIT_MS);
  const [hover, setHover] = useState(false);
  const [tapped, setTapped] = useState(false);
  const spread = (hover || tapped) && shown.length > 1;
  const listRef = useRef<HTMLOListElement>(null);
  const drag = useRef<{ key: string; x: number; y: number; t: number; dx: number; moving: boolean } | null>(
    null,
  );
  const dragged = useRef(false);
  const touch = useRef(false);

  // After every change of the list or the spread, before the paint: the cards move from where they were.
  useLayoutEffect(() => layout(listRef.current, spread));
  useEffect(() => {
    const onResize = () => layout(listRef.current, spread);
    window.addEventListener('resize', onResize);
    return () => window.removeEventListener('resize', onResize);
  }, [spread]);

  // A pile spread by a tap folds back on a tap elsewhere, or on Esc.
  useEffect(() => {
    if (!tapped) return;
    const onDown = (event: globalThis.PointerEvent) => {
      if (!listRef.current?.parentElement?.contains(event.target as Node)) setTapped(false);
    };
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setTapped(false);
    };
    document.addEventListener('pointerdown', onDown, true);
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('pointerdown', onDown, true);
      document.removeEventListener('keydown', onKey);
    };
  }, [tapped]);
  if (tapped && shown.length <= 1) setTapped(false);

  const onPointerDown = (key: string) => (event: PointerEvent<HTMLElement>) => {
    touch.current = event.pointerType !== 'mouse';
    dragged.current = false;
    if (event.button !== 0) return;
    drag.current = { key, x: event.clientX, y: event.clientY, t: event.timeStamp, dx: 0, moving: false };
  };
  const onPointerMove = (event: PointerEvent<HTMLElement>) => {
    const d = drag.current;
    if (!d) return;
    const dx = event.clientX - d.x;
    const dy = event.clientY - d.y;
    if (!d.moving) {
      if (Math.abs(dy) > DRAG_START_PX && Math.abs(dy) > Math.abs(dx)) {
        drag.current = null;
        return;
      }
      if (Math.abs(dx) < DRAG_START_PX) return;
      d.moving = true;
      event.currentTarget.setPointerCapture(event.pointerId);
      event.currentTarget.dataset.drag = 'true';
    }
    d.dx = dx;
    event.currentTarget.style.setProperty('--hx-n-dx', `${dx}px`);
  };
  const onPointerEnd = (event: PointerEvent<HTMLElement>) => {
    const d = drag.current;
    drag.current = null;
    if (!d?.moving) return;
    dragged.current = true;
    const slot = event.currentTarget;
    delete slot.dataset.drag;
    const fast = Math.abs(d.dx) / Math.max(1, event.timeStamp - d.t) > SWIPE_VELOCITY;
    if (Math.abs(d.dx) > SWIPE_PX || fast) {
      slot.style.setProperty('--hx-n-dx', `${Math.sign(d.dx) * (slot.offsetWidth + 24)}px`);
      onDismiss(d.key);
      return;
    }
    slot.style.removeProperty('--hx-n-dx');
  };

  // A tap on a folded pile spreads it before anything in it is pressed; a drag is never a press.
  const onClickCapture = useCallback(
    (event: MouseEvent) => {
      if (dragged.current) {
        dragged.current = false;
        event.stopPropagation();
        event.preventDefault();
        return;
      }
      if (touch.current && !spread && shown.length > 1) {
        event.stopPropagation();
        event.preventDefault();
        setTapped(true);
      }
    },
    [spread, shown.length],
  );

  return (
    <section
      className="hx-notices"
      aria-label={t('next.notices.region')}
      data-spread={spread || undefined}
      data-empty={rows.length === 0 || undefined}
      onPointerEnter={(event) => {
        if (event.pointerType === 'mouse') setHover(true);
      }}
      onPointerLeave={(event) => {
        if (event.pointerType === 'mouse') setHover(false);
      }}
      onFocus={(event) => {
        // Only a keyboard's focus spreads the pile: a tap focuses the button it lands on, and that tap must spread first.
        if (event.target.matches(':focus-visible')) setHover(true);
      }}
      onBlur={(event) => {
        if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setHover(false);
      }}
    >
      <ol ref={listRef} className="hx-notices__list" aria-live="polite" onClickCapture={onClickCapture}>
        {rows.map(({ item, leaving }) => (
          <li
            key={item.key}
            className="hx-notice-slot"
            data-leaving={leaving || undefined}
            onPointerDown={onPointerDown(item.key)}
            onPointerMove={onPointerMove}
            onPointerUp={onPointerEnd}
            onPointerCancel={onPointerEnd}
          >
            <div className="hx-notice" data-tone={TONE[item.kind]}>
              <Icon name={item.icon} size="sm" className="hx-notice__ic" />
              <button
                type="button"
                className="hx-notice__main"
                tabIndex={leaving ? -1 : undefined}
                onClick={() => onOpen(item)}
              >
                <span className="hx-notice__top">
                  <span className="hx-notice__title">{item.title}</span>
                  <span className="hx-notice__when">{formatAgo(item.at, now)}</span>
                </span>
                <span className="hx-notice__text">{item.text}</span>
              </button>
              <button
                type="button"
                className="hx-notice__x"
                aria-label={t('next.notices.dismiss')}
                tabIndex={leaving ? -1 : undefined}
                onClick={() => onDismiss(item.key)}
              >
                <Icon name="x" size="sm" />
              </button>
              <button
                type="button"
                className="hx-notice__open"
                tabIndex={leaving ? -1 : undefined}
                onClick={() => onOpen(item)}
              >
                {t('next.notices.open')}
              </button>
            </div>
          </li>
        ))}
      </ol>
      {shown.length > 1 && (
        <button
          type="button"
          className="hx-notices__all"
          tabIndex={spread ? undefined : -1}
          aria-hidden={spread ? undefined : true}
          onClick={() => {
            setTapped(false);
            onDismissAll();
          }}
        >
          {t('next.notices.dismissAll')}
        </button>
      )}
    </section>
  );
};

export default NoticeStack;
