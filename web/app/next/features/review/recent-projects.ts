// The projects the person opened in the review picker last, newest first: they stand on top of the list. This
// browser's only; with storage blocked the list starts empty on every page.
const KEY = 'hx-review-recent';
export const RECENT_MAX = 8;

export interface IRecentProject {
  path: string;
  name: string;
}

const isRecent = (v: unknown): v is IRecentProject =>
  typeof v === 'object' &&
  v !== null &&
  typeof (v as IRecentProject).path === 'string' &&
  (v as IRecentProject).path !== '' &&
  typeof (v as IRecentProject).name === 'string';

export const parseRecent = (raw: string | null): IRecentProject[] => {
  if (!raw) return [];
  try {
    const v: unknown = JSON.parse(raw);
    return Array.isArray(v) ? v.filter(isRecent).slice(0, RECENT_MAX) : [];
  } catch {
    return [];
  }
};

/** The list with `project` on top, once. */
export const pushRecent = (list: readonly IRecentProject[], project: IRecentProject): IRecentProject[] => [
  project,
  ...list.filter((x) => x.path !== project.path).slice(0, RECENT_MAX - 1),
];

/** The found projects with the recent ones first, in their own order; a recent one the search does not find stays out. */
export const withRecentFirst = <T extends { path: string }>(
  found: readonly T[],
  recent: readonly IRecentProject[],
): T[] => {
  const rank = new Map(recent.map((x, i) => [x.path, i]));
  const top = found
    .filter((x) => rank.has(x.path))
    .sort((a, b) => (rank.get(a.path) ?? 0) - (rank.get(b.path) ?? 0));
  return [...top, ...found.filter((x) => !rank.has(x.path))];
};

export const readRecent = (): IRecentProject[] => {
  try {
    return parseRecent(localStorage.getItem(KEY));
  } catch {
    return [];
  }
};

export const writeRecent = (list: readonly IRecentProject[]) => {
  try {
    localStorage.setItem(KEY, JSON.stringify(list));
  } catch {
    // Storage blocked: the order is this page's only.
  }
};
