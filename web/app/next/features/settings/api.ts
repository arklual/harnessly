// The Settings screen's requests: the agent CLIs' settings files (GET and PATCH /api/v2/agent-settings/<provider>), the
// skills the CLIs find (the composer's command catalog), and the sign-in of a runtime that asks for a password.
import { getPage, sendBody, sendJson, sendPage } from '@/next/core/net/http';
import { machineApi, platformPath, runtimePath } from '@/next/core/net/machine';
import {
  CAPS_QUERY,
  parsePage,
  type AgentSettingsPatch,
  type MachineId,
  type Provider,
  type RuntimeSettings,
} from '@/next/protocol/v2';

const SETTINGS_TIMEOUT_MS = 10_000;
const CATALOG_TIMEOUT_MS = 15_000;

export const loadAgentSettings = (machine: MachineId, provider: Provider) =>
  getPage(
    machineApi(machine).http(`/agent-settings/${provider}`),
    parsePage.agentSettings,
    SETTINGS_TIMEOUT_MS,
  );

/** Changes what the patch names, if the file is still at `rev`; the answer is the file as it is now. */
export const patchAgentSettings = (
  machine: MachineId,
  provider: Provider,
  rev: string,
  patch: AgentSettingsPatch,
) =>
  sendJson(
    machineApi(machine).http(`/agent-settings/${provider}`),
    { rev, patch },
    parsePage.agentSettings,
    SETTINGS_TIMEOUT_MS,
  );

/** The settings of the runtime itself (GET /api/v2/runtime-settings): when it stops terminals that stand idle. */
export const loadRuntimeSettings = (machine: MachineId) =>
  getPage(
    machineApi(machine).http(`/runtime-settings?${CAPS_QUERY}`),
    parsePage.runtimeSettings,
    SETTINGS_TIMEOUT_MS,
  );

/** Puts the runtime's settings; the answer is them as they are now. */
export const saveRuntimeSettings = (machine: MachineId, next: RuntimeSettings) =>
  sendBody(
    'PUT',
    machineApi(machine).http(`/runtime-settings?${CAPS_QUERY}`),
    next,
    parsePage.runtimeSettings,
    SETTINGS_TIMEOUT_MS,
  );

/** What the pages need of a command of the catalog: to list it as a skill, and to offer it in the field's palette. */
export interface ICatalogItem {
  name: string;
  /** What starts it in the field: «/», or «$» for a skill of Codex. */
  trigger: string;
  aliases: string[];
  description: string;
  argumentHint: string | null;
  /** quick, builtin, project, user, skill, plugin. */
  source: string;
  sourceLabel: string;
  /** How the CLI runs it: send (typed as a message), model, effort, restart, menu, confirm, terminal, blocked… */
  execution: string;
  /** May be the first message of a new chat. */
  welcome: boolean;
  hidden: boolean;
}

const text = (value: unknown, what: string): string => {
  if (typeof value !== 'string') throw new Error(`catalog: ${what}`);
  return value;
};

const readCatalog = (doc: unknown): ICatalogItem[] => {
  const items = (doc as { items?: unknown } | null)?.items;
  if (!Array.isArray(items)) throw new Error('catalog: items');
  return items.map((raw: unknown) => {
    const item = raw as Record<string, unknown>;
    return {
      name: text(item.name, 'name'),
      trigger: typeof item.trigger === 'string' ? item.trigger : '/',
      aliases: Array.isArray(item.aliases)
        ? item.aliases.filter((a): a is string => typeof a === 'string')
        : [],
      description: text(item.description, 'description'),
      argumentHint: typeof item.argumentHint === 'string' ? item.argumentHint : null,
      source: text(item.source, 'source'),
      sourceLabel: text(item.sourceLabel, 'sourceLabel'),
      execution: typeof item.execution === 'string' ? item.execution : 'send',
      welcome: item.welcome === true,
      hidden: item.hidden === true,
    };
  });
};

/** The commands of the agent for a folder (the project's own ones are found from it); without one, the machine's. */
export const loadCatalog = (machine: MachineId, provider: Provider, cwd: string | null = null) =>
  getPage(
    runtimePath(
      platformPath(`/commands?provider=${provider}${cwd ? `&cwd=${encodeURIComponent(cwd)}` : ''}`),
      machine,
    ),
    readCatalog,
    CATALOG_TIMEOUT_MS,
  );

const readSession = (doc: unknown): { password: boolean } => ({
  password: (doc as { hasAuthPassword?: unknown } | null)?.hasAuthPassword === true,
});

/** Whether the runtime signs people in with a password, so there is a session to end. */
export const loadSession = (machine: MachineId) =>
  getPage(runtimePath('/api/config', machine), readSession, SETTINGS_TIMEOUT_MS);

export const signOut = (machine: MachineId) =>
  sendPage('POST', runtimePath('/api/auth/logout', machine), () => null, SETTINGS_TIMEOUT_MS);
