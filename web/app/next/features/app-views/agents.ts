// The conversations of the person on this machine, as an app's view with `agents.read` sees them: who they are and
// what each is doing, never what was said (no text, prompt, path or directory). The page already holds the list (the
// sidebar's), so the bridge answers from it and tells a change as it happens.
import { chatStatusOf } from '@/next/features/sidebar/status';
import { byActivity, conversationsStore } from '@/next/stores/conversations';
import { machinesStore } from '@/next/stores/machines';
import type { ConvId, Conversation, MachineId } from '@/next/protocol/v2';

/** At work, waiting for the person (a question, a permission, a plan), or free. */
export const AGENT_STATUSES = ['working', 'waiting', 'free'] as const;
export type TAgentStatus = (typeof AGENT_STATUSES)[number];

export interface IAgentRow {
  id: ConvId;
  title: string;
  status: TAgentStatus;
  provider: string;
  machine: string;
}

/** The sidebar's status of a chat, in three words. */
export const agentStatusOf = (c: Conversation): TAgentStatus => {
  switch (chatStatusOf(c, true)) {
    case 'thinking':
    case 'background':
      return 'working';
    case 'question':
    case 'perm':
    case 'plan':
      return 'waiting';
    default:
      return 'free';
  }
};

/** The rows of a machine's conversations, latest activity first (pinned first, as the sidebar has them). */
export const agentRows = (machine: MachineId): IAgentRow[] => {
  const name = machinesStore.get(machine)?.machine.name ?? machine;
  return [...conversationsStore.values()]
    .filter((c) => c.machineId === machine)
    .sort(byActivity)
    .map((c) => ({
      id: c.id,
      title: c.title,
      status: agentStatusOf(c),
      provider: c.provider,
      machine: name,
    }));
};

/** Calls fn with the rows each time they change (not on a change that leaves them as they were). */
export const watchAgents = (machine: MachineId, fn: (rows: IAgentRow[]) => void): (() => void) => {
  let last = JSON.stringify(agentRows(machine));
  const check = () => {
    const rows = agentRows(machine);
    const now = JSON.stringify(rows);
    if (now === last) return;
    last = now;
    fn(rows);
  };
  const stops = [conversationsStore.subscribeAll(check), machinesStore.subscribeAll(check)];
  return () => stops.forEach((stop) => stop());
};

/** The target `chat:<id>` of hl.pane.open; null for any other. */
export const chatTarget = (target: string): ConvId | null =>
  /^chat:tab-[A-Za-z0-9_-]{1,100}$/.test(target) ? (target.slice(5) as ConvId) : null;

/** Opens a conversation on the page: the event the page already listens to for a click on a push (bindings/notices.ts). */
export const openConversation = (id: ConvId): void => {
  window.dispatchEvent(new CustomEvent('hl:open-conversation', { detail: id }));
};
