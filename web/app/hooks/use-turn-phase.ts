import { useEffect, useState } from 'react';
import useTabStore from '@/hooks/use-tab-store';
import { describeAction } from '@/lib/hl-action-label';
import {
  deriveTurnPhase,
  holdStatus,
  isLivePhase,
  settleDecision,
  SETTLE_WAIT_MS,
  type ISettleWait,
  type ITurnStatus,
} from '@/lib/turn-phase';
import type { TCliState } from '@/types/timeline';

interface IUseTurnPhaseArgs {
  tabId?: string;
  cliState: TCliState;
  launchingSince: number | null;
  compactingSince: number | null;
  lastEntryAt: number | null;
  launchLabel: string;
}

const currentTime = () => Date.now();
const QUIET_UNIT_MS = 60_000;

const sameStatus = (a: ITurnStatus, b: ITurnStatus) =>
  a.phase === b.phase && a.label === b.label && a.since === b.since && a.quietMs === b.quietMs;

/** The held status and the second tick it was derived at, so callers can time other things on the same tick. */
export const useTurnPhase = ({
  tabId,
  cliState,
  launchingSince,
  compactingSince,
  lastEntryAt,
  launchLabel,
}: IUseTurnPhaseArgs): { status: ITurnStatus; now: number } => {
  const busySince = useTabStore((s) => (tabId ? (s.tabs[tabId]?.busySince ?? null) : null));
  const toolAction = useTabStore((s) => describeAction(tabId ? s.tabs[tabId]?.currentAction : null));
  const [unknownSince, setUnknownSince] = useState<number | null>(null);
  const [now, setNow] = useState(currentTime);

  if (cliState === 'unknown' && unknownSince === null) setUnknownSince(currentTime());
  else if (cliState !== 'unknown' && unknownSince !== null) setUnknownSince(null);

  const derived = deriveTurnPhase({
    cliState,
    busySince,
    launchingSince,
    compactingSince,
    unknownSince,
    action: launchingSince ? launchLabel : toolAction,
    lastEntryAt,
    now,
  });
  // "Quiet N min" shows whole minutes; a per-second quietMs would re-render the live turn every tick for nothing.
  const next =
    derived.quietMs === null
      ? derived
      : { ...derived, quietMs: Math.floor(derived.quietMs / QUIET_UNIT_MS) * QUIET_UNIT_MS };

  const [held, setHeld] = useState({ status: next, at: now });
  const [settle, setSettle] = useState<ISettleWait | null>(null);
  const decision = settleDecision({
    shownLive: isLivePhase(held.status.phase),
    nextPhase: next.phase,
    settle,
    lastEntryAt,
    now: currentTime(),
  });
  if (decision.settle !== settle) setSettle(decision.settle);
  const result = holdStatus(held.status, held.at, decision.hold ? held.status : next, now);
  if (!sameStatus(result.status, held.status) || result.at !== held.at) setHeld(result);

  const live = isLivePhase(result.status.phase) || isLivePhase(next.phase);
  const settleAt = decision.hold ? (decision.settle?.at ?? null) : null;
  useEffect(() => {
    if (settleAt === null) return;
    const id = setTimeout(
      () => setNow(currentTime()),
      Math.max(0, settleAt + SETTLE_WAIT_MS - currentTime()),
    );
    return () => clearTimeout(id);
  }, [settleAt]);
  useEffect(() => {
    if (!live) return;
    const tick = () => setNow(currentTime());
    tick();
    const id = setInterval(tick, 1000);
    return () => clearInterval(id);
  }, [live]);

  // The same status object until something shown changes, so a memoized live turn skips the second ticks.
  return { status: sameStatus(result.status, held.status) ? held.status : result.status, now };
};
