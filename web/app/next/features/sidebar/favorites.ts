// The favorites of the sidebar: how the pinned chats stand (groups, order) and what a drop of a chat or a group on
// a place of the list asks of the machine. Pure: the pointer and the DOM are use-drag's, the commands are the owner's.
import { byPrompt } from '@/next/features/sidebar/group';
import type { ISidebarChat, ISidebarFavorites } from '@/next/features/sidebar/types';
import { EDGE, type TEdge } from '@/next/ui/use-drag';

export interface IFavGroupView {
  id: string;
  name: string;
  chats: readonly ISidebarChat[];
}

export interface IFavView {
  groups: readonly IFavGroupView[];
  loose: readonly ISidebarChat[];
}

export const EMPTY_VIEW: IFavView = { groups: [], loose: [] };

/**
 * The favorites as they are drawn, the way the machine reads them: a chat stands where the layout puts it (the first
 * place it names), one the layout does not name comes first among the loose, the one written to last first, and a name of a chat that
 * is not pinned (or gone) is skipped.
 */
export const favoritesOf = (chats: readonly ISidebarChat[], layout: ISidebarFavorites): IFavView => {
  const pinned = new Map(
    chats
      .filter((chat) => chat.pinned)
      .sort(byPrompt)
      .map((chat) => [chat.id, chat]),
  );
  const seen = new Set<string>();
  const take = (ids: readonly string[]): ISidebarChat[] => {
    const out: ISidebarChat[] = [];
    for (const id of ids) {
      const chat = pinned.get(id);
      if (chat && !seen.has(id)) {
        seen.add(id);
        out.push(chat);
      }
    }
    return out;
  };
  const groups = layout.groups.map((group) => ({ id: group.id, name: group.name, chats: take(group.chats) }));
  const loose = take(layout.loose);
  const fresh = [...pinned.values()].filter((chat) => !seen.has(chat.id));
  return { groups, loose: [...fresh, ...loose] };
};

export const isFavEmpty = (view: IFavView): boolean => view.groups.length === 0 && view.loose.length === 0;

/** Where a favorite chat stands: its group's id, or null among the loose; undefined when it is not a favorite. */
export const groupOfChat = (view: IFavView, id: string): string | null | undefined => {
  for (const group of view.groups) if (group.chats.some((chat) => chat.id === id)) return group.id;
  return view.loose.some((chat) => chat.id === id) ? null : undefined;
};

/** The chat next to `id` in its place and whether each neighbour exists, for «Выше» / «Ниже». */
export const neighboursOf = (view: IFavView, id: string): { prev: boolean; next: boolean } => {
  const place = [...view.groups.map((group) => group.chats), view.loose].find((list) =>
    list.some((chat) => chat.id === id),
  );
  const i = place?.findIndex((chat) => chat.id === id) ?? -1;
  return { prev: i > 0, next: place !== undefined && i >= 0 && i < place.length - 1 };
};

// ── What a drag is, what it can drop on, and what the drop asks ──

export const DRAG = { chat: 'chat', group: 'group' } as const;
export type TDragKind = (typeof DRAG)[keyof typeof DRAG];
export const DRAG_KINDS: readonly TDragKind[] = Object.values(DRAG);
export interface IDragSource {
  kind: TDragKind;
  id: string;
}

export const DROP = {
  favorites: 'favorites',
  group: 'group',
  chat: 'chat',
  project: 'project',
  recent: 'recent',
} as const;
export type TDropKind = (typeof DROP)[keyof typeof DROP];
export const DROP_KINDS: readonly TDropKind[] = Object.values(DROP);

export { EDGE, type TEdge };

export interface IDropTarget {
  kind: TDropKind;
  /** The group, chat or project; absent for the favorites and the recent chats as a whole. */
  id?: string;
  edge?: TEdge;
}

export const PLAN = {
  place: 'place',
  moveGroup: 'move-group',
  move: 'move',
  unpin: 'unpin',
  attach: 'attach',
} as const;
export type TDropPlan =
  | { op: typeof PLAN.place; id: string; groupId: string | null; beforeId: string | null }
  | { op: typeof PLAN.moveGroup; id: string; beforeId: string | null }
  /** A chat into a project (null: out of any); a pinned one leaves the favorites with it, for it would show there. */
  | { op: typeof PLAN.move; id: string; projectId: string | null; unpin: boolean }
  | { op: typeof PLAN.unpin; id: string }
  /** A chat carried out of the list onto the composer: the open chat's next message points at it. */
  | { op: typeof PLAN.attach; id: string };

interface ILayoutIds {
  groups: { id: string; chats: string[] }[];
  loose: string[];
}

const idsOf = (view: IFavView): ILayoutIds => ({
  groups: view.groups.map((group) => ({ id: group.id, chats: group.chats.map((chat) => chat.id) })),
  loose: view.loose.map((chat) => chat.id),
});

/** What the machine makes of «place»: the same layout after it (it mirrors the runtime's wire.Favorites.Placed). */
const placed = (view: IFavView, id: string, groupId: string | null, beforeId: string | null): ILayoutIds => {
  const out = idsOf(view);
  const here = groupId === null ? out.loose : out.groups.find((group) => group.id === groupId)?.chats;
  if (beforeId === id && here?.includes(id)) return out;
  out.loose = out.loose.filter((x) => x !== id);
  for (const group of out.groups) group.chats = group.chats.filter((x) => x !== id);
  const target = groupId === null ? out.loose : out.groups.find((group) => group.id === groupId)?.chats;
  if (!target) return out;
  const at = beforeId === null ? -1 : target.indexOf(beforeId);
  target.splice(at < 0 ? target.length : at, 0, id);
  return out;
};

const sameLayout = (a: ILayoutIds, b: ILayoutIds): boolean => JSON.stringify(a) === JSON.stringify(b);

const placeOrNothing = (
  view: IFavView,
  id: string,
  groupId: string | null,
  beforeId: string | null,
): TDropPlan | null => {
  if (sameLayout(idsOf(view), placed(view, id, groupId, beforeId))) return null;
  return { op: PLAN.place, id, groupId, beforeId };
};

const after = (list: readonly { id: string }[], id: string): string | null => {
  const i = list.findIndex((item) => item.id === id);
  return list[i + 1]?.id ?? null;
};

const chatPlan = (
  source: string,
  target: IDropTarget,
  view: IFavView,
  chats: readonly ISidebarChat[],
): TDropPlan | null => {
  const chat = chats.find((item) => item.id === source);
  if (!chat) return null;
  switch (target.kind) {
    case DROP.favorites:
      return placeOrNothing(view, source, null, null);
    case DROP.group:
      return target.id ? placeOrNothing(view, source, target.id, null) : null;
    case DROP.chat: {
      if (!target.id || target.id === source) return null;
      const groupId = groupOfChat(view, target.id);
      if (groupId === undefined) return null;
      const place = groupId === null ? view.loose : (view.groups.find((g) => g.id === groupId)?.chats ?? []);
      const before = target.edge === EDGE.after ? after(place, target.id) : target.id;
      return placeOrNothing(view, source, groupId, before);
    }
    case DROP.project:
      if (!target.id || (!chat.pinned && chat.projectId === target.id)) return null;
      return { op: PLAN.move, id: source, projectId: target.id, unpin: chat.pinned };
    case DROP.recent:
      if (chat.pinned) return { op: PLAN.unpin, id: source };
      return chat.projectId === null ? null : { op: PLAN.move, id: source, projectId: null, unpin: false };
  }
};

const groupPlan = (source: string, target: IDropTarget, view: IFavView): TDropPlan | null => {
  let before: string | null;
  if (target.kind === DROP.favorites) before = null;
  else if (target.kind === DROP.group && target.id) {
    if (target.id === source) return null;
    before = target.edge === EDGE.after ? after(view.groups, target.id) : target.id;
  } else if (target.kind === DROP.chat && target.id) {
    // Over a chat of a group: after that group; over a loose one: last.
    const groupId = groupOfChat(view, target.id);
    before = groupId ? after(view.groups, groupId) : null;
    if (groupId === source) return null;
  } else return null;
  const order = view.groups.map((group) => group.id).filter((id) => id !== source);
  const at = before === null ? order.length : order.indexOf(before);
  order.splice(at < 0 ? order.length : at, 0, source);
  if (order.join() === view.groups.map((group) => group.id).join()) return null;
  return { op: PLAN.moveGroup, id: source, beforeId: before };
};

/** What dropping `source` on `target` asks of the machine; null when the place takes it as nothing (or not at all). */
export const resolveDrop = (
  source: IDragSource,
  target: IDropTarget,
  view: IFavView,
  chats: readonly ISidebarChat[],
): TDropPlan | null =>
  source.kind === DRAG.chat ? chatPlan(source.id, target, view, chats) : groupPlan(source.id, target, view);

/** A favorite chat one place up (-1) or down (1) in its group or among the loose; null at the end of the place. */
export const shiftPlan = (view: IFavView, id: string, direction: -1 | 1): TDropPlan | null => {
  const groupId = groupOfChat(view, id);
  if (groupId === undefined) return null;
  const list = groupId === null ? view.loose : (view.groups.find((g) => g.id === groupId)?.chats ?? []);
  const i = list.findIndex((chat) => chat.id === id);
  if (i < 0) return null;
  if (direction < 0) return i > 0 ? { op: PLAN.place, id, groupId, beforeId: list[i - 1]?.id ?? null } : null;
  return i < list.length - 1 ? { op: PLAN.place, id, groupId, beforeId: list[i + 2]?.id ?? null } : null;
};

/** A group one place up (-1) or down (1); null at the end of the list. */
export const shiftGroupPlan = (view: IFavView, id: string, direction: -1 | 1): TDropPlan | null => {
  const i = view.groups.findIndex((group) => group.id === id);
  if (i < 0) return null;
  if (direction < 0)
    return i > 0 ? { op: PLAN.moveGroup, id, beforeId: view.groups[i - 1]?.id ?? null } : null;
  return i < view.groups.length - 1
    ? { op: PLAN.moveGroup, id, beforeId: view.groups[i + 2]?.id ?? null }
    : null;
};
