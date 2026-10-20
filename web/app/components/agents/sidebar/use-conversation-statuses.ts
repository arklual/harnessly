import { useContext, useMemo } from 'react';
import useTabStore from '@/hooks/use-tab-store';
import useFleetStatus, { RuntimeMachineContext } from '@/hooks/use-fleet-status';
import type { TProviderStatus } from '@/components/agents/provider-icon';
import type { Conversation } from '@/lib/hl-conversations';
import type { ITabState } from '@/hooks/use-tab-store';
import { providerStatusOf } from '@/lib/hl-fleet-statuses';

const localStatus = (tab: ITabState | undefined): TProviderStatus => providerStatusOf(tab);

/** Agent conversations of the shell's own machine (the tab store knows them) and of the other machines. */
export const splitByMachine = (conversations: readonly Conversation[], native: string | undefined) => {
  const local: Conversation[] = [];
  const remote: Conversation[] = [];
  for (const item of conversations) {
    if (item.kind !== 'agent') continue;
    (!item.machineId || item.machineId === native ? local : remote).push(item);
  }
  return { local, remote };
};

/**
 * Status of every agent conversation at once, by key: the local tab store for the shell's own machine, the fleet
 * store for the others. Idle ones are left out.
 * Selectors return a joined string so a store change that keeps every status does not re-render.
 *
 * The workspace shell renders RuntimeMachineContext itself, so it passes its machine in `nativeMachineId`:
 * its own hooks run above that provider.
 */
const useConversationStatuses = (
  conversations: readonly Conversation[],
  nativeMachineId?: string,
): Readonly<Record<string, TProviderStatus>> => {
  const contextMachineId = useContext(RuntimeMachineContext);
  const native = nativeMachineId ?? contextMachineId;
  const split = useMemo(() => splitByMachine(conversations, native), [conversations, native]);
  const local = useTabStore((s) =>
    split.local.map((item) => `${item.key}=${localStatus(s.tabs[item.id])}`).join('\n'),
  );
  const remote = useFleetStatus((s) =>
    split.remote.map((item) => `${item.key}=${s.machines[item.machineId!]?.[item.id] ?? 'idle'}`).join('\n'),
  );
  return useMemo(() => {
    const statuses: Record<string, TProviderStatus> = {};
    for (const line of `${local}\n${remote}`.split('\n')) {
      const at = line.lastIndexOf('=');
      const status = line.slice(at + 1) as TProviderStatus;
      if (at > 0 && status !== 'idle') statuses[line.slice(0, at)] = status;
    }
    return statuses;
  }, [local, remote]);
};

export default useConversationStatuses;
