/**
 * What the person's preview saw: script errors, rejected promises, long tasks and slow loads, reported by
 * the library's page script while the board is open in the panel. An agent asked "it doesn't work" reads it with
 * `harnessly design logs <id>` instead of guessing; it lives in memory, the newest entries of each design.
 */
export const LIVE_LOG_KIND = {
  error: 'error',
  rejection: 'rejection',
  longtask: 'longtask',
  slow: 'slow',
} as const;
export type TLiveLogKind = (typeof LIVE_LOG_KIND)[keyof typeof LIVE_LOG_KIND];

export interface ILiveLogEntry {
  at: number;
  kind: TLiveLogKind;
  /** The board's path and query inside the project. */
  page: string;
  message: string;
}

const KEEP = 200;
const MAX_TEXT = 600;
const KINDS = new Set<string>(Object.values(LIVE_LOG_KIND));

const g = globalThis as unknown as { __ptDesignLiveLog?: Map<string, ILiveLogEntry[]> };
if (!g.__ptDesignLiveLog) g.__ptDesignLiveLog = new Map();
const logs = g.__ptDesignLiveLog;

const clip = (value: string | null): string =>
  (value ?? '').replace(/[\u0000-\u0008\u000b-\u001f\u007f]+/g, ' ').slice(0, MAX_TEXT);

export const recordLiveLog = (
  artifact: string,
  kind: string | null,
  page: string | null,
  message: string | null,
): boolean => {
  if (!kind || !KINDS.has(kind) || !message) return false;
  const entries = logs.get(artifact) ?? [];
  entries.push({ at: Date.now(), kind: kind as TLiveLogKind, page: clip(page), message: clip(message) });
  if (entries.length > KEEP) entries.splice(0, entries.length - KEEP);
  logs.set(artifact, entries);
  return true;
};

export const readLiveLog = (artifact: string): ILiveLogEntry[] => [...(logs.get(artifact) ?? [])];
