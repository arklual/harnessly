// The conversation's folder over v2 (GET .../files and .../file, PUT .../file): a folder's entries, folders first, a text
// file's content with its revision and its save over that revision; paths are relative to the conversation's cwd. Refusals come in the runtime's own words.
import { wordedPage } from '@/next/core/net/worded';
import { machineApi } from '@/next/core/net/machine';
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
