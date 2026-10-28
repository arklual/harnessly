// The site upload from the browser: a folder (or a dropped set of files) is hashed here and sent by the deploy
// protocol of docs/hub/sites.md — manifest first, then only the files the hub does not have, then the commit. A zip
// goes in one request and the hub unpacks it.
import { ApiError, type HubApi } from './api';
import { errorText } from './errors';
import { currentLang } from './i18n';
import type { SiteDeploy } from './types';

export type PickedFile = { path: string; file: File };

/** What goes with a folder is not a site: the same skips as `harnessly site deploy`. */
const SKIPPED = /(^|\/)(\.git|node_modules|\.DS_Store)(\/|$)/;

export const isZip = (f: File): boolean => /\.zip$/i.test(f.name) || f.type === 'application/zip';

/** The files of a folder chosen by the `webkitdirectory` input, the folder's own name cut from the paths. */
export const fromInput = (list: FileList): PickedFile[] =>
  Array.from(list)
    .map((file) => ({ path: file.webkitRelativePath.split('/').slice(1).join('/'), file }))
    .filter((p) => p.path !== '' && !SKIPPED.test(p.path));

const readEntries = (reader: FileSystemDirectoryReader): Promise<FileSystemEntry[]> =>
  new Promise((resolve, reject) => reader.readEntries(resolve, reject));

const fileOf = (entry: FileSystemFileEntry): Promise<File> =>
  new Promise((resolve, reject) => entry.file(resolve, reject));

const walk = async (entry: FileSystemEntry, prefix: string, out: PickedFile[]): Promise<void> => {
  if (SKIPPED.test(prefix + entry.name)) return;
  if (entry.isFile) {
    out.push({ path: prefix + entry.name, file: await fileOf(entry as FileSystemFileEntry) });
    return;
  }
  const reader = (entry as FileSystemDirectoryEntry).createReader();
  // readEntries gives a batch at a time, until it gives an empty one.
  for (;;) {
    const batch = await readEntries(reader);
    if (batch.length === 0) break;
    for (const child of batch) await walk(child, `${prefix}${entry.name}/`, out);
  }
};

export type Dropped = { zip: File } | { files: PickedFile[] } | null;

/** What was dropped: one zip, or a folder (its name cut from the paths) or a set of files. */
export const fromDrop = async (data: DataTransfer): Promise<Dropped> => {
  const items = Array.from(data.items);
  const entries = items.map((i) => i.webkitGetAsEntry?.() ?? null);
  const only = entries.length === 1 ? entries[0] : null;
  if (only?.isDirectory) {
    const out: PickedFile[] = [];
    const reader = (only as FileSystemDirectoryEntry).createReader();
    for (;;) {
      const batch = await readEntries(reader);
      if (batch.length === 0) break;
      for (const child of batch) await walk(child, '', out);
    }
    return out.length > 0 ? { files: out } : null;
  }
  const first = data.files[0];
  if (data.files.length === 1 && first && isZip(first)) return { zip: first };
  const out: PickedFile[] = [];
  for (const e of entries) if (e) await walk(e, '', out);
  return out.length > 0 ? { files: out } : null;
};

const hex = (buf: ArrayBuffer): string =>
  Array.from(new Uint8Array(buf), (b) => b.toString(16).padStart(2, '0')).join('');

export const sha256 = async (data: Blob): Promise<string> =>
  hex(await crypto.subtle.digest('SHA-256', await data.arrayBuffer()));

/** The folder's own `dist/` or `build/` wrapper is not part of the site: when every path starts with one name, it goes. */
export const trimRoot = (files: PickedFile[]): PickedFile[] => {
  const roots = new Set(files.map((f) => (f.path.includes('/') ? f.path.split('/')[0] : '')));
  if (roots.size !== 1 || roots.has('')) return files;
  return files.map((f) => ({ ...f, path: f.path.split('/').slice(1).join('/') }));
};

const PARALLEL = 6;

/** Sends the files by the protocol; `progress` is told how many of the files that must go are sent. */
export const deployFiles = async (
  api: HubApi,
  space: string,
  site: string,
  files: PickedFile[],
  progress: (sent: number, total: number) => void,
): Promise<SiteDeploy> => {
  const manifest: Record<string, { hash: string; size: number }> = {};
  const byHash = new Map<string, File>();
  for (const { path, file } of files) {
    const hash = await sha256(file);
    manifest[path] = { hash, size: file.size };
    byHash.set(hash, file);
  }
  const { id, missing } = await api.sites.begin(space, site, manifest);
  let sent = 0;
  progress(0, missing.length);
  const queue = [...missing];
  const worker = async () => {
    for (let hash = queue.shift(); hash !== undefined; hash = queue.shift()) {
      const file = byHash.get(hash);
      if (!file) throw new ApiError(0, 'internal', '');
      await api.sites.blob(space, site, id, hash, file);
      progress(++sent, missing.length);
    }
  };
  await Promise.all(Array.from({ length: Math.min(PARALLEL, missing.length) }, worker));
  return api.sites.commit(space, site, id);
};

/** The hub's own Russian text for what only the sites know (a secret in a file, a limit); else the common wording. */
export const siteErrorText = (e: unknown): string =>
  e instanceof ApiError && e.message !== '' && (e.code === 'internal' || e.code === 'invalid_request')
    ? e.message
    : errorText(e);

export const whenText = (iso: string): string =>
  new Date(iso).toLocaleString(currentLang() === 'en' ? 'en-GB' : 'ru-RU', {
    day: 'numeric',
    month: 'short',
    hour: '2-digit',
    minute: '2-digit',
  });

/** The address without the scheme: the name of the site is what the person reads. */
export const bareAddress = (url: string): string => url.replace(/^https?:\/\//, '').replace(/\/$/, '');

export const SITE_NAME = /^[a-z0-9]([a-z0-9-]{0,28}[a-z0-9])?$/;

/** A name for a site made from what the person typed or a folder is called. */
export const slugName = (text: string): string =>
  text
    .toLowerCase()
    .replace(/\.zip$/, '')
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 30)
    .replace(/-+$/, '');
