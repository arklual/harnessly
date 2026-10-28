// The accounts the person's agents were signed in with on each machine, as the cluster keeps them: a second
// subscription account on one machine can get it blocked, and the CLI forgets an account the moment it signs out. The
// page tells the cluster the account it reads off a machine; what it shows comes back from the cluster.
import { createStore, useStore } from '@/next/core/store';
import { gatewayPath } from '@/next/core/net/machine';
import { getPage, sendBody } from '@/next/core/net/http';
import { parseCentral, type AgentAccountNote, type MachineId, type Provider } from '@/next/protocol/v2';

const TIMEOUT_MS = 15_000;
const PATH = '/history/agent-accounts';

/** null: not read yet, or a gateway that keeps none. */
export const agentAccountsStore = createStore<readonly AgentAccountNote[] | null>(null);

export async function refreshAgentAccounts(): Promise<void> {
  const res = await getPage(gatewayPath(PATH), parseCentral.agentAccounts, TIMEOUT_MS);
  if ('ok' in res) agentAccountsStore.set(res.value);
}

/** The cluster notes the account an agent holds on a machine now; its answer is the whole list again. */
export async function noteAgentAccount(machine: MachineId, provider: Provider, who: string): Promise<void> {
  const res = await sendBody(
    'POST',
    gatewayPath(PATH),
    { machineId: machine, provider, who },
    parseCentral.agentAccounts,
    TIMEOUT_MS,
  );
  if ('ok' in res) agentAccountsStore.set(res.value);
}

/** The accounts of one agent on one machine, the newest sign-in first; undefined while the cluster has not answered. */
export const useMachineAccounts = (machine: MachineId, provider: Provider): string[] | undefined => {
  const all = useStore(agentAccountsStore);
  if (all === null) return undefined;
  return all
    .filter((note) => note.machineId === machine && note.provider === provider)
    .sort((a, b) => b.lastAt - a.lastAt)
    .map((note) => note.who);
};
