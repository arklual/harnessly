import { t } from '@/lib/i18n';
import type { TCliState } from '@/types/timeline';

export type TTurnPhase =
  'launching' | 'checking' | 'thinking' | 'acting' | 'awaiting-input' | 'compacting' | 'stale' | 'settled';

export const UNKNOWN_GRACE_MS = 20_000;
export const STALE_AFTER_MS = 600_000;
export const COMPACTING_MAX_MS = 60_000;
export const PHASE_HOLD_MS = 600;
/** How long a finished turn keeps its live line while the reply is still on its way into the transcript. */
export const SETTLE_WAIT_MS = 1500;

export interface ITurnPhaseInput {
  cliState: TCliState;
  busySince: number | null;
  launchingSince: number | null;
  compactingSince: number | null;
  unknownSince: number | null;
  action: string | null;
  lastEntryAt: number | null;
  now: number;
}

export interface ITurnStatus {
  phase: TTurnPhase;
  label: string | null;
  since: number | null;
  quietMs: number | null;
}

const status = (
  phase: TTurnPhase,
  label: string | null = null,
  since: number | null = null,
  quietMs: number | null = null,
): ITurnStatus => ({
  phase,
  label,
  since,
  quietMs,
});

export const deriveTurnPhase = (input: ITurnPhaseInput): ITurnStatus => {
  const { cliState, busySince, launchingSince, compactingSince, unknownSince, action, lastEntryAt, now } =
    input;
  if (cliState === 'needs-input') return status('awaiting-input');
  if (compactingSince != null && now - compactingSince < COMPACTING_MAX_MS)
    return status('compacting', t('lib.turnPhase.compacting'));
  if (launchingSince) return status('launching', action);
  if (cliState === 'unknown') {
    if (unknownSince == null || now - unknownSince < UNKNOWN_GRACE_MS)
      return status('checking', t('lib.turnPhase.checking'));
    return status('settled');
  }
  if (cliState === 'busy') {
    if (lastEntryAt != null && now - lastEntryAt > STALE_AFTER_MS)
      return status('stale', action, busySince, now - lastEntryAt);
    return action ? status('acting', action, busySince) : status('thinking', null, busySince);
  }
  return status('settled');
};

const LIVE_PHASES: ReadonlySet<TTurnPhase> = new Set([
  'thinking',
  'acting',
  'stale',
  'launching',
  'checking',
  'compacting',
]);

export const isLivePhase = (phase: TTurnPhase): boolean => LIVE_PHASES.has(phase);

export const holdStatus = (
  shown: ITurnStatus,
  shownAt: number,
  next: ITurnStatus,
  now: number,
): { status: ITurnStatus; at: number } => {
  if (next.phase === 'awaiting-input' || next.phase === 'settled') return { status: next, at: now };
  if (next.phase === shown.phase && next.label === shown.label) return { status: next, at: shownAt };
  if (now - shownAt < PHASE_HOLD_MS) return { status: shown, at: shownAt };
  return { status: next, at: now };
};

export interface ISettleWait {
  at: number;
  entryAt: number | null;
  released: boolean;
}

/**
 * The Stop hook lands a few hundred milliseconds before Claude Code writes the reply into the transcript,
 * so a turn dropped "Thinking…" and showed «worked N s» with no reply for a moment. A turn that was live keeps its live
 * line until a new entry arrives or the wait runs out; a turn that was not live settles at once.
 */
export const settleDecision = ({
  shownLive,
  nextPhase,
  settle,
  lastEntryAt,
  now,
}: {
  shownLive: boolean;
  nextPhase: TTurnPhase;
  settle: ISettleWait | null;
  lastEntryAt: number | null;
  now: number;
}): { hold: boolean; settle: ISettleWait | null } => {
  if (nextPhase !== 'settled') return { hold: false, settle: null };
  if (settle?.released) return { hold: false, settle };
  if (!settle) return { hold: shownLive, settle: { at: now, entryAt: lastEntryAt, released: !shownLive } };
  if (lastEntryAt !== settle.entryAt || now - settle.at >= SETTLE_WAIT_MS)
    return { hold: false, settle: { ...settle, released: true } };
  return { hold: true, settle };
};
