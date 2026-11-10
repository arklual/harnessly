// The move of a machine's own data into the hub (docs/migrations/0001-data-to-hub.md, HRN-883), as the machine's
// runtime tells it (<platform>/machine/migration): how far it is, what failed with which code, and the two things a
// person can do: try again, or hand it to an agent on that machine with a brief.
import { getPage, sendBody } from '@/next/core/net/http';
import { platformPath, runtimePath } from '@/next/core/net/machine';
import { plural, t } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';

const TIMEOUT_MS = 15_000;

export interface IMoveFailure {
  kind: string;
  object: string;
  code: string;
  error: string;
  attempts: number;
}

export interface IMachineMove {
  migration: string;
  total: number;
  verified: number;
  pending: number;
  failed: number;
  skipped: number;
  /** Failed objects a person asked to try again that the machine has not tried yet. */
  retrying: number;
  failures: readonly IMoveFailure[];
  doc: string;
  /** Whether the hub keeps the journal too ('on'), or why not. */
  hub: string;
  running: boolean;
}

const num = (v: unknown): number => (typeof v === 'number' && Number.isFinite(v) ? v : 0);
const str = (v: unknown): string => (typeof v === 'string' ? v : '');

const readFailure = (v: unknown): IMoveFailure | null => {
  if (typeof v !== 'object' || v === null) return null;
  const o = v as Record<string, unknown>;
  if (typeof o.kind !== 'string' || typeof o.object !== 'string') return null;
  return {
    kind: o.kind,
    object: o.object,
    code: str(o.code),
    error: str(o.error),
    attempts: num(o.attempts),
  };
};

/** The runtime's answer; null when it moves nothing (no hub, turned off) or answered something else. */
export const readMove = (v: unknown): IMachineMove | null => {
  if (typeof v !== 'object' || v === null) return null;
  const o = v as Record<string, unknown>;
  if (typeof o.off === 'string' || typeof o.migration !== 'string') return null;
  return {
    migration: o.migration,
    total: num(o.total),
    verified: num(o.verified),
    pending: num(o.pending),
    failed: num(o.failed),
    skipped: num(o.skipped),
    retrying: num(o.retrying),
    failures: Array.isArray(o.failures)
      ? o.failures.map(readFailure).filter((f): f is IMoveFailure => f !== null)
      : [],
    doc: str(o.doc),
    hub: str(o.hub),
    running: o.running === true,
  };
};

const movePath = (machine: MachineId, tail = '') =>
  runtimePath(platformPath(`/machine/migration${tail}`), machine);

/** The machine's move; null when it has none or does not answer (an older runtime has none). */
export const loadMachineMove = async (machine: MachineId): Promise<IMachineMove | null> => {
  const page = await getPage(movePath(machine), readMove, TIMEOUT_MS);
  return 'ok' in page ? page.value : null;
};

const readAsked = (v: unknown): IMachineMove | null =>
  typeof v === 'object' && v !== null ? readMove((v as Record<string, unknown>).status) : null;

/** «Повторить»: every failed object is tried again now. The move as it is after; null when it did not go through. */
export const retryMachineMove = async (machine: MachineId): Promise<IMachineMove | null> => {
  const page = await sendBody('POST', movePath(machine, '/retry'), { objects: [] }, readAsked, TIMEOUT_MS);
  return 'ok' in page ? page.value : null;
};

/** Nothing left to move, every object in the hub or left on the machine by a person. */
export const moveDone = (m: IMachineMove): boolean =>
  m.total > 0 && m.verified + m.skipped >= m.total && m.failed === 0 && m.pending === 0;

/** The move stopped: objects failed and nothing else waits (a retry asked is not a stop). */
export const moveStopped = (m: IMachineMove): boolean => m.failed > m.retrying && m.pending === 0;

/** What a person sees of it: «переносим данные с машины X: 412 из 420», or that it stopped; null when there is
 * nothing to say (none, or over). */
export const moveLine = (m: IMachineMove | null, machine: string): string | null => {
  if (m === null || m.total === 0 || moveDone(m)) return null;
  const moved = m.verified + m.skipped;
  if (moveStopped(m)) return plural(m.failed, 'next.move.stopped', { machine, moved, total: m.total });
  return t('next.move.moving', { machine, moved, total: m.total });
};

/** The codes that failed, each once with how many: «MIG-READ ×6, MIG-HASH ×2». */
export const moveCodes = (m: IMachineMove): string => {
  const count = new Map<string, number>();
  for (const f of m.failures) if (f.code) count.set(f.code, (count.get(f.code) ?? 0) + 1);
  return [...count].map(([code, n]) => `${code} ×${n}`).join(', ');
};

/**
 * The brief of «Продолжить с агентом»: an agent on the machine reads the status, the page of the move and fixes what
 * it can with `harnessly migrate`. The status is the runtime's, as `harnessly migrate status --json` prints it.
 */
export const moveBrief = (m: IMachineMove, machine: string): string => {
  const failed = m.failures
    .slice(0, 20)
    .map((f) => `- ${f.kind}/${f.object}: ${f.code} ${f.error}`)
    .join('\n');
  return [
    t('next.move.brief', { machine, n: m.failed, total: m.total }),
    '',
    failed,
    '',
    t('next.move.briefSteps', { doc: m.doc, page: `self-host/${m.migration}` }),
  ].join('\n');
};
