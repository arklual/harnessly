/** What was dropped on the list or picked with `webkitdirectory`: files with the folder each one sits in. */
import { t } from '@/lib/i18n';

export interface IDiskPick {
  file: File;
  /** Folders between the target folder and the file, `a/b`; empty for a file dropped on its own. */
  dir: string;
}

/** The part of `FileSystemEntry` the walk uses; the browser's own types are not in the DOM lib for directories. */
export interface IDropEntry {
  isFile: boolean;
  isDirectory: boolean;
  name: string;
  file?: (done: (file: File) => void, fail: (error: unknown) => void) => void;
  createReader?: () => {
    readEntries: (done: (entries: IDropEntry[]) => void, fail: (error: unknown) => void) => void;
  };
}

export interface IDropItem {
  kind: string;
  webkitGetAsEntry?: () => IDropEntry | null;
  getAsFile?: () => File | null;
}

const readFile = (entry: IDropEntry): Promise<File> =>
  new Promise((resolve, reject) => {
    if (!entry.file) reject(new Error(t('lib.hlDiskDrop.readFailed')));
    else entry.file(resolve, reject);
  });

/** A reader hands out a batch at a time (100 in Chromium) and answers an empty one at the end. */
const readAll = async (entry: IDropEntry): Promise<IDropEntry[]> => {
  const reader = entry.createReader?.();
  if (!reader) return [];
  const all: IDropEntry[] = [];
  for (;;) {
    const batch = await new Promise<IDropEntry[]>((resolve, reject) => reader.readEntries(resolve, reject));
    if (batch.length === 0) return all;
    all.push(...batch);
  }
};

const walk = async (entry: IDropEntry, dir: string, out: IDiskPick[]): Promise<void> => {
  if (entry.isFile) {
    out.push({ file: await readFile(entry), dir });
    return;
  }
  if (!entry.isDirectory) return;
  const inner = dir ? `${dir}/${entry.name}` : entry.name;
  for (const child of await readAll(entry)) await walk(child, inner, out);
};

/** Entries are taken first and synchronously: the browser drops the whole list once the handler yields. */
export const collectDropped = async (items: ArrayLike<IDropItem>): Promise<IDiskPick[]> => {
  const picks: IDiskPick[] = [];
  const entries: IDropEntry[] = [];
  for (const item of Array.from(items)) {
    if (item.kind !== 'file') continue;
    const entry = item.webkitGetAsEntry?.() ?? null;
    if (entry) {
      entries.push(entry);
      continue;
    }
    const file = item.getAsFile?.() ?? null;
    if (file) picks.push({ file, dir: '' });
  }
  for (const entry of entries) await walk(entry, '', picks);
  return picks;
};

/** A folder chosen with `<input webkitdirectory>`: the relative path of each file starts with the chosen folder. */
export const picksFromInput = (files: ArrayLike<File>): IDiskPick[] =>
  Array.from(files).map((file) => {
    const relative = (file as File & { webkitRelativePath?: string }).webkitRelativePath ?? '';
    const cut = relative.lastIndexOf('/');
    return { file, dir: cut > 0 ? relative.slice(0, cut) : '' };
  });
