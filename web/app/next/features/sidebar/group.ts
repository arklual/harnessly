import { DAY, startOfDay } from '@/next/i18n/format';
import type { ISidebarChat, ISidebarProject } from '@/next/features/sidebar/types';
import { layoutOf, type TShownLayout } from '@/next/core/steady-order';

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

/** The chats an app started for the person (ISidebarChat.from) and the person has not taken yet. */
export interface IAppGroup {
  app: string;
  label: string;
  chats: readonly ISidebarChat[];
}

export interface ISidebarSections {
  pinned: readonly ISidebarChat[];
  projects: readonly IProjectGroup[];
  /** The apps' folders, the one with the newest chat first. */
  apps: readonly IAppGroup[];
  days: readonly IDayGroup[];
  /** The chats each chat delegated, in the order they were started; they are drawn under it, not in the lists. */
  children: ReadonlyMap<string, readonly ISidebarChat[]>;
}

const NO_CHILDREN: readonly ISidebarChat[] = [];

/** The delegated chats under a chat, none for one that delegated nothing. */
export const childrenOf = (sections: ISidebarSections, id: string): readonly ISidebarChat[] =>
  sections.children.get(id) ?? NO_CHILDREN;

/** How many chats a folder shows before «Дальше». */
export const FOLDER_ROWS = 5;

/**
 * A folder's first rows, and how many wait behind «Дальше». The open chat is never hidden: past the first rows it is
 * shown after them, in its place in the order.
 */
export const folderRows = <T extends { id: string }>(
  list: readonly T[],
  currentId: string | null,
  max: number = FOLDER_ROWS,
): { shown: readonly T[]; hidden: number } => {
  if (list.length <= max) return { shown: list, hidden: 0 };
  const shown = list.filter((row, i) => i < max || row.id === currentId);
  return { shown, hidden: list.length - shown.length };
};

/** Newest message of the person first; chats written at the same moment stay in the order of their creation. */
export const byPrompt = (a: ISidebarChat, b: ISidebarChat) =>
  b.lastPromptAt - a.lastPromptAt || b.createdAt - a.createdAt;

/**
 * Where each chat goes: a pinned one to «Закреплённые» only, a delegated one under the chat that delegated it, one in a
 * known project under it, one an app started to that app's folder, the rest by day. Within
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
  const outside = rest.filter((chat) => !inProject(chat));
  const apps = new Map<string, { app: string; label: string; chats: ISidebarChat[] }>();
  for (const chat of outside) {
    if (!chat.from) continue;
    const folder = apps.get(chat.from.app);
    if (folder) folder.chats.push(chat);
    else apps.set(chat.from.app, { app: chat.from.app, label: chat.from.label, chats: [chat] });
  }
  const loose = outside.filter((chat) => !chat.from);
  return {
    pinned,
    projects: projects.map((project) => ({
      project,
      chats: rest.filter((chat) => chat.projectId === project.id),
    })),
    apps: [...apps.values()],
    days: DAY_GROUPS.map((group) => ({
      group,
      chats: loose.filter((chat) => dayGroupOf(chat.lastPromptAt, now) === group),
    })).filter((day) => day.chats.length > 0),
    children,
  };
};

/** The sidebar's lists by key: the pinned, each project, each app's folder, each day. */
const listsOf = (sections: ISidebarSections): readonly { id: string; chats: readonly ISidebarChat[] }[] => [
  { id: 'pinned', chats: sections.pinned },
  ...sections.projects.map(({ project, chats }) => ({ id: `p:${project.id}`, chats })),
  ...sections.apps.map(({ app, chats }) => ({ id: `a:${app}`, chats })),
  ...sections.days.map(({ group, chats }) => ({ id: `d:${group}`, chats })),
];

/** Where each list shows which chats now. */
export const sidebarLayout = (sections: ISidebarSections): TShownLayout =>
  layoutOf(
    listsOf(sections),
    (list) => list.chats,
    (chat) => chat.id,
  );

/**
 * The lists held as they were shown while the pointer or the keyboard is in them: a chat written to from
 * another device, an agent's update or a chat that came in moves no row under the person. The chats shown stay in
 * their list and place with their fresh data; a chat that is gone leaves; a chat not shown yet waits for the hold to
 * end, as it would come in above the row the person is on. The person's own change (a pin, a move) ends the hold.
 */
export const holdSections = (sections: ISidebarSections, layout: TShownLayout | null): ISidebarSections => {
  if (layout === null) return sections;
  const lists = listsOf(sections);
  const present = new Set(lists.map((list) => list.id));
  const byId = new Map(lists.flatMap((list) => list.chats.map((chat) => [chat.id, chat] as const)));
  const heldIn = new Map<string, string>();
  for (const [list, ids] of layout) {
    if (!present.has(list)) continue;
    for (const id of ids) if (byId.has(id) && !heldIn.has(id)) heldIn.set(id, list);
  }
  const hold = (list: string): readonly ISidebarChat[] =>
    (layout.get(list) ?? []).flatMap((id) => {
      const chat = byId.get(id);
      return chat !== undefined && heldIn.get(id) === list ? [chat] : [];
    });
  return {
    pinned: hold('pinned'),
    projects: sections.projects.map((item) => ({ ...item, chats: hold(`p:${item.project.id}`) })),
    apps: sections.apps
      .map((item) => ({ ...item, chats: hold(`a:${item.app}`) }))
      .filter((item) => item.chats.length > 0),
    days: sections.days
      .map((item) => ({ ...item, chats: hold(`d:${item.group}`) }))
      .filter((item) => item.chats.length > 0),
    children: sections.children,
  };
};
