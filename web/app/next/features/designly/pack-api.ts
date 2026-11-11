// The portable pack over HTTP: Sinopia projects with the kits they are pinned to, as one file. The runtime
// does the work (internal/design/pack_routes.go); here is only the asking and the reading of its answers. A refusal
// comes back in the runtime's own words and is shown as it is.
import { platformPath, runtimePath } from '@/next/core/net/machine';
import type { MachineId } from '@/next/protocol/v2';

/** A kit file whose license is restricted (a corporate font): it travels only with the person's word. */
export interface IPackRestricted {
  path: string;
  font?: string;
  holder?: string;
  reason: string;
}

export const PACK_EXPORT = { done: 'done', restricted: 'restricted', refused: 'refused' } as const;

export type TPackExport =
  | { kind: typeof PACK_EXPORT.done; blob: Blob; name: string }
  | { kind: typeof PACK_EXPORT.restricted; files: IPackRestricted[] }
  | { kind: typeof PACK_EXPORT.refused; message: string };

export const PACK_STATUS = {
  added: 'added',
  renamed: 'renamed',
  unchanged: 'unchanged',
  conflict: 'conflict',
} as const;
export type TPackStatus = (typeof PACK_STATUS)[keyof typeof PACK_STATUS];

export interface IPackProject {
  id: string;
  /** The id it has (or would have) in this library. */
  as: string;
  title: string;
  status: TPackStatus;
}

export interface IPackKit {
  id: string;
  version: string;
  status: TPackStatus;
}

export interface IPackPlan {
  projects: IPackProject[];
  kits: IPackKit[];
  warnings: string[];
}

export type TPackImport =
  { ok: true; plan: IPackPlan } | { ok: false; message: string; conflict: IPackKit[] };

const rec = (v: unknown): Record<string, unknown> =>
  typeof v === 'object' && v !== null ? (v as Record<string, unknown>) : {};
const str = (v: unknown): string => (typeof v === 'string' ? v : '');
const list = (v: unknown): Record<string, unknown>[] => (Array.isArray(v) ? v.map(rec) : []);

const exportPath = (ids: readonly string[], acceptRestricted: boolean): `/${string}` => {
  const q = new URLSearchParams();
  ids.forEach((id) => q.append('project', id));
  if (acceptRestricted) q.set('acceptRestricted', '1');
  return `/design/pack/export?${q.toString()}`;
};

const FILE_NAME = /filename="([^"]+)"/;

/** Reads the body in pieces so a big pack shows how far it is; `total` is 0 where the runtime did not say. */
const readBody = async (res: Response, onProgress: (done: number, total: number) => void): Promise<Blob> => {
  const total = Number(res.headers.get('content-length')) || 0;
  if (!res.body) return res.blob();
  const reader = res.body.getReader();
  const parts: BlobPart[] = [];
  let done = 0;
  for (;;) {
    const { value, done: end } = await reader.read();
    if (end) break;
    parts.push(value as BlobPart);
    done += value.byteLength;
    onProgress(done, total);
  }
  return new Blob(parts, { type: 'application/gzip' });
};

/** Packs the projects and their kits. Without `acceptRestricted` a kit with restricted files comes back as a list to
 *  confirm, never as a file. A broken link throws; a refusal of the runtime is `refused` with its words. */
export const exportPack = async (
  m: MachineId,
  ids: readonly string[],
  opt: { acceptRestricted?: boolean; onProgress?: (done: number, total: number) => void } = {},
): Promise<TPackExport> => {
  const res = await fetch(runtimePath(platformPath(exportPath(ids, opt.acceptRestricted === true)), m), {
    credentials: 'same-origin',
  });
  if (res.ok) {
    const blob = await readBody(res, opt.onProgress ?? (() => undefined));
    const name = FILE_NAME.exec(res.headers.get('content-disposition') ?? '')?.[1] ?? 'sinopia-pack.tar.gz';
    return { kind: PACK_EXPORT.done, blob, name };
  }
  const body = rec(await res.json().catch(() => null));
  if (res.status === 409 && Array.isArray(body.restricted)) {
    return {
      kind: PACK_EXPORT.restricted,
      files: list(body.restricted).map((r) => ({
        path: str(r.path),
        font: str(r.font) || undefined,
        holder: str(r.holder) || undefined,
        reason: str(r.reason),
      })),
    };
  }
  return { kind: PACK_EXPORT.refused, message: str(body.error) || String(res.status) };
};

/** Gives a made file to the browser's downloads. */
export const saveBlob = (blob: Blob, name: string): void => {
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = name;
  document.body.append(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 10_000);
};

const planOf = (body: Record<string, unknown>): IPackPlan => {
  const titles = new Map(list(rec(body.manifest).projects).map((p) => [str(p.id), str(p.title)]));
  return {
    projects: list(body.projects).map((p) => ({
      id: str(p.id),
      as: str(p.as) || str(p.id),
      title: titles.get(str(p.id)) || str(p.id),
      status: str(p.status) as TPackStatus,
    })),
    kits: list(body.kits).map((k) => ({
      id: str(k.id),
      version: str(k.version),
      status: str(k.status) as TPackStatus,
    })),
    warnings: Array.isArray(body.warnings)
      ? body.warnings.filter((w): w is string => typeof w === 'string')
      : [],
  };
};

/** Checks an uploaded pack (`dryRun`: nothing is written) or installs it. */
export const importPack = async (
  m: MachineId,
  uploadId: string,
  opt: { dryRun?: boolean } = {},
): Promise<TPackImport> => {
  const q = new URLSearchParams({ upload: uploadId });
  if (opt.dryRun) q.set('dryRun', '1');
  const res = await fetch(
    runtimePath(platformPath(`/design/pack/import?${q.toString()}` as `/${string}`), m),
    {
      method: 'POST',
      credentials: 'same-origin',
    },
  );
  const body = rec(await res.json().catch(() => null));
  if (!res.ok) {
    return {
      ok: false,
      message: str(body.error) || String(res.status),
      conflict: list(body.conflict).map((k) => ({
        id: str(k.id),
        version: str(k.version),
        status: PACK_STATUS.conflict,
      })),
    };
  }
  return { ok: true, plan: planOf(body) };
};
