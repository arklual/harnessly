// The registry of the model providers a person connects by API (internal/extproviders) as the page reads and
// changes it. A key goes one way only: out in the body of a POST or PATCH; the answers say only whether one is kept.
import { getPage, sendBody, sendJson, sendPage } from '@/next/core/net/http';
import { platformPath, runtimePath } from '@/next/core/net/machine';
import { answer, bool, list, num, obj, opt, str } from '@/next/features/settings/platform-answer';
import type { MachineId } from '@/next/protocol/v2';

const TIMEOUT_MS = 10_000;
/** The machine asks the provider over the internet: a check is given longer than a write. */
const CHECK_TIMEOUT_MS = 40_000;

export const EXT_API = { openai: 'openai', anthropic: 'anthropic' } as const;
export type TExtApi = (typeof EXT_API)[keyof typeof EXT_API];

/** The levels of reasoning a model may take (extproviders.ModelEfforts), in their order. */
export const EXT_EFFORTS = ['low', 'medium', 'high', 'xhigh', 'max', 'ultra'] as const;
export type TExtEffort = (typeof EXT_EFFORTS)[number];

export interface IExtModel {
  id: string;
  label: string;
  /** The levels of reasoning it takes; none: the model has no choice. */
  efforts?: readonly TExtEffort[];
  /** The one a chat starts on; '' is the model's own. */
  defaultEffort?: TExtEffort | '';
  /** The context window in tokens; 0 or absent: unknown. */
  window?: number;
  /** The person added it; a refresh keeps it. */
  manual?: boolean;
  /** The person named it or gave it its levels; a refresh keeps what they gave. */
  edited?: boolean;
}

const isEffort = (v: unknown): v is TExtEffort =>
  typeof v === 'string' && (EXT_EFFORTS as readonly string[]).includes(v);

/** A variable a provider adds to the launch of an agent on it. A secret one comes without its value. */
export interface IExtEnvVar {
  name: string;
  value: string;
  secret: boolean;
  /** The machine has a value for it (always so for a plain one). */
  set: boolean;
}

/** A variable as it is sent: a secret one without a value keeps the value the safe has. */
export interface IExtEnvInput {
  name: string;
  secret: boolean;
  value?: string;
}

export interface IExtProvider {
  id: string;
  name: string;
  baseUrl: string;
  host: string;
  api: TExtApi;
  /** The preset it was made from; '' for an address of the person's own. */
  preset: string;
  hasKey: boolean;
  models: readonly IExtModel[];
  /** What the accounts of one provider share; '' from a runtime that keeps one account of a provider. */
  group: string;
  /** The account of its provider that is used where none is named. */
  isDefault: boolean;
  /** It comes from the team's environment: shown, checked and chosen, but changed only in the hub. */
  managed: boolean;
  /** The variables of a launch of Claude Code on it. */
  env: readonly IExtEnvVar[];
}

export interface IExtPreset {
  id: string;
  name: string;
  baseUrl: string;
  api: TExtApi;
  /** Where the vendor gives out keys; '' when the preset names no page. */
  keyUrl: string;
}

/** What a check of the connection found: the models on success, else the reason in the runtime's words. */
export interface IExtCheck {
  ok: boolean;
  reason: string;
  message: string;
  models: readonly IExtModel[];
}

export interface IExtRegistry {
  providers: readonly IExtProvider[];
  presets: readonly IExtPreset[];
  /** The runtime keeps several accounts of one provider (an older one keeps one). */
  accounts: boolean;
  /** The runtime starts Claude Code chats on an Anthropic-compatible provider, with the variables typed for it. */
  launch: boolean;
}

const readApi = (v: unknown): TExtApi => {
  if (v !== EXT_API.openai && v !== EXT_API.anthropic) throw new Error('answer: api');
  return v;
};

const readModel = (v: unknown): IExtModel => {
  const o = obj(v, 'model');
  const id = str(o.id, 'model.id');
  const efforts = Array.isArray(o.efforts) ? o.efforts.filter(isEffort) : [];
  const defaultEffort = isEffort(o.defaultEffort) ? o.defaultEffort : '';
  const window = typeof o.window === 'number' && o.window > 0 ? o.window : 0;
  return {
    id,
    label: opt(o.label) || id,
    efforts,
    defaultEffort,
    window,
    manual: o.manual === true,
    edited: o.edited === true,
  };
};

const readEnvVar = (v: unknown): IExtEnvVar => {
  const o = obj(v, 'env');
  return {
    name: str(o.name, 'env.name'),
    value: opt(o.value),
    secret: bool(o.secret, 'env.secret'),
    set: bool(o.set, 'env.set'),
  };
};

const readProvider = (v: unknown): IExtProvider => {
  const o = obj(v, 'provider');
  return {
    id: str(o.id, 'id'),
    name: str(o.name, 'name'),
    baseUrl: str(o.baseUrl, 'baseUrl'),
    host: str(o.host, 'host'),
    api: readApi(o.api),
    preset: opt(o.preset),
    hasKey: bool(o.hasKey, 'hasKey'),
    models: list(o.models, 'models', readModel),
    group: opt(o.group),
    isDefault: o.default === true,
    managed: o.managed === true,
    // An older runtime keeps no variables and sends none.
    env: o.env === undefined || o.env === null ? [] : list(o.env, 'env', readEnvVar),
  };
};

const readPreset = (v: unknown): IExtPreset => {
  const o = obj(v, 'preset');
  return {
    id: str(o.id, 'id'),
    name: str(o.name, 'name'),
    baseUrl: str(o.baseUrl, 'baseUrl'),
    api: readApi(o.api),
    keyUrl: opt(o.keyUrl),
  };
};

const readCheck = (o: Record<string, unknown>): IExtCheck => ({
  ok: bool(o.ok, 'ok'),
  reason: opt(o.reason),
  message: opt(o.message),
  models: list(o.models, 'models', readModel),
});

const readOne = answer((o) => readProvider(o.provider));

const url = (machine: MachineId, rest = '') => runtimePath(platformPath(`/ext-providers${rest}`), machine);
const of = (id: string) => `/${encodeURIComponent(id)}`;

/** GET <platform>/ext-providers as the runtime answers it. */
export const readExtRegistry = answer((o): IExtRegistry => ({
  providers: list(o.providers, 'providers', readProvider),
  presets: list(o.presets, 'presets', readPreset),
  accounts: o.accounts === true,
  launch: o.launch === true,
}));

export const loadExtProviders = (machine: MachineId) => getPage(url(machine), readExtRegistry, TIMEOUT_MS);

export interface IExtDraft {
  name: string;
  baseUrl: string;
  api: TExtApi;
  preset: string;
  key: string;
  models: readonly IExtModel[];
}

export const createExtProvider = (machine: MachineId, draft: IExtDraft) =>
  sendBody('POST', url(machine), draft, readOne, TIMEOUT_MS);

/** Changes what the patch names. A key that is '' is not sent: the kept one stays. */
export const patchExtProvider = (
  machine: MachineId,
  id: string,
  patch: Partial<Pick<IExtDraft, 'name' | 'baseUrl' | 'api' | 'key'>> & {
    default?: true;
    env?: readonly IExtEnvInput[];
    /** The whole list of models, with what the person did to them. */
    models?: readonly IExtModel[];
  },
) => sendJson(url(machine, of(id)), patch, readOne, TIMEOUT_MS);

export const removeExtProvider = (machine: MachineId, id: string) =>
  sendPage(
    'DELETE',
    url(machine, of(id)),
    answer(() => true),
    TIMEOUT_MS,
  );

/** Checks an address and a key before the provider is made. */
export const checkExtDraft = (machine: MachineId, draft: Pick<IExtDraft, 'baseUrl' | 'api' | 'key'>) =>
  sendBody('POST', url(machine, '/check'), draft, answer(readCheck), CHECK_TIMEOUT_MS);

/** Checks a kept provider with its kept key; the models it lists are kept by the machine. */
export const checkExtProvider = (machine: MachineId, id: string) =>
  sendPage('POST', url(machine, `${of(id)}/check`), answer(readCheck), CHECK_TIMEOUT_MS);

/** Asks the provider for its models again. `check` is how that went; without it the kept list came back. */
export const refreshExtModels = (machine: MachineId, id: string) =>
  getPage(
    url(machine, `${of(id)}/models?refresh=1`),
    answer((o) => ({
      models: list(o.models, 'models', readModel),
      check: o.check === undefined || o.check === null ? null : readCheck(obj(o.check, 'check')),
      at: o.at === undefined ? 0 : num(o.at, 'at'),
    })),
    CHECK_TIMEOUT_MS,
  );
