// A file the agent handed over (or any file the pane opens that is not text): which way the pane shows it and where it
// reads it. The runtime's file-preview route serves all of it, under the same rules as the viewer's own files.
import { platformPath, runtimePath } from '@/next/core/net/machine';
export { DELIVERED_VIEW, deliveredViewOf, type DeliveredView } from '@/next/features/pane/delivered-kind';
import type { MachineId } from '@/next/protocol/v2';

const route = (m: MachineId, query: string) => runtimePath(platformPath(`/file-preview?${query}`), m);
const enc = encodeURIComponent;

export const previewUrl = (m: MachineId, path: string) => route(m, `path=${enc(path)}`);
export const downloadUrl = (m: MachineId, path: string) => route(m, `download=1&path=${enc(path)}`);
export const zipEntryUrl = (m: MachineId, path: string, name: string) =>
  route(m, `zip=entry&path=${enc(path)}&name=${enc(name)}`);

export interface IZipEntry {
  name: string;
  size: number;
  directory: boolean;
}
export interface IZipList {
  entries: IZipEntry[];
  truncated: boolean;
}

const isEntry = (v: unknown): v is IZipEntry =>
  typeof v === 'object' &&
  v !== null &&
  typeof (v as IZipEntry).name === 'string' &&
  typeof (v as IZipEntry).size === 'number' &&
  typeof (v as IZipEntry).directory === 'boolean';

/** What is inside a zip. A refusal is thrown with the runtime's own words. */
export const listZip = async (m: MachineId, path: string, signal?: AbortSignal): Promise<IZipList> => {
  const res = await fetch(route(m, `zip=list&path=${enc(path)}`), { credentials: 'same-origin', signal });
  const doc: unknown = await res.json().catch(() => null);
  if (!res.ok) {
    const error = (doc as { error?: unknown } | null)?.error;
    throw new Error(typeof error === 'string' ? error : `HTTP ${res.status}`);
  }
  const entries = (doc as { entries?: unknown } | null)?.entries;
  if (!Array.isArray(entries) || !entries.every(isEntry)) throw new Error('bad answer');
  return { entries, truncated: (doc as { truncated?: unknown }).truncated === true };
};
