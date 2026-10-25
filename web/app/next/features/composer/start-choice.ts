// The model a new chat starts on: what the person picked on this page, else the default of the agent's settings file
// (Settings → the CLI's page), else the last pick (kept in the browser), and only else the agent's first model.
import { firstChoice, type IModelChoice, type IPickerAgent } from '@/next/features/composer/agents';
import { EFFORTS, type AgentOption, type Effort, type Provider } from '@/next/protocol/v2';

const LAST_KEY = 'hx-next-new-chat-choice';

const isEffort = (value: unknown): value is Effort =>
  typeof value === 'string' && (EFFORTS as readonly string[]).includes(value);

export const readLastChoice = (): IModelChoice | null => {
  try {
    const raw: unknown = JSON.parse(localStorage.getItem(LAST_KEY) ?? 'null');
    if (typeof raw !== 'object' || raw === null) return null;
    const { provider, model, effort, ultracode } = raw as Record<string, unknown>;
    if (provider !== 'claude' && provider !== 'codex') return null;
    return {
      provider,
      model: typeof model === 'string' ? model : null,
      effort: isEffort(effort) ? effort : null,
      ultracode: ultracode === true,
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
    model: model.id,
    effort: isEffort(effort) && model.efforts.includes(effort) ? effort : model.defaultEffort,
    ultracode: ultracode === true && model.ultracode === true,
  };
};

/** The last pick, if its agent and model are still offered. */
const lastFor = (agent: IPickerAgent, last: IModelChoice | null): IModelChoice | null => {
  if (!last || last.provider !== agent.provider) return null;
  const model = agent.models.find((m) => m.id === last.model);
  if (!model) return null;
  const effort = last.effort && model.efforts.includes(last.effort) ? last.effort : model.defaultEffort;
  return {
    provider: agent.provider,
    model: model.id,
    effort,
    ultracode: last.ultracode && model.ultracode === true,
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
  if (picked && ready.some((a) => a.provider === picked.provider)) return picked;
  const agent = ready.find((a) => a.provider === last?.provider) ?? ready[0];
  if (!agent) return null;
  const options = settings[agent.provider];
  return (options && settingsChoice(agent, options)) ?? lastFor(agent, last) ?? firstChoice(agent);
};
