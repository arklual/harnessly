import { DAY, startOfDay } from '@/next/i18n/format';
import type { ISidebarChat, ISidebarProject } from '@/next/features/sidebar/types';

export const DAY_GROUP = { today: 'today', yesterday: 'yesterday', week: 'week', older: 'older' } as const;
export type TDayGroup = (typeof DAY_GROUP)[keyof typeof DAY_GROUP];

const DAY_GROUPS: readonly TDayGroup[] = [
  DAY_GROUP.today,
  DAY_GROUP.yesterday,
  DAY_GROUP.week,
  DAY_GROUP.older,
];

export const dayGroupOf = (at: number, now: number): TDayGroup => {
  const today = startOfDay(now);
  if (at >= today) return DAY_GROUP.today;
  if (at >= today - DAY) return DAY_GROUP.yesterday;
  if (at >= today - 6 * DAY) return DAY_GROUP.week;
  return DAY_GROUP.older;
};

export interface IProjectGroup {
  project: ISidebarProject;
  chats: readonly ISidebarChat[];
}

export interface IDayGroup {
  group: TDayGroup;
  chats: readonly ISidebarChat[];
}

export interface ISidebarSections {
  pinned: readonly ISidebarChat[];
  projects: readonly IProjectGroup[];
  days: readonly IDayGroup[];
  /** The chats each chat delegated, in the order they were started; they are drawn under it, not in the lists. */
  children: ReadonlyMap<string, readonly ISidebarChat[]>;
}

const NO_CHILDREN: readonly ISidebarChat[] = [];

/** The delegated chats under a chat, none for one that delegated nothing. */
export const childrenOf = (sections: ISidebarSections, id: string): readonly ISidebarChat[] =>
  sections.children.get(id) ?? NO_CHILDREN;

/** Newest message of the person first; chats written at the same moment stay in the order of their creation. */
export const byPrompt = (a: ISidebarChat, b: ISidebarChat) =>
  b.lastPromptAt - a.lastPromptAt || b.createdAt - a.createdAt;

/**
 * Where each chat goes: a pinned one to «Закреплённые» only, a delegated one under the chat that delegated it, one in a
 * known project under it, the rest by day. Within
 * a section the one the person wrote to last is first (an agent's reply does not move a chat); empty day groups are left out, empty projects are kept (they can be filled).
 */
export const sectionsOf = (
  chats: readonly ISidebarChat[],
  projects: readonly ISidebarProject[],
  now: number,
): ISidebarSections => {
  const known = new Set(projects.map((project) => project.id));
  const ids = new Set(chats.map((chat) => chat.id));
  // A delegated chat goes under its parent while the parent is in the list; one whose parent went stands on its own.
  const nested = (chat: ISidebarChat) => chat.parentId !== undefined && ids.has(chat.parentId);
  const children = new Map<string, ISidebarChat[]>();
  for (const chat of [...chats].sort((a, b) => a.createdAt - b.createdAt)) {
    const parent = chat.parentId;
    if (parent === undefined || !ids.has(parent)) continue;
    const kids = children.get(parent);
    if (kids) kids.push(chat);
    else children.set(parent, [chat]);
  }
  const sorted = [...chats].sort(byPrompt);
  const pinned = sorted.filter((chat) => chat.pinned);
  const rest = sorted.filter((chat) => !chat.pinned && !nested(chat));
  const inProject = (chat: ISidebarChat) => chat.projectId !== null && known.has(chat.projectId);
  const loose = rest.filter((chat) => !inProject(chat));
  return {
    pinned,
    projects: projects.map((project) => ({
      project,
      chats: rest.filter((chat) => chat.projectId === project.id),
    })),
    days: DAY_GROUPS.map((group) => ({
      group,
      chats: loose.filter((chat) => dayGroupOf(chat.lastPromptAt, now) === group),
    })).filter((day) => day.chats.length > 0),
    children,
  };
};
