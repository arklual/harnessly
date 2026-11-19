// The agent's limits for the composer's chip: asked for when the chat opens and every minute while the page is visible
// (they move slowly; the machine itself reads them about as often). The last good answer is kept through a stumble, so
// a failed ask changes nothing for a machine that has answered once.
import { useEffect } from 'react';
import { createKeyedStore, useKey } from '@/next/core/store';
import { useLinkRestored } from '@/next/stores/connection';
import { useDayStartHour } from '@/next/stores/me';
import { loadMachineLimits } from '@/next/features/limits/api';
import {
  otherLimitsOf,
  limitsOfOwner,
  readLimitsOf,
  type LimitsOwner,
  type LimitsView,
  type MachineLimits,
  type OtherLimits,
} from '@/next/features/limits/model';
import type { MachineId } from '@/next/protocol/v2';

export const LIMITS_POLL_MS = 60_000;

interface IEntry {
  limits: MachineLimits | null;
  /** The last ask failed. */
  failed: boolean;
}

const entries = createKeyedStore<MachineId, IEntry>();

const ask = async (machine: MachineId): Promise<void> => {
  try {
    entries.set(machine, { limits: await loadMachineLimits(machine), failed: false });
  } catch {
    entries.patch(machine, (prev) => ({ limits: prev?.limits ?? null, failed: true }));
  }
};

/** what the hub remembers of the machine's limits (the fleet's list), drawn until the machine answers. A
 *  machine that has answered on this page keeps its own word. */
export const seedLimits = (machine: MachineId, usage: unknown): void => {
  const limits = readLimitsOf(usage);
  if (limits) entries.patch(machine, (prev) => prev ?? { limits, failed: false });
};

/** The card's «other subscriptions», when the machine has any. */
const othersOf = (limits: MachineLimits, owner: LimitsOwner): { others?: OtherLimits[] } => {
  const others = otherLimitsOf(limits, owner);
  return others.length > 0 ? { others } : {};
};

/** Keeps the machine's limits asked while something on the page shows them. */
const useLimitsAsked = (machine: MachineId | null): void => {
  // The link coming back asks at once, not at the next minute.
  const restored = useLinkRestored(machine);
  useEffect(() => {
    if (!machine) return;
    let live = true;
    let timer: number | undefined;
    const schedule = () => {
      timer = window.setTimeout(() => void again(), LIMITS_POLL_MS);
    };
    const again = async () => {
      window.clearTimeout(timer);
      if (!document.hidden) await ask(machine);
      if (live) schedule();
    };
    const resume = () => {
      if (!document.hidden) void again();
    };
    void again();
    document.addEventListener('visibilitychange', resume);
    return () => {
      live = false;
      window.clearTimeout(timer);
      document.removeEventListener('visibilitychange', resume);
    };
  }, [machine, restored]);
};

/** Everything the machine says of its limits (Settings lists them by account); null until it has answered. */
export const useMachineLimits = (machine: MachineId | null): MachineLimits | null => {
  const entry = useKey(entries, machine);
  useLimitsAsked(machine);
  return entry?.limits ?? null;
};

/** Null while the first answer has not come: the chip is not drawn before it knows what to say. */
export const useAgentLimits = (machine: MachineId | null, owner: LimitsOwner | null): LimitsView | null => {
  const entry = useKey(entries, machine);
  const dayStartHour = useDayStartHour();
  useLimitsAsked(machine);

  if (!entry || !owner || !machine || owner.provider === 'antigravity') return null;
  const { provider } = owner;
  if (owner.profile !== null) {
    // A vendor's profile has a chip only once the machine has its limits: one it cannot ask says nothing, not zero.
    const limits = entry.limits ? limitsOfOwner(entry.limits, owner) : null;
    if (!limits || !entry.limits) return null;
    return {
      state: 'ready',
      provider,
      profile: owner.profile,
      limits,
      spend: null,
      ...othersOf(entry.limits, owner),
    };
  }
  // The spend is Claude's: a Codex chat goes by its limits only.
  const claudeSpend = provider === 'claude' ? (entry.limits?.claudeSpend ?? null) : null;
  const spend = claudeSpend ? { spend: claudeSpend, dayStartHour } : null;
  if (entry.limits) {
    const limits = limitsOfOwner(entry.limits, owner);
    return { state: 'ready', provider, limits, spend, ...othersOf(entry.limits, owner) };
  }
  if (entry.failed) return { state: 'failed', provider, onRetry: () => ask(machine) };
  return { state: 'ready', provider, limits: null, spend: null };
};
