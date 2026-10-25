// The machines list of the Settings screen: what the fleet's records say about each machine, in words.
import { agoText } from '@/next/features/apps/model';
import { LOGIN_COMMANDS } from '@/next/features/composer/agents';
import { t } from '@/next/i18n';
import type { MachineAgent, MachineId, Provider } from '@/next/protocol/v2';
import type { MachineView } from '@/next/stores/machines';

export interface IMachineRow {
  id: MachineId;
  name: string;
  host: string;
  online: boolean;
  /** The agents on it with their versions. */
  agents: string;
  /** Whether it answers, and since when it does not. */
  state: string;
  main: boolean;
  /** It can be made the main one: it answers and is not the main one. */
  canMain: boolean;
}

const agentText = (agent: MachineAgent): string => {
  const name = t(`next.settings.agentName.${agent.provider satisfies Provider}`);
  if (agent.status === 'missing') return t('next.settings.machines.missing', { name });
  if (agent.status === 'login-required')
    return t('next.settings.machines.loginRequired', { name, command: LOGIN_COMMANDS[agent.provider] });
  return agent.version ? `${name} ${agent.version}` : name;
};

/**
 * The main machine is the person's choice (`defaultMachine`); a person with one machine has no other to choose, so it
 * is the main one without a choice.
 */
export const isMain = (id: MachineId, defaultMachine: MachineId | null, count: number): boolean =>
  defaultMachine === null ? count === 1 : defaultMachine === id;

export const machineRows = (
  views: readonly MachineView[],
  defaultMachine: MachineId | null,
  now: number,
): IMachineRow[] =>
  views.map(({ machine }) => {
    const online = machine.reach === 'online';
    const main = isMain(machine.id, defaultMachine, views.length);
    let state = t('next.settings.machines.online');
    if (!online)
      state = machine.lastSeenAt
        ? t('next.settings.machines.offSince', { ago: agoText(machine.lastSeenAt, now) })
        : t('next.settings.machines.off');
    return {
      id: machine.id,
      name: machine.name,
      host: machine.host,
      online,
      agents: machine.agents.map(agentText).join(' · '),
      state,
      main,
      canMain: online && !main,
    };
  });
