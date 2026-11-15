// «Чаты» of the phone: a tap on the chat's title in the header opens this sheet — a search on top, the recent chats
// under it, the open one with a check. A tap on a chat opens it and the sheet goes; a swipe down or a tap outside
// closes it (ui/sheet.tsx draws the rise, the exit and the drag).
import { useMemo, useState } from 'react';
import Sheet from '@/next/ui/sheet';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';
import { STATUS_LOOK } from '@/next/features/sidebar/status';
import { CHAT_STATUS, type ISidebarChat } from '@/next/features/sidebar/types';

/** How many recent chats the sheet lists before a search narrows them. */
const RECENT = 30;

export interface IChatsSheetProps {
  open: boolean;
  onClose: () => void;
  /** The chats the sidebar shows; null while they load. */
  chats: readonly ISidebarChat[] | null;
  currentId: string | null;
  onOpen: (id: string) => void;
}

/** Start of the person's day, local time: a chat with a message since then is «today's». */
const dayStart = (now: number): number => {
  const day = new Date(now);
  day.setHours(0, 0, 0, 0);
  return day.getTime();
};

/**
 * The chats the sheet lists, newest first by the person's last message. Without a query: today's chats and those with
 * a status (at work, waiting, a question, done unseen); with one: any of the person's chats it finds. Chats an agent
 * delegated stay under their parent in the sidebar and are never here. The open chat is always among them.
 */
export const recentChats = (
  chats: readonly ISidebarChat[],
  query: string,
  currentId: string | null,
  now: number = Date.now(),
): ISidebarChat[] => {
  const words = query.trim().toLowerCase().split(/\s+/).filter(Boolean);
  const today = dayStart(now);
  const found = chats
    .filter((chat) => chat.parentId === undefined)
    .filter((chat) =>
      words.length > 0
        ? words.every((word) => chat.title.toLowerCase().includes(word))
        : chat.id === currentId || chat.status !== CHAT_STATUS.none || chat.lastPromptAt >= today,
    )
    .sort((a, b) => b.lastPromptAt - a.lastPromptAt);
  const top = found.slice(0, RECENT);
  if (words.length > 0 || currentId === null || top.some((chat) => chat.id === currentId)) return top;
  const current = found.find((chat) => chat.id === currentId);
  return current ? [current, ...top.slice(0, RECENT - 1)] : top;
};

const ChatsSheet = ({ open, onClose, chats, currentId, onOpen }: IChatsSheetProps) => {
  const [query, setQuery] = useState('');
  const list = useMemo(
    () => (chats === null ? null : recentChats(chats, query, currentId)),
    [chats, query, currentId],
  );
  const close = () => {
    onClose();
    setQuery('');
  };
  return (
    <Sheet open={open} onClose={close} label={t('next.header.chats')} className="hx-chats-sheet">
      <div className="hx-chats-sheet__head">
        <Icon name="search" size="sm" className="hx-chats-sheet__glass" />
        <input
          className="hx-chats-sheet__search"
          type="search"
          autoComplete="off"
          enterKeyHint="search"
          placeholder={t('next.header.chatsSearch')}
          aria-label={t('next.header.chatsSearch')}
          value={query}
          onChange={(event) => setQuery(event.target.value)}
        />
      </div>
      {list !== null && list.length === 0 && (
        <p className="hx-chats-sheet__empty">{t('next.header.chatsEmpty')}</p>
      )}
      {list !== null && list.length > 0 && (
        <ul className="hx-chats-sheet__list" aria-label={t('next.header.chats')}>
          {list.map((chat) => {
            const current = chat.id === currentId;
            const look = chat.status === CHAT_STATUS.none ? null : STATUS_LOOK[chat.status];
            return (
              <li key={chat.id}>
                <button
                  type="button"
                  className="hx-chats-sheet__row"
                  data-status={chat.status}
                  aria-current={current || undefined}
                  onClick={() => {
                    if (!current) onOpen(chat.id);
                    close();
                  }}
                >
                  <span className="hx-chats-sheet__title">{chat.title}</span>
                  {look && (
                    <i
                      className="hx-icon hx-chat-status"
                      data-i={look.icon}
                      data-size="sm"
                      data-status={chat.status}
                      role="img"
                      aria-label={chat.waitsFor ?? look.label}
                    />
                  )}
                  {current && <Icon name="check" size="sm" className="hx-chats-sheet__check" />}
                </button>
              </li>
            );
          })}
        </ul>
      )}
    </Sheet>
  );
};

export default ChatsSheet;
