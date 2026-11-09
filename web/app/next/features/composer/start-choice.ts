// The model a new chat starts on: what the person picked on this page, else the default of the agent's settings file
// (Settings → the CLI's page), else the last pick (kept in the browser), and only else the agent's first model.
import {
  agentKey,
  choiceAgentKey,
  firstChoice,
  modesOf,
  type IModelChoice,
  type IPickerAgent,
} from '@/next/features/composer/agents';
import {
  EFFORTS,
  PERMISSION_MODES,
  type AgentOption,
  type Effort,
  type PermissionMode,
  type Provider,
} from '@/next/protocol/v2';

const LAST_KEY = 'hx-next-new-chat-choice';

const isEffort = (value: unknown): value is Effort =>
  typeof value === 'string' && (EFFORTS as readonly string[]).includes(value);

const isPermission = (value: unknown): value is PermissionMode =>
  typeof value === 'string' && (PERMISSION_MODES as readonly string[]).includes(value);

export const readLastChoice = (): IModelChoice | null => {
  try {
    const raw: unknown = JSON.parse(localStorage.getItem(LAST_KEY) ?? 'null');
    if (typeof raw !== 'object' || raw === null) return null;
    const { provider, profile, model, effort, ultracode, fast, permission } = raw as Record<string, unknown>;
    if (provider !== 'claude' && provider !== 'codex') return null;
    return {
      provider,
      ...(typeof profile === 'string' ? { profile } : {}),
      model: typeof model === 'string' ? model : null,
      effort: isEffort(effort) ? effort : null,
      ultracode: ultracode === true,
      ...(fast === true ? { fast: true } : {}),
      ...(isPermission(permission) ? { permission } : {}),
    };
  } catch {
    return null;
  }
};

export const rememberChoice = (choice: IModelChoice) => {
  try {
    localStorage.setItem(LAST_KEY, JSON.stringify(choice));
  } catch {
    // Private mode or full storage: the pick lives for this page only.
  }
};

/** The fast and permission modes of a choice made in a running chat become the next new chat's, its model staying
 *  as last picked for a new chat. */
export const rememberModes = (choice: Pick<IModelChoice, 'fast' | 'permission'>) => {
  const last = readLastChoice();
  if (!last) return;
  const { fast: _fast, permission: _permission, ...rest } = last;
  rememberChoice({
    ...rest,
    ...(choice.fast ? { fast: true } : {}),
    ...(choice.permission ? { permission: choice.permission } : {}),
  });
};

/** The agent's model a settings value names: the id itself, or an alias or a full id around it («opus[1m]»,
 *  «claude-opus-5-5»). */
const modelOf = (agent: IPickerAgent, value: string) => {
  const name = value.trim().toLowerCase();
  const exact = agent.models.find((m) => m.id.toLowerCase() === name);
  if (exact) return exact;
  const bare = name.replace(/\[.*\]$/, '');
  return (
    agent.models.find((m) => m.id.toLowerCase() === bare) ??
    agent.models.find((m) => bare.split(/[-_.\s]/).includes(m.id.toLowerCase()))
  );
};

/** The choice the settings file makes for this agent; null when it names no model the agent offers. */
export const settingsChoice = (agent: IPickerAgent, options: readonly AgentOption[]): IModelChoice | null => {
  const value = options.find((o) => o.key === 'model')?.value;
  if (typeof value !== 'string' || !value.trim()) return null;
  const model = modelOf(agent, value);
  if (!model) return null;
  const effort = options.find((o) => o.key === 'effort')?.value;
  const ultracode = options.find((o) => o.key === 'ultracode')?.value;
  return {
    provider: agent.provider,
    ...(agent.profile ? { profile: agent.profile } : {}),
    model: model.id,
    effort: isEffort(effort) && model.efforts.includes(effort) ? effort : model.defaultEffort,
    ultracode: ultracode === true && model.ultracode === true,
  };
};

/** A pick made on the page or restored from storage, if its agent and model are still offered. */
const choiceFor = (agent: IPickerAgent, last: IModelChoice | null): IModelChoice | null => {
  if (!last || choiceAgentKey(last) !== agentKey(agent)) return null;
  const model = agent.models.find((m) => m.id === last.model);
  if (!model) return null;
  const effort =
    last.effort === null || (last.effort && model.efforts.includes(last.effort))
      ? last.effort
      : model.defaultEffort;
  return {
    provider: agent.provider,
    ...(agent.profile ? { profile: agent.profile } : {}),
    model: model.id,
    effort,
    ultracode: last.ultracode && model.ultracode === true,
    ...modesOf(last, model),
  };
};

interface IStartInput {
  ready: readonly IPickerAgent[];
  picked: IModelChoice | null;
  /** Each agent's settings options, null while unread or unreadable. */
  settings: Readonly<Record<Provider, readonly AgentOption[] | null>>;
  last: IModelChoice | null;
}

export const startChoice = ({ ready, picked, settings, last }: IStartInput): IModelChoice | null => {
  const pickedAgent = picked && ready.find((a) => agentKey(a) === choiceAgentKey(picked));
  if (pickedAgent) return choiceFor(pickedAgent, picked);
  const agent = (last && ready.find((a) => agentKey(a) === choiceAgentKey(last))) || ready[0];
  if (!agent) return null;
  // The settings file is the CLI's own: a profile group runs on the vendor's models, not on what it names.
  const options = agent.profile ? null : settings[agent.provider];
  return withLastModes(
    (options && settingsChoice(agent, options)) ?? choiceFor(agent, last) ?? firstChoice(agent),
    agent,
    last,
  );
};

/** The fast and permission modes of the last pick carry to the next chat whatever its model came from: they are the
 *  person's way of working, not the model's. Fast stays on only on a model that runs it. */
const withLastModes = (
  choice: IModelChoice,
  agent: IPickerAgent,
  last: IModelChoice | null,
): IModelChoice => {
  if (!last || (!last.fast && !last.permission)) return choice;
  const model = agent.models.find((m) => m.id === choice.model);
  const { fast: _fast, permission: _permission, ...rest } = choice;
  return { ...rest, ...modesOf(last, model) };
};
