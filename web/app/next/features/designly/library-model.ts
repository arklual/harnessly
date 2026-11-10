// What the library shows from its cards (HRN-860): the filters that combine with the search, the sorts, and the folders
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

/** The demo projects every library starts with (HRN-813); the tour's lessons are about them. */
export const DEMO_IDS = ['relay-site', 'relay-deck', 'relay-video', 'relay-ds'] as const;

/** The next click on the way to a card (the tour's), by what the library shows now. */
export const ROUTE = {
  /** The card is on screen. */
  card: 'card',
  /** A folder tile in the folder that is open leads on. */
  folder: 'folder',
  /** The card lies in another folder: back to the library's top. */
  up: 'up',
  /** A search, a filter, another tab or the trash hides it. */
  reset: 'reset',
  /** The library has no such project (deleted, or never there). */
  gone: 'gone',
} as const;
export type TRoute = (typeof ROUTE)[keyof typeof ROUTE];

export interface ILibraryRoute {
  route: TRoute;
  /** For `folder`: the tile to open. */
  folder?: string;
}

interface IRouteView {
  folders: readonly IFolder[];
  /** The folder that is open; '' is the top. */
  here: string;
  trashOpen: boolean;
  /** The card passes the search, the filters and the tab (so it is among the cards the view lists). */
  matches: boolean;
  /** A search or a filter looks through every folder: the card then stands in the flat list. */
  searching: boolean;
  /** Folders exist at all: an older runtime lays the library out flat. */
  organized: boolean;
}

/** Where the person must click to reach the card, given what the library shows. */
export const routeToCard = (card: IDesignCard | undefined, view: IRouteView): ILibraryRoute => {
  if (!card) return { route: ROUTE.gone };
  if (view.trashOpen || !view.matches) return { route: ROUTE.reset };
  if (!view.organized || view.searching) return { route: ROUTE.card };
  const place = view.folders.find((f) => f.id === card.folder);
  const target = place?.id ?? '';
  if (target === view.here) return { route: ROUTE.card };
  const top = place?.parent ? place.parent : target;
  // A folder shows its tiles under it only from its parent; a top folder is reached from the top.
  if (place?.parent && view.here === place.parent) return { route: ROUTE.folder, folder: target };
  if (view.here === '' && top !== '') return { route: ROUTE.folder, folder: top };
  return { route: ROUTE.up };
};
