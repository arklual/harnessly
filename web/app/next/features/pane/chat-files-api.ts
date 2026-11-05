// The conversation's folder over v2 (GET .../files and .../file, PUT .../file): a folder's entries, folders first, a text
// file's content with its revision and its save over that revision; paths are relative to the conversation's cwd. Refusals come in the runtime's own words.
import { wordedPage } from '@/next/core/net/worded';
import { machineApi, platformPath } from '@/next/core/net/machine';
import { runtimeJson } from '@/next/core/net/runtime-json';
import { t } from '@/next/i18n';
import type { ITextHit } from '@/next/features/pane/find-in-files';
import { parseFileRefusal, parsePage, type ConvId, type FileSave, type MachineId } from '@/next/protocol/v2';

const base = (conv: ConvId) => `/conversations/${encodeURIComponent(conv)}` as const;

export const listFolder = (m: MachineId, conv: ConvId, path: string, signal?: AbortSignal) =>
  wordedPage(m, `${base(conv)}/files?path=${encodeURIComponent(path)}`, parsePage.files, parseFileRefusal, {
    signal,
  });

export const readFile = (m: MachineId, conv: ConvId, path: string, signal?: AbortSignal) =>
  wordedPage(m, `${base(conv)}/file?path=${encodeURIComponent(path)}`, parsePage.file, parseFileRefusal, {
    signal,
  });

/** The whole new text over the revision that was read; a file that changed since is refused as a conflict. */
export const saveFile = (m: MachineId, conv: ConvId, path: string, body: FileSave) =>
  wordedPage(m, `${base(conv)}/file?path=${encodeURIComponent(path)}`, parsePage.file, parseFileRefusal, {
    method: 'PUT',
    body,
  });

/** A file as a git revision has it, for the editor's diff. */
export interface IGitSide {
  ref: string;
  inRepo: boolean;
  tracked: boolean;
  content: string;
  truncated: boolean;
  binary: boolean;
}

export const readGitSide = async (
  m: MachineId,
  conv: ConvId,
  path: string,
  ref = 'HEAD',
  signal?: AbortSignal,
): Promise<IGitSide> => {
  const url = machineApi(m).http(
    `${base(conv)}/file-git?path=${encodeURIComponent(path)}&ref=${encodeURIComponent(ref)}`,
  );
  const res = await fetch(url, { credentials: 'same-origin', signal });
  const doc: unknown = await res.json().catch(() => null);
  const o = (doc ?? {}) as Partial<IGitSide> & { error?: unknown };
  if (!res.ok) throw new Error(typeof o.error === 'string' ? o.error : `HTTP ${res.status}`);
  return {
    ref: typeof o.ref === 'string' ? o.ref : ref,
    inRepo: o.inRepo === true,
    tracked: o.tracked === true,
    content: typeof o.content === 'string' ? o.content : '',
    truncated: o.truncated === true,
    binary: o.binary === true,
  };
};

/** The files under a folder whose paths match a query by letters in order, best first, as the «@» of the chat field
 *  finds them (at most 20); relative to the folder. */
export const findFiles = async (m: MachineId, folder: string, query: string, signal?: AbortSignal) => {
  const path =
    `/context-files?cwd=${encodeURIComponent(folder)}&q=${encodeURIComponent(query.trim())}` as const;
  const body = await runtimeJson<{ files?: unknown }>(m, platformPath(path), { signal });
  return Array.isArray(body.files) ? body.files.filter((f): f is string => typeof f === 'string') : [];
};

/** «Найти в файлах»: the lines of the text files under a folder (the chat's when empty, or an absolute one of the
 *  workspace's places) that hold a text, as paths the files tab opens; `truncated` when the machine stopped early. */
export const findText = async (
  m: MachineId,
  conv: ConvId,
  folder: string,
  query: string,
  signal?: AbortSignal,
) => {
  const url = machineApi(m).http(
    `${base(conv)}/find?q=${encodeURIComponent(query)}&path=${encodeURIComponent(folder)}`,
  );
  const res = await fetch(url, { credentials: 'same-origin', signal });
  const doc: unknown = await res.json().catch(() => null);
  const o = (doc ?? {}) as { hits?: unknown; truncated?: unknown; error?: unknown };
  if (!res.ok || !Array.isArray(o.hits))
    throw new Error(typeof o.error === 'string' ? o.error : t('next.files.findFailed'));
  const hits = o.hits.filter(
    (h): h is ITextHit =>
      typeof h === 'object' &&
      h !== null &&
      typeof (h as ITextHit).path === 'string' &&
      typeof (h as ITextHit).line === 'number' &&
      typeof (h as ITextHit).col === 'number' &&
      typeof (h as ITextHit).text === 'string',
  );
  return { hits, truncated: o.truncated === true };
};

/** What «Файлы» changes in a folder. */
export type TEntryOp = 'newFile' | 'rename' | 'trash';

/** A new empty file `name` in the folder `path`, a new name for the entry `path`, or the entry moved to the trash.
 *  Paths are as the files tab has them; the answer is the path of what was made or renamed. */
export const entryOp = async (m: MachineId, conv: ConvId, op: TEntryOp, path: string, name = '') => {
  const res = await fetch(machineApi(m).http(`${base(conv)}/file-op`), {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ op, path, name }),
  });
  const doc: unknown = await res.json().catch(() => null);
  const o = (doc ?? {}) as { path?: unknown; error?: unknown };
  if (!res.ok || typeof o.path !== 'string')
    throw new Error(typeof o.error === 'string' ? o.error : t('next.files.opFailed'));
  return o.path;
};
