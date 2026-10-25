import { t } from '@/next/i18n';
import type { ISearchGroup, ISearchRow, ISearchScope } from '@/next/features/search/search-dialog';
import type { ISidebarChat, ISidebarProject } from '@/next/features/sidebar/types';

/** The scopes a source can search: the chats' titles are all there is without the runtime's index, so there is no
 * «where» to choose until messages, files and designs can be searched too. */
export const CHAT_SCOPES: readonly ISearchScope[] = [];

const RECENT_COUNT = 6;

const rowOf = (chat: ISidebarChat, projects: readonly ISidebarProject[]): ISearchRow => ({
  id: chat.id,
  kind: 'chat',
  icon: 'message-square',
  title: chat.title,
  meta: projects.find((project) => project.id === chat.projectId)?.name ?? '',
});

const fold = (text: string) => text.toLowerCase().replace(/ё/g, 'е');

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
