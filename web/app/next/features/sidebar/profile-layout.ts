// The sidebar of the person, not of a machine: the projects (folders) and the favorites' layout are one
// document kept by the cluster for the person, the same whichever machine is picked. A project of the profile stands
// for projects of the machines (`members`): a chat is in it when its machine's project is one of them, and a chat of a
// machine the project has no folder on yet gets one there when it moves in. Which chats are favorites stays each
// chat's own `pinned`; the document says only where they stand. Pure: the store writes it, the shell reads it.
import type {
  ConvId,
  Favorites,
  MachineId,
  ProfileMember,
  ProfileSidebar,
  Project,
} from '@/next/protocol/v2';

type IProfileSidebar = ProfileSidebar;
type IProfileMember = ProfileMember;

export const EMPTY_PROFILE: IProfileSidebar = {
  projects: [],
  favorites: { groups: [], loose: [] },
  merged: [],
};

/** The most groups the favorites hold, as a machine allows. */
export const FAV_GROUPS_MAX = 50;

const hex = (n: number): string => {
  const b = new Uint8Array(n);
  crypto.getRandomValues(b);
  return Array.from(b, (x) => x.toString(16).padStart(2, '0')).join('');
};
export const newProfileProjectId = (): string => `pp-${hex(6)}`;
export const newFavGroupId = (): string => `fg-${hex(6)}`;

const sameName = (a: string, b: string): boolean =>
  a.trim().toLocaleLowerCase() === b.trim().toLocaleLowerCase();

const memberKey = (machine: MachineId, project: string): string => `${machine}/${project}`;

/** The project of the profile each machine project stands in, by `machine/project`. */
export const profileProjectOf = (doc: IProfileSidebar): ReadonlyMap<string, string> =>
  new Map(
    doc.projects.flatMap((p) =>
      p.members.map((m): [string, string] => [memberKey(m.machineId, m.projectId), p.id]),
    ),
  );

/** The profile's project a chat stands in: by its machine's project; null outside every project. */
export const chatProjectOf = (
  index: ReadonlyMap<string, string>,
  machine: MachineId,
  project: string | null,
): string | null => (project === null ? null : (index.get(memberKey(machine, project)) ?? null));

/** The machine's project a project of the profile has on `machine`; null while it has none there. */
export const memberOn = (doc: IProfileSidebar, projectId: string, machine: MachineId): string | null =>
  doc.projects.find((p) => p.id === projectId)?.members.find((m) => m.machineId === machine)?.projectId ??
  null;

/** A machine's project joins a project of the profile; a member already there changes nothing. */
export const withMember = (
  doc: IProfileSidebar,
  projectId: string,
  member: IProfileMember,
): IProfileSidebar => {
  if (profileProjectOf(doc).has(memberKey(member.machineId, member.projectId))) return doc;
  return {
    ...doc,
    projects: doc.projects.map((p) => (p.id === projectId ? { ...p, members: [...p.members, member] } : p)),
  };
};

/** A new project of the profile, by a name alone; its folders come on the machines as chats move in. */
export const withProject = (
  doc: IProfileSidebar,
  name: string,
  id: string,
  now: number,
): IProfileSidebar => ({
  ...doc,
  projects: [...doc.projects, { id, name: name.trim(), createdAt: now, members: [] }],
});

/**
 * The projects and the favorites of one machine merged into the profile's, nothing lost: a project joins the profile's of
 * the same name or becomes one of its own; the favorites' groups join by name, their chats added where missing, and the
 * loose ones after the loose. The machine's favorites are merged once; its projects whenever one is new. The same
 * document back when nothing changes, so the caller writes only a change.
 */
export const mergeMachine = (
  doc: IProfileSidebar,
  machine: MachineId,
  projects: readonly Project[],
  favorites: Favorites | null,
  ids: { project: () => string; group: () => string } = {
    project: newProfileProjectId,
    group: newFavGroupId,
  },
): IProfileSidebar => {
  let next = doc;
  const known = profileProjectOf(doc);
  for (const p of [...projects].sort((a, b) => a.createdAt - b.createdAt)) {
    if (known.has(memberKey(machine, p.id))) continue;
    const same = next.projects.find((q) => sameName(q.name, p.name));
    const member = { machineId: machine, projectId: p.id };
    next = same
      ? withMember(next, same.id, member)
      : {
          ...next,
          projects: [
            ...next.projects,
            { id: ids.project(), name: p.name, createdAt: p.createdAt, members: [member] },
          ],
        };
  }
  if (next.merged.includes(machine)) return next;
  const placed = new Set([...next.favorites.groups.flatMap((g) => g.chats), ...next.favorites.loose]);
  const fresh = (chats: readonly ConvId[]): ConvId[] =>
    chats.filter((c) => {
      if (placed.has(c)) return false;
      placed.add(c);
      return true;
    });
  let groups = next.favorites.groups;
  for (const g of favorites?.groups ?? []) {
    const same = groups.find((h) => sameName(h.name, g.name));
    if (same) {
      const add = fresh(g.chats);
      groups = groups.map((h) => (h === same ? { ...h, chats: [...h.chats, ...add] } : h));
    } else if (groups.length < FAV_GROUPS_MAX) {
      const taken = groups.some((h) => h.id === g.id);
      groups = [...groups, { id: taken ? ids.group() : g.id, name: g.name, chats: fresh(g.chats) }];
    }
  }
  const loose = [...next.favorites.loose, ...fresh(favorites?.loose ?? [])];
  return { ...next, favorites: { groups, loose }, merged: [...next.merged, machine] };
};

const without = (f: Favorites, chat: ConvId): Favorites => ({
  groups: f.groups.map((g) => ({ ...g, chats: g.chats.filter((c) => c !== chat) })),
  loose: f.loose.filter((c) => c !== chat),
});

const insertBefore = (list: readonly ConvId[], chat: ConvId, before: ConvId | null): ConvId[] => {
  const at = before === null ? -1 : list.indexOf(before);
  return at < 0 ? [...list, chat] : [...list.slice(0, at), chat, ...list.slice(at)];
};

/** A chat in its place among the favorites: in a group or with null ungrouped, before a chat or last; null when the
 *  group is gone. */
export const favPlaced = (
  doc: IProfileSidebar,
  chat: ConvId,
  groupId: string | null,
  before: ConvId | null,
): IProfileSidebar | null => {
  if (groupId !== null && !doc.favorites.groups.some((g) => g.id === groupId)) return null;
  const rest = without(doc.favorites, chat);
  const favorites =
    groupId === null
      ? { ...rest, loose: insertBefore(rest.loose, chat, before) }
      : {
          ...rest,
          groups: rest.groups.map((g) =>
            g.id === groupId ? { ...g, chats: insertBefore(g.chats, chat, before) } : g,
          ),
        };
  return { ...doc, favorites };
};

/** A new group last; null when the favorites hold as many as they may. */
export const favGroupAdded = (doc: IProfileSidebar, id: string, name: string): IProfileSidebar | null =>
  doc.favorites.groups.length >= FAV_GROUPS_MAX
    ? null
    : {
        ...doc,
        favorites: {
          ...doc.favorites,
          groups: [...doc.favorites.groups, { id, name: name.trim(), chats: [] }],
        },
      };

const hasGroup = (doc: IProfileSidebar, id: string): boolean => doc.favorites.groups.some((g) => g.id === id);

export const favGroupRenamed = (doc: IProfileSidebar, id: string, name: string): IProfileSidebar | null =>
  hasGroup(doc, id)
    ? {
        ...doc,
        favorites: {
          ...doc.favorites,
          groups: doc.favorites.groups.map((g) => (g.id === id ? { ...g, name: name.trim() } : g)),
        },
      }
    : null;

/** The group goes, its chats stay in the favorites, first among the loose (as on a machine). */
export const favGroupDeleted = (doc: IProfileSidebar, id: string): IProfileSidebar | null => {
  const group = doc.favorites.groups.find((g) => g.id === id);
  if (!group) return null;
  return {
    ...doc,
    favorites: {
      groups: doc.favorites.groups.filter((g) => g.id !== id),
      loose: [...group.chats, ...doc.favorites.loose],
    },
  };
};

/** A group before another, or last with null. */
export const favGroupMoved = (
  doc: IProfileSidebar,
  id: string,
  before: string | null,
): IProfileSidebar | null => {
  const group = doc.favorites.groups.find((g) => g.id === id);
  if (!group || (before !== null && !hasGroup(doc, before))) return null;
  const rest = doc.favorites.groups.filter((g) => g.id !== id);
  const at = before === null || before === id ? -1 : rest.findIndex((g) => g.id === before);
  const groups = at < 0 ? [...rest, group] : [...rest.slice(0, at), group, ...rest.slice(at)];
  return { ...doc, favorites: { ...doc.favorites, groups } };
};
