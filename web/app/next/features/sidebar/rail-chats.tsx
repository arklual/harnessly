// The folded sidebar's list: the chats that are alive — an agent at work, a question or a permission waiting for the
// person, a turn finished and not read yet — as small marks with their status, instead of the projects' letters (the
// projects are a hover away, in the sidebar that opens over the window). The name comes as the rail's tip.
import { memo } from 'react';
import { byPrompt } from '@/next/features/sidebar/group';
import { STATUS_LOOK } from '@/next/features/sidebar/status';
import { CHAT_STATUS, type ISidebarChat, type TChatStatus } from '@/next/features/sidebar/types';
import { t } from '@/next/i18n';

/** The rail has room for this many; the rest are in the open sidebar. */
export const RAIL_CHATS_MAX = 8;

/** The chat the person wrote to last first: a mark never changes its place while its chat works or finishes. */
export const activeChats = (chats: readonly ISidebarChat[] | null): ISidebarChat[] =>
  (chats ?? [])
    .filter((chat) => chat.status !== CHAT_STATUS.none)
    .sort(byPrompt)
    .slice(0, RAIL_CHATS_MAX);

export interface IRailChatsProps {
  chats: readonly ISidebarChat[] | null;
  currentId: string | null;
  /** `newTab`: opened with ⌘ or the middle button. */
  onOpen: (id: string, newTab: boolean) => void;
}

const RailChats = ({ chats, currentId, onOpen }: IRailChatsProps) => {
  const live = activeChats(chats);
  if (live.length === 0) return null;
  return (
    <section className="hx-railchats" aria-label={t('next.rail.active')}>
      {live.map((chat) => {
        const look = STATUS_LOOK[chat.status as Exclude<TChatStatus, 'none'>];
        const what = chat.waitsFor ?? look.label;
        return (
          <button
            key={chat.id}
            type="button"
            className="hx-railchat"
            data-status={chat.status}
            data-terminal={chat.terminal ? undefined : 'off'}
            data-tip={`${chat.title} · ${what}`}
            aria-label={`${chat.title} · ${what}`}
            aria-current={chat.id === currentId ? 'page' : undefined}
            onClick={(event) => onOpen(chat.id, event.metaKey || event.ctrlKey)}
            onMouseDown={(event) => {
              if (event.button === 1) event.preventDefault();
            }}
            onAuxClick={(event) => {
              if (event.button !== 1) return;
              event.preventDefault();
              onOpen(chat.id, true);
            }}
          >
            <span className="hx-railchat__mark hx-mono" aria-hidden="true">
              {chat.title.slice(0, 1).toUpperCase()}
            </span>
            <span className="hx-railchat__badge" aria-hidden="true">
              <i
                className="hx-icon hx-chat-status hx-railchat__status"
                data-i={look.icon}
                data-size="sm"
                data-status={chat.status}
              />
            </span>
          </button>
        );
      })}
    </section>
  );
};

export default memo(RailChats);
