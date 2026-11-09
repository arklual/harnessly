// The runtime's snapshots and read-only links of a chat (internal/share): a redacted draft of the transcript is captured
// first, its page can be previewed or downloaded as Markdown or HTML, and a link is published from it for 7, 30 or 90
// days. The routes are the platform API's on the chat's machine; a runtime without them answers 404 (`unavailable`).
import { getPage, sendBody, sendPage } from '@/next/core/net/http';
import { platformPath, runtimePath } from '@/next/core/net/machine';
import type { MachineId } from '@/next/protocol/v2';

const TIMEOUT_MS = 30_000;

export const EXPIRY_DAYS = [7, 30, 90] as const;
export type TExpiryDays = (typeof EXPIRY_DAYS)[number];
export type TFormat = 'md' | 'html';

export interface ISnapshotOptions {
  tools: 'summary' | 'none';
  thinking: boolean;
  diffs: boolean;
  economics: boolean;
}

export const DEFAULT_OPTIONS: ISnapshotOptions = {
  tools: 'summary',
  thinking: false,
  diffs: false,
  economics: false,
};

export interface IDraft {
  id: string;
  title: string;
  /** How many secret-looking values were hidden. */
  hidden: number;
  /** How many paths were shortened. */
  shortened: number;
}

export interface IShare {
  id: string;
  url: string;
  title: string;
  expiresAt: number;
  views: number;
}

export type TShareAnswer<T> = { ok: T } | { error: string };

const path = (machine: MachineId, rest: string) => runtimePath(platformPath(rest as `/${string}`), machine);

const record = (v: unknown, what: string): Record<string, unknown> => {
  if (typeof v !== 'object' || v === null || Array.isArray(v)) throw new Error(what);
  return v as Record<string, unknown>;
};

const sum = (v: unknown): number => {
  if (typeof v === 'number') return v;
  if (typeof v !== 'object' || v === null) return 0;
  return Object.values(v).reduce<number>((n, x) => n + sum(x), 0);
};

export const readDraft = (v: unknown): IDraft => {
  const o = record(v, 'answer');
  const d = record(o.draft, 'draft');
  if (typeof d.id !== 'string') throw new Error('draft');
  const report =
    typeof d.report === 'object' && d.report !== null ? (d.report as Record<string, unknown>) : {};
  return {
    id: d.id,
    title: typeof d.title === 'string' ? d.title : '',
    hidden: sum(report.redactions),
    shortened: typeof report.pathsShortened === 'number' ? report.pathsShortened : 0,
  };
};

export const readShare = (v: unknown): IShare => {
  const s = record(record(v, 'answer').share, 'share');
  if (typeof s.id !== 'string' || typeof s.url !== 'string') throw new Error('share');
  return {
    id: s.id,
    url: s.url,
    title: typeof s.title === 'string' ? s.title : '',
    expiresAt: typeof s.expiresAt === 'number' ? s.expiresAt : 0,
    views: typeof s.views === 'number' ? s.views : 0,
  };
};

const failure = (res: { r: string; code?: string; status?: number | null }): string =>
  (res.r === 'failed' && res.status === 404) || res.code === 'not-found'
    ? 'unavailable'
    : (res.code ?? res.r);

/** Captures the chat's snapshot: its draft, with the secrets hidden. */
export async function captureSnapshot(
  machine: MachineId,
  tabId: string,
  options: ISnapshotOptions,
): Promise<TShareAnswer<IDraft>> {
  const res = await sendBody('POST', path(machine, '/snapshots'), { tabId, options }, readDraft, TIMEOUT_MS);
  return 'ok' in res ? { ok: res.value } : { error: failure(res) };
}

/** Publishes the draft as a link. */
export async function publishShare(
  machine: MachineId,
  draftId: string,
  expiresInDays: TExpiryDays,
): Promise<TShareAnswer<IShare>> {
  const res = await sendBody(
    'POST',
    path(machine, '/shares'),
    { draftId, expiresInDays },
    readShare,
    TIMEOUT_MS,
  );
  return 'ok' in res ? { ok: res.value } : { error: failure(res) };
}

/** The chat's links that are on. */
export async function listShares(machine: MachineId, tabId: string): Promise<IShare[]> {
  const res = await getPage(
    path(machine, `/shares?tabId=${encodeURIComponent(tabId)}`),
    (v) => {
      const list = record(v, 'answer').shares;
      return Array.isArray(list) ? list.map((s) => readShare({ share: s })) : [];
    },
    TIMEOUT_MS,
  );
  return 'ok' in res ? res.value : [];
}

/** Turns a link off: its content goes at once. */
export async function revokeShare(machine: MachineId, id: string): Promise<boolean> {
  const res = await sendPage(
    'DELETE',
    path(machine, `/shares/${encodeURIComponent(id)}`),
    () => true,
    TIMEOUT_MS,
  );
  return 'ok' in res;
}

/** Where a draft downloads as a file; the page of the draft itself is `previewUrl`. */
export const downloadUrl = (machine: MachineId, draftId: string, format: TFormat): string =>
  path(machine, `/snapshots/${encodeURIComponent(draftId)}/download?format=${format}`);
export const previewUrl = (machine: MachineId, draftId: string): string =>
  path(machine, `/snapshots/${encodeURIComponent(draftId)}/preview`);
