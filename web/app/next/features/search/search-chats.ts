import { peerFrom } from '@/next/features/chat/peer';
import { t } from '@/next/i18n';
import { clock, formatAgo, startOfDay } from '@/next/i18n/format';
import { matching, termsOf, type ITerms } from '@/next/features/search/terms';
import type { ISearchGroup, ISearchRow, ISearchScope } from '@/next/features/search/search-dialog';
import type { MessageHit } from '@/next/protocol/v2';
import { SUBJECT } from '@/next/features/search/subject';
import type { ISidebarChat, ISidebarProject } from '@/next/features/sidebar/types';

/** The scopes a source can search: titles and messages are one list, so there is no «where» to choose until files
 * and designs can be searched too. */
export const CHAT_SCOPES: readonly ISearchScope[] = [];

const RECENT_COUNT = 6;
/** The chats' own tab lists this many of the latest before anything is typed. */
const OWN_COUNT = 40;

const rowOf = (chat: ISidebarChat, projects: readonly ISidebarProject[]): ISearchRow => ({
  id: chat.id,
  kind: 'chat',
  icon: 'message-square',
  title: chat.title,
  meta: projects.find((project) => project.id === chat.projectId)?.name ?? '',
  preview: { kind: t('next.search.kindChat'), text: '' },
  subject: { k: SUBJECT.chat, id: chat.id },
});

/** Chats whose title answers the query (every word of it, or a keyword of a question); the newest first. */
export const searchChats = (
  chats: readonly ISidebarChat[],
  projects: readonly ISidebarProject[],
  query: string,
  terms: ITerms = termsOf(query),
): readonly ISearchGroup[] => {
  const newest = [...chats].sort((a, b) => b.lastActivityAt - a.lastActivityAt);
  const found = matching(newest, terms, (chat) => [chat.title]).map((chat) => rowOf(chat, projects));
  return found.length > 0 ? [{ id: 'chats', label: t('next.search.groupChats'), rows: found }] : [];
};

export const recentChats = (
  chats: readonly ISidebarChat[],
  projects: readonly ISidebarProject[],
  own = false,
): readonly ISearchRow[] =>
  [...chats]
    .sort((a, b) => b.lastActivityAt - a.lastActivityAt)
    .slice(0, own ? OWN_COUNT : RECENT_COUNT)
    .map((chat) => rowOf(chat, projects));

/** The date a message was written, as a short line: the time today, else the day. */
/** «16:50» today, then «вчера», «13 нояб.»: in the page's language, not the browser's. */
const whenOf = (at: number, now: number): string => (at >= startOfDay(now) ? clock(at) : formatAgo(at, now));

/** The messages found, a group for each chat they are in (the chats in the order of their newest hit); a hit in a chat
 *  the list does not have (another machine's, deleted) is left out. */
/** Who a hit's words are: the agent, another chat (its message) or the person. */
const whoSaid = (hit: MessageHit): { icon: string; title: string; text: string } => {
  if (hit.role !== 'user') return { icon: 'sparkles', title: t('next.search.agent'), text: hit.snippet };
  const said = peerFrom(hit.snippet);
  if (said.from === null) return { icon: 'user', title: t('next.search.you'), text: said.text };
  return { icon: 'messages-square', title: t('next.search.from', { chat: said.from }), text: said.text };
};

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
    const said = whoSaid(hit);
    rows.push({
      id: `message:${hit.convId}:${hit.entryId}`,
      kind: 'message',
      icon: said.icon,
      title: said.title,
      snippet: said.text,
      meta: whenOf(hit.at, now),
      target: { chatId: hit.convId, entryId: hit.entryId, turnId: hit.turnId },
      preview: { kind: t('next.search.kindMessage'), text: said.text },
      subject: { k: SUBJECT.chat, id: hit.convId },
    });
    groups.set(chat.id, rows);
  }
  return [...groups].flatMap(([id, rows]) => {
    const chat = byId.get(id);
    return chat ? [{ id: `messages:${id}`, label: chat.title, rows }] : [];
  });
};
