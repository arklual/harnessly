// The switch in the head of a design's chat: which chat the column shows, and the chats that worked on the design —
// with what they do now and when they last did something — to change to, or a new one. Only a pick here changes the
// chat; the list itself never does.
import { useEffect, useRef, useState } from 'react';
import { STATUS_LOOK } from '@/next/features/sidebar/status';
import { CHAT_STATUS, type TChatStatus } from '@/next/features/sidebar/types';
import { formatAgo } from '@/next/i18n/format';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import { usePresence } from '@/next/ui/presence';

export interface IChatSwitchRow {
  id: string;
  title: string;
  status: TChatStatus;
  /** The chat's last activity. */
  at: number;
}

export interface IChatSwitchProps {
  /** The chat on screen; null: a new chat for the design. */
  current: string | null;
  /** Its title (it may not be in the rows yet: a chat made a moment ago). */
  title: string;
  rows: readonly IChatSwitchRow[];
  onPick: (id: string | null) => void;
}

/** The menu's exit, as long as hx-dl-pop-out in site.css. */
const MENU_MS = 190;

const StatusMark = ({ status }: { status: TChatStatus }) => {
  if (status === CHAT_STATUS.none) return null;
  const look = STATUS_LOOK[status];
  return (
    <i
      className="hx-icon hx-chat-status"
      data-i={look.icon}
      data-size="sm"
      data-status={status}
      role="img"
      aria-label={look.label}
      title={look.label}
    />
  );
};

export const ChatSwitch = ({ current, title, rows, onPick }: IChatSwitchProps) => {
  const [open, setOpen] = useState(false);
  const menu = usePresence(open ? true : null, MENU_MS);
  const rootRef = useRef<HTMLDivElement>(null);
  const [now, setNow] = useState(() => Date.now());

  // A press outside and Esc close it.
  useEffect(() => {
    if (!open) return;
    const onDown = (event: PointerEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) setOpen(false);
    };
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setOpen(false);
    };
    window.addEventListener('pointerdown', onDown);
    window.addEventListener('keydown', onKey);
    return () => {
      window.removeEventListener('pointerdown', onDown);
      window.removeEventListener('keydown', onKey);
    };
  }, [open]);

  const choose = (id: string | null) => {
    setOpen(false);
    if (id !== current) onPick(id);
  };

  return (
    <div className="hx-chatswitch" ref={rootRef}>
      <button
        type="button"
        className="hx-chatswitch__btn"
        aria-haspopup="listbox"
        aria-expanded={open}
        onPointerDown={(event) => event.stopPropagation()}
        onClick={() => {
          setNow(Date.now());
          setOpen((on) => !on);
        }}
      >
        <span className="hx-chatswitch__title">{title}</span>
        <Icon name="chevron-down" size="sm" className="hx-chatswitch__chev" />
      </button>
      {menu.value !== null && (
        <div
          className="hx-site-menu hx-site-menu--start hx-chatswitch__menu"
          data-state={menu.state}
          role="listbox"
          aria-label={t('next.site.chats')}
          onPointerDown={(event) => event.stopPropagation()}
        >
          <button
            type="button"
            role="option"
            className="hx-option hx-chatswitch__row"
            aria-selected={current === null}
            onClick={() => choose(null)}
          >
            <Icon name="square-pen" size="sm" />
            <span className="hx-chatswitch__name">{t('next.site.newChat')}</span>
          </button>
          {rows.length > 0 && <div className="hx-dl-pop__label">{t('next.site.chats')}</div>}
          {rows.map((row) => (
            <button
              key={row.id}
              type="button"
              role="option"
              className="hx-option hx-chatswitch__row"
              aria-selected={row.id === current}
              onClick={() => choose(row.id)}
            >
              <span className="hx-chatswitch__name">{row.title}</span>
              <StatusMark status={row.status} />
              <span className="hx-chatswitch__at">{formatAgo(row.at, now)}</span>
            </button>
          ))}
        </div>
      )}
    </div>
  );
};
