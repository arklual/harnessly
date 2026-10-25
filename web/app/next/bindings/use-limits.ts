// The agent's limits for the composer's chip: asked for when the chat opens and every minute while the page is visible
// (they move slowly; the machine itself reads them about as often). The last good answer is kept through a stumble, so
// a failed ask changes nothing for a machine that has answered once.
import { useEffect } from 'react';
import { createKeyedStore, useKey } from '@/next/core/store';
import { useLinkRestored } from '@/next/stores/connection';
import { loadMachineLimits } from '@/next/features/limits/api';
import type { LimitsView, MachineLimits } from '@/next/features/limits/model';
import type { MachineId, Provider } from '@/next/protocol/v2';

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

/** Null while the first answer has not come: the chip is not drawn before it knows what to say. */
export const useAgentLimits = (machine: MachineId | null, provider: Provider | null): LimitsView | null => {
  const entry = useKey(entries, machine);
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

  if (!entry || !provider || !machine) return null;
  if (entry.limits) return { state: 'ready', provider, limits: entry.limits[provider] };
  if (entry.failed) return { state: 'failed', provider, onRetry: () => ask(machine) };
  return { state: 'ready', provider, limits: null };
};
