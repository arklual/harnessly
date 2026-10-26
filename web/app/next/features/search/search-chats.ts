import { t } from '@/next/i18n';
import { foldText } from '@/next/features/search/highlight';
import type { ISearchGroup, ISearchRow, ISearchScope } from '@/next/features/search/search-dialog';
import type { MessageHit } from '@/next/protocol/v2';
import type { ISidebarChat, ISidebarProject } from '@/next/features/sidebar/types';

/** The scopes a source can search: titles and messages are one list, so there is no «where» to choose until files
 * and designs can be searched too. */
export const CHAT_SCOPES: readonly ISearchScope[] = [];

const RECENT_COUNT = 6;

const rowOf = (chat: ISidebarChat, projects: readonly ISidebarProject[]): ISearchRow => ({
  id: chat.id,
  kind: 'chat',
  icon: 'message-square',
  title: chat.title,
  meta: projects.find((project) => project.id === chat.projectId)?.name ?? '',
});

const fold = foldText;

/** Chats whose title has every word of the query; the newest first. */
export const searchChats = (
  chats: readonly ISidebarChat[],
  projects: readonly ISidebarProject[],
  query: string,
): readonly ISearchGroup[] => {
  const words = fold(query).trim().split(/\s+/).filter(Boolean);
  const found = [...chats]
    .filter((chat) => words.every((word) => fold(chat.title).includes(word)))
    .sort((a, b) => b.lastActivityAt - a.lastActivityAt)
    .map((chat) => rowOf(chat, projects));
  return found.length > 0 ? [{ id: 'chats', label: t('next.search.groupChats'), rows: found }] : [];
};

export const recentChats = (
  chats: readonly ISidebarChat[],
  projects: readonly ISidebarProject[],
): readonly ISearchRow[] =>
  [...chats]
    .sort((a, b) => b.lastActivityAt - a.lastActivityAt)
    .slice(0, RECENT_COUNT)
    .map((chat) => rowOf(chat, projects));

/** The date a message was written, as a short line: the time today, else the day. */
const whenOf = (at: number, now: number): string => {
  const date = new Date(at);
  const sameDay = date.toDateString() === new Date(now).toDateString();
  return sameDay
    ? date.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })
    : date.toLocaleDateString(undefined, { day: 'numeric', month: 'short' });
};

/** The messages found, a group for each chat they are in (the chats in the order of their newest hit); a hit in a chat
 *  the list does not have (another machine's, deleted) is left out. */
export const messageGroups = (
  hits: readonly MessageHit[],
  chats: readonly ISidebarChat[],
  now: number,
): readonly ISearchGroup[] => {
  const byId = new Map(chats.map((chat) => [chat.id, chat]));
  const groups = new Map<string, ISearchRow[]>();
  for (const hit of hits) {
    const chat = byId.get(hit.convId);
    if (!chat) continue;
    const rows = groups.get(chat.id) ?? [];
    rows.push({
      id: `message:${hit.convId}:${hit.entryId}`,
      kind: 'message',
      icon: hit.role === 'user' ? 'user' : 'sparkles',
      title: hit.role === 'user' ? t('next.search.you') : t('next.search.agent'),
      snippet: hit.snippet,
      meta: whenOf(hit.at, now),
      target: { chatId: hit.convId, entryId: hit.entryId, turnId: hit.turnId },
    });
    groups.set(chat.id, rows);
  }
  return [...groups].flatMap(([id, rows]) => {
    const chat = byId.get(id);
    return chat ? [{ id: `messages:${id}`, label: chat.title, rows }] : [];
  });
};
