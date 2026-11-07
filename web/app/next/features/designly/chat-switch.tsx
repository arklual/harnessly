// The switch in the head of a design's chat: which chat the column shows, and the design's chats on every machine of
// the person — with what they do now, when they last did something and, for another machine's, which one — to change
// to, or a new one. Only a pick here changes the chat; the list itself never does.
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
  /** The name of the machine the chat is on, when it is not the design's open one (it opens there); null: this one. */
  machine: string | null;
}

/** A row above the chats in place of the design's «Новый чат»: the app page's new chat and its «Без чата». */
export interface IChatSwitchLead {
  id: string;
  icon: string;
  label: string;
  selected: boolean;
  onPick: () => void;
}

export interface IChatSwitchProps {
  /** The chat on screen; null: a new chat for the design (or none, with `lead`). */
  current: string | null;
  /** Its title (it may not be in the rows yet: a chat made a moment ago). */
  title: string;
  rows: readonly IChatSwitchRow[];
  onPick: (id: string | null) => void;
  /** The list opens: the machines are asked again for the design's chats. */
  onOpen?: () => void;
  /** The rows above the chats; absent: the design's own «Новый чат», picked as null. */
  lead?: readonly IChatSwitchLead[];
  /** The heading over the chats; absent: «Чаты этого дизайна». */
  chatsLabel?: string;
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

export const ChatSwitch = ({ current, title, rows, onPick, onOpen, lead, chatsLabel }: IChatSwitchProps) => {
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

  const heading = chatsLabel ?? t('next.site.chats');
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
          if (!open) onOpen?.();
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
          aria-label={heading}
          onPointerDown={(event) => event.stopPropagation()}
        >
          {lead ? (
            lead.map((row) => (
              <button
                key={row.id}
                type="button"
                role="option"
                className="hx-option hx-chatswitch__row"
                aria-selected={row.selected}
                onClick={() => {
                  setOpen(false);
                  if (!row.selected) row.onPick();
                }}
              >
                <Icon name={row.icon} size="sm" />
                <span className="hx-chatswitch__name">{row.label}</span>
              </button>
            ))
          ) : (
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
          )}
          {rows.length > 0 && <div className="hx-dl-pop__label">{heading}</div>}
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
              {row.machine !== null && <span className="hx-chatswitch__machine">{row.machine}</span>}
              <StatusMark status={row.status} />
              <span className="hx-chatswitch__at">{formatAgo(row.at, now)}</span>
            </button>
          ))}
        </div>
      )}
    </div>
  );
};
