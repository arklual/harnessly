import { peerFrom } from '@/next/features/chat/peer';
import { t } from '@/next/i18n';
import { clock, formatAgo, formatWhen, startOfDay, whenOf, WHEN, type TWhen } from '@/next/i18n/format';
import { matching, termsOf, type ITerms } from '@/next/features/search/terms';
import type { ISearchGroup, ISearchRow, ISearchScope } from '@/next/features/search/search-dialog';
import type { MessageHit } from '@/next/protocol/v2';
import { SUBJECT } from '@/next/features/search/subject';
import type { ISidebarChat } from '@/next/features/sidebar/types';

/** The scopes a source can search: titles and messages are one list, so there is no «where» to choose until files
 * and designs can be searched too. */
export const CHAT_SCOPES: readonly ISearchScope[] = [];

const RECENT_COUNT = 6;
/** The chats' own tab lists this many of the latest before anything is typed. */
const OWN_COUNT = 40;

const WHEN_ORDER: readonly TWhen[] = [WHEN.today, WHEN.yesterday, WHEN.week, WHEN.month, WHEN.older];

/** When the person last used a chat: their last message (a chat's opening is not kept anywhere, and its read mark
 *  moves with the agent's turns), the order the sidebar keeps too. */
const usedAt = (chat: ISidebarChat): number => chat.lastPromptAt;

const SECTION_KEY: Readonly<Record<TWhen, string>> = {
  [WHEN.today]: 'next.search.when.today',
  [WHEN.yesterday]: 'next.search.when.yesterday',
  [WHEN.week]: 'next.search.when.week',
  [WHEN.month]: 'next.search.when.month',
  [WHEN.older]: 'next.search.when.older',
};

const rowOf = (chat: ISidebarChat, now: number, sections: boolean): ISearchRow => {
  const at = usedAt(chat);
  return {
    id: chat.id,
    kind: 'chat',
    icon: 'message-square',
    title: chat.title,
    meta: formatWhen(at, now),
    section: sections ? t(SECTION_KEY[whenOf(at, now)]) : undefined,
    preview: { kind: t('next.search.kindChat'), text: '' },
    subject: { k: SUBJECT.chat, id: chat.id },
  };
};

/** The chats in the order of their use, by the day groups; inside a group a chat named just as the words comes first,
 *  then the latest. */
export const orderChats = (
  chats: readonly ISidebarChat[],
  now: number,
  query = '',
): readonly ISidebarChat[] => {
  const words = query.trim().toLowerCase();
  const rank = (chat: ISidebarChat): number[] => [
    WHEN_ORDER.indexOf(whenOf(usedAt(chat), now)),
    words !== '' && chat.title.trim().toLowerCase() === words ? 0 : 1,
  ];
  return [...chats].sort((a, b) => {
    const ra = rank(a);
    const rb = rank(b);
    const i = ra.findIndex((value, n) => value !== rb[n]);
    return i === -1 ? usedAt(b) - usedAt(a) : (ra[i] ?? 0) - (rb[i] ?? 0);
  });
};

/** The chats the person started: the ones an agent delegated are not searched at all, as the phone's sheet leaves them
 *  out (Sergey). */
const ownChats = (chats: readonly ISidebarChat[]): readonly ISidebarChat[] =>
  chats.filter((chat) => chat.parentId === undefined);

/** Chats whose title answers the query (every word of it, or a keyword of a question), by the time they were used. */
export const searchChats = (
  chats: readonly ISidebarChat[],
  query: string,
  terms: ITerms = termsOf(query),
  now: number = Date.now(),
): readonly ISearchGroup[] => {
  // What answers the words, in the order of use: how many of a question's words a title holds does not lift it.
  const own = ownChats(chats);
  const answers = new Set(matching(own, terms, (chat) => [chat.title]));
  const found = orderChats(
    own.filter((chat) => answers.has(chat)),
    now,
    query,
  ).map((chat) => rowOf(chat, now, true));
  return found.length > 0 ? [{ id: 'chats', label: t('next.search.groupChats'), rows: found }] : [];
};

/** The person's latest chats before anything is typed; `own` is the chats' tab: more of them, under the day groups. */
export const recentChats = (
  chats: readonly ISidebarChat[],
  own = false,
  now: number = Date.now(),
): readonly ISearchRow[] =>
  orderChats(ownChats(chats), now)
    .slice(0, own ? OWN_COUNT : RECENT_COUNT)
    .map((chat) => rowOf(chat, now, own));

/** The date a message was written, as a short line: the time today, else the day. */
/** «16:50» today, then «вчера», «13 нояб.»: in the page's language, not the browser's. */
const saidAt = (at: number, now: number): string => (at >= startOfDay(now) ? clock(at) : formatAgo(at, now));

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
    // A delegated chat's messages are left out as the chat itself is.
    if (!chat || chat.parentId !== undefined) continue;
    const rows = groups.get(chat.id) ?? [];
    const said = whoSaid(hit);
    rows.push({
      id: `message:${hit.convId}:${hit.entryId}`,
      kind: 'message',
      icon: said.icon,
      title: said.title,
      snippet: said.text,
      meta: saidAt(hit.at, now),
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
