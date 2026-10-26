// What each agent can be asked to run, as the machine reports it (machine.agents[].models): the picker over the field
// shows these models and, per model, the depths of thinking it takes. A model's id is what the CLI takes in --model.
import {
  PROVIDERS,
  type AgentInstallStatus,
  type AgentModel,
  type Effort,
  type MachineAgent,
  type Provider,
} from '@/next/protocol/v2';
import type { Vendor } from '@/next/features/providers/model';
import { t } from '@/next/i18n';

export interface IPickerAgent {
  provider: Provider;
  /** The profile this group runs: null for Claude and Codex themselves. */
  profile?: string | null;
  /** The vendor of a profile group, for its mark; absent for Claude and Codex. */
  vendor?: Vendor | null;
  name: string;
  short: string;
  models: readonly AgentModel[];
  /** A running chat of this agent takes a new model. */
  canSwitch: boolean;
  /** Ready to run with known models; else its segment says why (not installed, not signed in). */
  available: boolean;
  status: AgentInstallStatus;
}

export interface IModelChoice {
  provider: Provider;
  /** The profile of the group the model is from; null or absent on Claude and Codex themselves. */
  profile?: string | null;
  model: string | null;
  /** Null: the model's own default depth, or one the agent has not said. */
  effort: Effort | null;
  /** Claude Code's ultracode: a mode beside the depth (dynamic workflows with subagents on every task), not a level. */
  ultracode: boolean;
}

export const effortLabel = (effort: Effort): string => t(`next.picker.effort.${effort}`);
export const effortHint = (effort: Effort): string => t(`next.picker.effortHint.${effort}`);

const NAMES: Record<Provider, { name: string; short: string }> = {
  claude: { name: 'Claude Code', short: 'Claude' },
  codex: { name: 'Codex', short: 'Codex' },
};

/** The profile a choice carries: nothing at all on Claude and Codex themselves. */
const profileOf = (agent: IPickerAgent): { profile?: string } =>
  agent.profile ? { profile: agent.profile } : {};

/** The key of a group in the picker: its profile, else the agent. */
export const agentKey = (a: { provider: Provider; profile?: string | null }): string =>
  a.profile ?? a.provider;
export const choiceAgentKey = (c: Pick<IModelChoice, 'provider' | 'profile'>): string => agentKey(c);

const pickerAgent = (agent: MachineAgent): IPickerAgent => ({
  provider: agent.provider,
  ...NAMES[agent.provider],
  models: agent.models,
  canSwitch: agent.canSwitchModel,
  available: agent.status === 'ready' && agent.models.length > 0,
  status: agent.status,
});

/** Every agent the machine reports, in the protocol's order: the picker's segments; one not ready says why. */
export const pickerAgents = (agents: readonly MachineAgent[]): IPickerAgent[] => {
  const own = PROVIDERS.flatMap((provider) => agents.filter((a) => a.provider === provider));
  return [...own.map(pickerAgent), ...own.flatMap((a) => profileAgents(a))];
};

/** The groups of the agent's profiles that have a key and models, after Claude and Codex. */
const profileAgents = (agent: MachineAgent): IPickerAgent[] =>
  (agent.profiles ?? [])
    .filter((p) => p.configured && p.models.length > 0)
    .map((p) => ({
      provider: agent.provider,
      profile: p.id,
      vendor: p.vendor,
      name: p.label,
      short: p.label,
      models: p.models,
      // A running chat takes a model of its own profile only; the runtime says so by the agent's flag.
      canSwitch: agent.canSwitchModel,
      available: agent.status === 'ready',
      status: agent.status,
    }));

/** The label of the profile a chat runs on when its key was removed (the profile is listed, but without a key); else null. */
export const removedKeyOf = (
  agents: readonly MachineAgent[],
  provider: Provider,
  profile: string | null | undefined,
): string | null => {
  if (!profile) return null;
  const found = agents.find((a) => a.provider === provider)?.profiles?.find((p) => p.id === profile);
  return found && !found.configured ? found.label : null;
};

/** What signs an agent in on the machine, typed in a terminal there. */
export const LOGIN_COMMANDS = { claude: 'claude auth login', codex: 'codex login' } as const satisfies Record<
  Provider,
  string
>;

/** Why an agent cannot start a chat, in its segment. */
export const unavailableNote = (agent: IPickerAgent): string =>
  agent.status === 'login-required'
    ? t('next.picker.loginRequired', { agent: agent.name, command: LOGIN_COMMANDS[agent.provider] })
    : t('next.picker.notInstalled', { agent: agent.name });

/** One agent of the machine, for a chat that already runs it; null when the machine does not report it. */
export const pickerAgentOf = (
  agents: readonly MachineAgent[],
  provider: Provider,
  profile: string | null = null,
): IPickerAgent | null => {
  if (profile !== null) return pickerAgents(agents).find((a) => a.profile === profile) ?? null;
  const agent = agents.find((a) => a.provider === provider);
  return agent ? pickerAgent(agent) : null;
};

// A full id as Claude's transcript names it: claude-opus-5-5, claude-sonnet-5-5-20261001, the [1m] suffix.
const FULL_ID = /^claude-([a-z]+)-(\d+)(?:-(\d{1,2}))?(?:-\d{8})?(\[1m\])?$/;

/** The listed model the id is: by its id, or a full id by its family's alias (claude-opus-5-5 is «opus»). */
export const modelOf = (agent: IPickerAgent, model: string | null): AgentModel | null => {
  const exact = agent.models.find((m) => m.id === model);
  if (exact || !model) return exact ?? null;
  const family = FULL_ID.exec(model)?.[1];
  return agent.models.find((m) => m.id === family) ?? null;
};

/** A full id as people say it: claude-opus-5-5 is «Opus 5.5»; "" when it is not one. */
const spokenId = (model: string): string => {
  const parts = FULL_ID.exec(model);
  if (!parts) return '';
  const [, family, major, minor] = parts;
  return `${family[0].toUpperCase()}${family.slice(1)} ${minor ? `${major}.${minor}` : major}`;
};

/** A model of a subagent as a row names it: a full id as people say it («Sonnet 5.5»), an alias with a capital. */
export const shortModelName = (model: string): string =>
  spokenId(model) || `${model[0].toUpperCase()}${model.slice(1)}`;

/** The model as the pill names it: its label, a full id as people say it, or the id as the agent reported it. */
export const modelName = (agent: IPickerAgent, model: string | null): string => {
  const exact = agent.models.find((m) => m.id === model);
  if (exact) return exact.label;
  if (!model) return agent.name;
  return spokenId(model) || model;
};

/** A new chat starts on the agent's first model at its default depth. */
export const firstChoice = (agent: IPickerAgent): IModelChoice => ({
  provider: agent.provider,
  ...profileOf(agent),
  model: agent.models[0].id,
  effort: agent.models[0].defaultEffort,
  ultracode: false,
});

/** A model change keeps the depth when the new model takes it, else takes the new model's default; ultracode stays
 *  on only on a model that runs it. */
export const withModel = (choice: IModelChoice, agent: IPickerAgent, model: AgentModel): IModelChoice => ({
  provider: agent.provider,
  ...profileOf(agent),
  model: model.id,
  effort: choice.effort && model.efforts.includes(choice.effort) ? choice.effort : model.defaultEffort,
  ultracode: choice.ultracode && model.ultracode === true,
});
