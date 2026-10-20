import { homeFetch } from '@/lib/app-path';
import { createSharedResource } from '@/lib/shared-resource';
import type { IMachine, IMachineFeatures } from '@/types/server/hl-machines';
import useSharedResource from '@/hooks/use-shared-resource';
import { t } from '@/lib/i18n';

/** `noFrame`: the shell draws this machine's conversations itself, without its frame. */
export interface IFleetMachine extends IMachine {
  available?: boolean;
  noFrame?: boolean;
}

/** Load and limits move slowly; a minute keeps them honest without a request stream. Shared so a reopened sidebar renders at once. */
/** The gateway also names the person's default machine; a runtime answering for itself does not. */
export const machinesSource = createSharedResource<{
  machines: IFleetMachine[];
  defaultMachineId?: string | null;
}>({
  load: async (signal) => {
    // The gateway's list of all machines: never the machine whose conversation is on screen.
    const response = await homeFetch('/api/platform/machines', { signal });
    if (!response.ok) throw new Error(`HTTP ${response.status}`);
    return response.json();
  },
  describeError: (error) =>
    t('hooks.useHlMachines.stale', {
      reason: error instanceof Error ? error.message : t('hooks.useHlMachines.noAnswer'),
    }),
  intervalMs: 60_000,
  maxBackoffMs: 5 * 60_000,
});

export const useHlMachines = () => useSharedResource(machinesSource);

/**
 * Features of one machine; `null` while the list loads, when the machine is missing from it
 * or when its runtime predates the field. Without a fleet (no machine id) the only machine is the local one.
 */
export const useMachineFeatures = (machineId: string | undefined): IMachineFeatures | null => {
  const machines = useHlMachines().data?.machines;
  if (!machines) return null;
  if (!machineId) return machines.length === 1 ? (machines[0].features ?? null) : null;
  return machines.find((machine) => machine.id === machineId)?.features ?? null;
};
