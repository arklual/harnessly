// How the strip keeps itself tidy, as rules over the tabs and what their chats say (the owner applies them):
// — a tab nothing was done in (a new chat's start page, a chat without a message) goes once the person leaves it;
// — a tab left alone for long goes, unless something there is the person's to see (an agent at work, a question,
//   a finished turn not read yet, a draft);
// — a tab that comes to want the person (a question, a permission, a plan; then a finished turn not read) moves to the
//   front, behind those that want them more. The strip's FLIP slides it there.
import { CHAT_STATUS, type TChatStatus } from '@/next/features/sidebar/types';

/** A tab left alone this long, with nothing in it for the person, goes. */
export const TAB_IDLE_MS = 15 * 60_000;
/** How often the strip looks for such tabs. */
export const TAB_SWEEP_MS = 60_000;

/** What a chat's tab is: the place it shows. */
export const TAB_PLACE = { home: 'home', chat: 'chat', other: 'other' } as const;
export type TTabPlace = (typeof TAB_PLACE)[keyof typeof TAB_PLACE];

/** What the rules read of one tab. */
export interface ITidyTab {
  id: string;
  place: TTabPlace;
  /** The chat's status; null when the tab is not a chat or its chat has not come yet (it is then left as it is). */
  status: TChatStatus | null;
  /** The chat has no message of the person's and nothing on its way (no turn, no queue, the agent not at work). */
  empty: boolean;
  /** The chat's composer holds something. */
  draft: boolean;
  /** Unix ms the person left the tab; null while it is active or not looked at yet. */
  leftAt: number | null;
  /** Unix ms of the chat's last activity; null when not a chat. */
  activityAt: number | null;
}

/** How much a tab wants the person: 0 an answer, 1 a reading, 2 nothing. */
export const RANK = { answer: 0, read: 1, none: 2 } as const;
export type TRank = (typeof RANK)[keyof typeof RANK];

const ANSWER = new Set<TChatStatus>([CHAT_STATUS.question, CHAT_STATUS.perm, CHAT_STATUS.plan]);
const READ = new Set<TChatStatus>([CHAT_STATUS.done, CHAT_STATUS.failed, CHAT_STATUS.stopped]);

export const rankOf = (status: TChatStatus | null): TRank => {
  if (status !== null && ANSWER.has(status)) return RANK.answer;
  if (status !== null && READ.has(status)) return RANK.read;
  return RANK.none;
};

/** Nothing was done in the tab: leaving it closes it. */
export const isUntouched = (tab: ITidyTab): boolean => {
  if (tab.place === TAB_PLACE.home) return true;
  return tab.place === TAB_PLACE.chat && tab.status === CHAT_STATUS.none && tab.empty && !tab.draft;
};

/** The tab has been left alone long enough and holds nothing for the person. */
export const isIdle = (tab: ITidyTab, now: number): boolean => {
  if (tab.leftAt === null) return false;
  const since = Math.max(tab.leftAt, tab.activityAt ?? 0);
  if (now - since < TAB_IDLE_MS) return false;
  if (tab.place !== TAB_PLACE.chat) return true;
  return tab.status === CHAT_STATUS.none && !tab.draft;
};

/**
 * Where a tab whose rank has just risen goes: behind the leading tabs that want the person as much or more. Null when
 * it is there already or further ahead.
 */
export const raiseIndex = (
  order: readonly string[],
  ranks: ReadonlyMap<string, TRank>,
  id: string,
): number | null => {
  const rank = ranks.get(id) ?? RANK.none;
  if (rank === RANK.none) return null;
  const from = order.indexOf(id);
  if (from < 0) return null;
  const rest = order.filter((x) => x !== id);
  let index = 0;
  while (index < rest.length && (ranks.get(rest[index]) ?? RANK.none) <= rank) index += 1;
  return index < from ? index : null;
};
