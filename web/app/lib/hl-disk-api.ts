/** Calls of the Disk tab other than the upload: roots, listing, new folder, download. */
import { appFetch, scopedAppPath } from '@/lib/app-path';
import { t } from '@/lib/i18n';

export const DISK_ENTRY_KIND = { dir: 'dir', file: 'file', link: 'link' } as const;
export type TDiskEntryKind = (typeof DISK_ENTRY_KIND)[keyof typeof DISK_ENTRY_KIND];

export interface IDiskEntry {
  name: string;
  kind: TDiskEntryKind;
  size: number;
  /** Epoch milliseconds. */
  mtime: number;
}

export interface IDiskRoot {
  id: string;
  label: string;
  path: string;
}

export interface IDiskPage {
  entries: IDiskEntry[];
  nextCursor: string | null;
  truncated: boolean;
}

export interface IDiskLocation {
  root: string;
  /** Relative to the root, `a/b`; empty is the root itself. */
  path: string;
}

/** What the person is told, and whether asking again can help: only a network break or a server fault can. */
export class DiskFailure extends Error {
  constructor(
    message: string,
    readonly retryable: boolean,
  ) {
    super(message);
  }
}

const asRecord = (value: unknown): Record<string, unknown> =>
  typeof value === 'object' && value !== null ? (value as Record<string, unknown>) : {};

export const diskFailureOf = (status: number, json: unknown): DiskFailure => {
  const { error, message } = asRecord(json);
  if (status === 404 && error === 'files-unavailable')
    return new DiskFailure(t('lib.hlDiskApi.unavailable'), false);
  if (typeof message === 'string' && message) return new DiskFailure(message, status >= 500);
  if (status === 403) return new DiskFailure(t('lib.hlDiskApi.forbidden'), false);
  if (status === 404) return new DiskFailure(t('lib.hlDiskApi.folderGone'), false);
  if (status >= 500) return new DiskFailure(t('lib.hlDiskApi.machineError', { status }), true);
  return new DiskFailure(t('lib.hlDiskApi.requestFailed', { status }), false);
};

const networkFailure = (): DiskFailure => new DiskFailure(t('lib.hlDiskApi.offline'), true);

const call = async (path: string, init: RequestInit): Promise<unknown> => {
  let response: Response;
  try {
    response = await appFetch(path, init);
  } catch (error) {
    if (error instanceof Error && error.name === 'AbortError') throw error;
    throw networkFailure();
  }
  const json: unknown = await response.json().catch(() => null);
  if (!response.ok) throw diskFailureOf(response.status, json);
  return json;
};

const query = (params: Record<string, string | null>): string => {
  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) if (value !== null) search.set(key, value);
  return search.toString();
};

export const fetchDiskRoots = async (signal: AbortSignal): Promise<IDiskRoot[]> => {
  const { roots } = asRecord(await call('/api/fs/roots', { signal }));
  return Array.isArray(roots) ? (roots as IDiskRoot[]) : [];
};

export const fetchDiskPage = async (
  { root, path }: IDiskLocation,
  cursor: string | null,
  signal: AbortSignal,
): Promise<IDiskPage> => {
  const body = asRecord(await call(`/api/fs/list?${query({ root, path, cursor })}`, { signal }));
  return {
    entries: Array.isArray(body.entries) ? (body.entries as IDiskEntry[]) : [],
    nextCursor: typeof body.nextCursor === 'string' ? body.nextCursor : null,
    truncated: body.truncated === true,
  };
};

export const createDiskFolder = async ({ root, path }: IDiskLocation, name: string): Promise<void> => {
  await call('/api/fs/mkdir', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ root, path, name }),
  });
};

/** The address of a file for a link: the machine of the conversation rides along. */
export const diskDownloadUrl = ({ root, path }: IDiskLocation, name: string): string =>
  scopedAppPath(`/api/fs/download?${query({ root, path: path ? `${path}/${name}` : name })}`);

export interface IDiskFavorite extends IDiskLocation {
  name: string;
  /** The folder is not on the disk now; the entry stays so it comes back with the folder. */
  missing: boolean;
}

export const FAVORITE_STATE = { unknown: 'unknown', pinned: 'pinned', free: 'free' } as const;
export type TFavoriteState = (typeof FAVORITE_STATE)[keyof typeof FAVORITE_STATE];

export const sameDiskPlace = (a: IDiskLocation, b: IDiskLocation): boolean =>
  a.root === b.root && a.path === b.path;

/** `unknown` while the favorites are loading or did not load: nothing is claimed about the folder. */
export const favoriteStateOf = (
  favorites: readonly IDiskFavorite[] | null,
  place: IDiskLocation,
): TFavoriteState => {
  if (favorites === null) return FAVORITE_STATE.unknown;
  return favorites.some((favorite) => sameDiskPlace(favorite, place))
    ? FAVORITE_STATE.pinned
    : FAVORITE_STATE.free;
};

const favoritesOf = (json: unknown): IDiskFavorite[] => {
  const { favorites } = asRecord(json);
  if (!Array.isArray(favorites)) return [];
  return favorites.filter((item): item is IDiskFavorite => {
    const { root, path, name, missing } = asRecord(item);
    return (
      typeof root === 'string' &&
      typeof path === 'string' &&
      typeof name === 'string' &&
      typeof missing === 'boolean'
    );
  });
};

export const fetchDiskFavorites = async (signal: AbortSignal): Promise<IDiskFavorite[]> =>
  favoritesOf(await call('/api/fs/favorites', { signal }));

export const pinDiskFavorite = async ({ root, path }: IDiskLocation): Promise<IDiskFavorite[]> =>
  favoritesOf(
    await call('/api/fs/favorites', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ root, path }),
    }),
  );

export const unpinDiskFavorite = async ({ root, path }: IDiskLocation): Promise<IDiskFavorite[]> =>
  favoritesOf(await call(`/api/fs/favorites?${query({ root, path })}`, { method: 'DELETE' }));

const withoutTrailingSlash = (path: string): string => path.replace(/\/+$/, '');

/** A folder of the machine, as an absolute path, in terms of the roots: the longest root that contains it wins. */
export const locateDiskPlace = (
  absolute: string,
  roots: readonly IDiskRoot[],
): { root: IDiskRoot; path: string } | null => {
  const target = withoutTrailingSlash(absolute);
  let best: { root: IDiskRoot; base: string } | null = null;
  for (const root of roots) {
    const base = withoutTrailingSlash(root.path);
    const inside = target === base || target.startsWith(`${base}/`);
    if (inside && (best === null || base.length > best.base.length)) best = { root, base };
  }
  return best && { root: best.root, path: target.slice(best.base.length).replace(/^\//, '') };
};

/** The path a place has on the machine itself. */
export const diskAbsolutePath = (rootPath: string, path: string): string =>
  path ? `${withoutTrailingSlash(rootPath)}/${path}` : rootPath;
