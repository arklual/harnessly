// The conversation's folder over v2 (GET .../files and .../file, PUT .../file): a folder's entries, folders first, a text
// file's content with its revision and its save over that revision; paths are relative to the conversation's cwd. Refusals come in the runtime's own words.
import { wordedPage } from '@/next/core/net/worded';
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
