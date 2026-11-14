// A file the agent handed over (or any file the pane opens that is not text): which way the pane shows it and where it
// reads it. The runtime's file-preview route serves all of it, under the same rules as the viewer's own files.
import { gatewayPath, platformPath, runtimePath } from '@/next/core/net/machine';
import { appConfig } from '@/platform/config';
import { connectionStore } from '@/next/stores/connection';
import { downloadHref } from '@/next/features/pane/download';
export { DELIVERED_VIEW, deliveredViewOf, type DeliveredView } from '@/next/features/pane/delivered-kind';
import type { ConvId, MachineId } from '@/next/protocol/v2';

const route = (m: MachineId, query: string) => runtimePath(platformPath(`/file-preview?${query}`), m);
const enc = encodeURIComponent;

/** The file for the pane. A machine that is not live has the hub answer instead: a file the agent handed
 *  over in the person's chat is read from the hub's store by its path; anything else is sent on to the machine. */
export const previewUrl = (m: MachineId, path: string) => {
  if (appConfig().centralUploads && connectionStore.get(m)?.k !== 'live')
    return `${gatewayPath('/uploads/delivered/by-path')}?${new URLSearchParams({ machine: m, path }).toString()}`;
  return route(m, `path=${enc(path)}`);
};
/** The file for the pane of a chat: read through the chat, under the rule its files are saved by, so a file
 *  of the chat's folder opens wherever the folder lies (a project of the shared design library is none of the viewer's
 *  places). A machine that is not live is answered as previewUrl says. */
export const chatPreviewUrl = (m: MachineId, conv: ConvId, path: string) => {
  if (appConfig().centralUploads && connectionStore.get(m)?.k !== 'live') return previewUrl(m, path);
  return `${downloadHref(m, conv, [path])}&show=1`;
};
/** What the platform read of a handed-over file: under a hub the machine put its bytes into the hub's store by it. */
export interface IKeptFile {
  sha256?: string;
  mime?: string;
  name?: string;
}

const SHA256 = /^[0-9a-f]{64}$/;

/** A handed-over file from the hub's store, by its sha256: it reads with the machine off, and the hub sends
 *  one it has not got to the machine's preview of the path. null when there is no store to read it from. */
const keptUrl = (
  m: MachineId,
  path: string,
  file: IKeptFile | undefined,
  download: boolean,
): string | null => {
  if (!appConfig().centralUploads || !file?.sha256 || !SHA256.test(file.sha256)) return null;
  const q = new URLSearchParams({ machine: m, path });
  if (file.mime) q.set('type', file.mime);
  if (file.name) q.set('name', file.name);
  if (download) q.set('download', '1');
  return `${gatewayPath(`/uploads/delivered/${file.sha256}`)}?${q.toString()}`;
};

export const downloadUrl = (m: MachineId, path: string, file?: IKeptFile) =>
  keptUrl(m, path, file, true) ?? route(m, `download=1&path=${enc(path)}`);
/** The file for a tab of the browser (⌘/Ctrl-click on its card): shown, not saved. */
export const viewUrl = (m: MachineId, path: string, file?: IKeptFile) =>
  keptUrl(m, path, file, false) ?? previewUrl(m, path);
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
