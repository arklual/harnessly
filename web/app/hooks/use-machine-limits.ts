import { useHlMachines } from '@/hooks/use-hl-machines';
import { isForecastWarning } from '@/lib/hl-limits-forecast';
import type { ILimitWindow, IMachine, IProviderLimits } from '@/types/server/hl-machines';

export type TLimitsProvider = keyof IMachine['limits'];

export interface ILimitWarning {
  provider: TLimitsProvider;
  kind: 'five_hour' | 'seven_day';
  window: ILimitWindow;
  /** Epoch seconds: forecast exhaustion, or the reset of an exhausted window. */
  at: number;
}

/** The soonest sure warning among a provider's windows, or null. `now` in epoch seconds. */
export const limitWarning = (
  limits: IProviderLimits | null | undefined,
  provider: TLimitsProvider,
  now = Date.now() / 1000,
): ILimitWarning | null => {
  if (!limits) return null;
  const candidates: ILimitWarning[] = [];
  const windows = [
    ['five_hour', limits.fiveHour],
    ['seven_day', limits.sevenDay],
  ] as const;
  for (const [kind, window] of windows) {
    if (!window?.forecast || !isForecastWarning(window.forecast, kind, now)) continue;
    const at = window.forecast.exhaustAt ?? window.resetsAt;
    candidates.push({ provider, kind, window, at });
  }
  candidates.sort((a, b) => a.at - b.at);
  return candidates[0] ?? null;
};

/**
 * Limits of one machine from the shared machines list (one request stream for the whole page); `null` while the
 * list loads or when the machine is not in it. Without a machine id the only machine is the local one.
 */
export const useMachineLimits = (machineId: string | undefined): IMachine['limits'] | null => {
  const machines = useHlMachines().data?.machines;
  if (!machines) return null;
  if (!machineId) return machines.length === 1 ? machines[0].limits : null;
  return machines.find((machine) => machine.id === machineId)?.limits ?? null;
};
