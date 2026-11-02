// A file on the machine as the pane's views need it: its revision, which says it changed, the whole file written back over
// the revision it was read at (a file changed since is refused, not overwritten), and its kept earlier states.
import { runtimePath } from '@/next/core/net/machine';
import type { MachineId } from '@/next/protocol/v2';

type TPath = Parameters<typeof runtimePath>[0];

const route = (m: MachineId, query: string) => runtimePath(`/api/v2/file-bytes?${query}` as TPath, m);
const enc = encodeURIComponent;

export class FileConflict extends Error {}

const readDoc = async (res: Response) => {
  const doc = (await res.json().catch(() => null)) as Record<string, unknown> | null;
  if (!res.ok) throw new Error(String(doc?.error ?? `HTTP ${res.status}`));
  return doc;
};

const revisionOf = (doc: Record<string, unknown> | null): string => {
  if (typeof doc?.revision !== 'string') throw new Error('bad answer');
  return doc.revision;
};

export const fileRevision = async (m: MachineId, path: string, signal?: AbortSignal): Promise<string> =>
  revisionOf(
    await readDoc(await fetch(route(m, `path=${enc(path)}`), { credentials: 'same-origin', signal })),
  );

/** Saves over the revision that was read; `revision` null writes whatever is there now. Answers the new revision. */
export const saveFileBytes = async (
  m: MachineId,
  path: string,
  bytes: Uint8Array,
  revision: string | null,
): Promise<string> => {
  const query = `path=${enc(path)}${revision === null ? '' : `&revision=${enc(revision)}`}`;
  const res = await fetch(route(m, query), {
    method: 'PUT',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/octet-stream' },
    body: bytes as BodyInit,
  });
  if (res.status === 409) throw new FileConflict('conflict');
  return revisionOf(await readDoc(res));
};

export interface IFileVersion {
  id: string;
  /** ms */
  at: number;
  size: number;
}

export const fileVersions = async (m: MachineId, path: string): Promise<IFileVersion[]> => {
  const doc = await readDoc(
    await fetch(route(m, `path=${enc(path)}&versions=1`), { credentials: 'same-origin' }),
  );
  return (doc?.versions ?? []) as IFileVersion[];
};

export const restoreFileVersion = async (m: MachineId, path: string, id: string): Promise<string> =>
  revisionOf(
    await readDoc(
      await fetch(route(m, `path=${enc(path)}&restore=${enc(id)}`), {
        method: 'POST',
        credentials: 'same-origin',
      }),
    ),
  );
