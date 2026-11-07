// What the library shows from its cards: the filters that combine with the search, the sorts, and the folders
// as a tree one level deep. Pure, so the screen only draws what it answers.
import type { IDesignCard, TDesignKind } from '@/next/features/designly/designly-home';
import type { IFolder } from '@/next/features/designly/api';

export const SORT = { changed: 'changed', oldest: 'oldest', name: 'name' } as const;
export type TSort = (typeof SORT)[keyof typeof SORT];
export const SORTS = [SORT.changed, SORT.oldest, SORT.name] as const;

/** «Недавние» are the projects changed within this long. */
export const RECENT_MS = 7 * 24 * 60 * 60 * 1000;

export interface ILibraryFilters {
  kind: TDesignKind | null;
  recent: boolean;
  /** The design system or kit a project was built on (its id); null for any. */
  system: string | null;
  sort: TSort;
}

export const NO_FILTERS: ILibraryFilters = { kind: null, recent: false, system: null, sort: SORT.changed };

/** Something narrows the list (the sort does not). */
export const isNarrowed = (f: ILibraryFilters): boolean => f.kind !== null || f.recent || f.system !== null;

export const matchesFilters = (card: IDesignCard, f: ILibraryFilters, now: number): boolean => {
  if (f.kind !== null && (card.appKind !== undefined || card.kind !== f.kind)) return false;
  if (f.recent && now - card.updatedAt > RECENT_MS) return false;
  if (f.system !== null && card.system?.id !== f.system) return false;
  return true;
};

const byName = (a: IDesignCard, b: IDesignCard) =>
  a.title.localeCompare(b.title, undefined, { sensitivity: 'base' });

/** Pinned first, then the sort the person chose. */
export const sortCards = (cards: IDesignCard[], sort: TSort): IDesignCard[] =>
  cards.sort((a, b) => {
    const pin = Number(b.pinned) - Number(a.pinned);
    if (pin !== 0) return pin;
    if (sort === SORT.name) return byName(a, b);
    if (sort === SORT.oldest) return a.updatedAt - b.updatedAt;
    return b.updatedAt - a.updatedAt;
  });

/** The design systems the cards were built on, by name. */
export const systemsOf = (cards: readonly IDesignCard[]): { id: string; label: string }[] => {
  const seen = new Map<string, string>();
  for (const c of cards) if (c.system && !seen.has(c.system.id)) seen.set(c.system.id, c.system.label);
  return [...seen].map(([id, label]) => ({ id, label })).sort((a, b) => a.label.localeCompare(b.label));
};

/** The folders directly under `parent` ('' is the top), by name. */
export const childrenOf = (folders: readonly IFolder[], parent: string): IFolder[] =>
  folders.filter((f) => f.parent === parent).sort((a, b) => a.name.localeCompare(b.name));

/** A folder and the folders under it. */
export const subtreeIds = (folders: readonly IFolder[], id: string): Set<string> =>
  new Set([id, ...folders.filter((f) => f.parent === id).map((f) => f.id)]);

/** How many projects lie in a folder and under it. */
export const projectsIn = (
  cards: readonly IDesignCard[],
  folders: readonly IFolder[],
  id: string,
): number => {
  const ids = subtreeIds(folders, id);
  return cards.filter((c) => c.folder !== null && c.folder !== undefined && ids.has(c.folder)).length;
};

/** The folders to move into, in the order the dialog lists them: a top folder, then its own. */
export const folderChoices = (folders: readonly IFolder[]): { folder: IFolder; depth: 0 | 1 }[] =>
  childrenOf(folders, '').flatMap((top) => [
    { folder: top, depth: 0 as const },
    ...childrenOf(folders, top.id).map((sub) => ({ folder: sub, depth: 1 as const })),
  ]);
