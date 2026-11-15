// «Умные функции» (internal/bgmodel): the background features of a machine and whose model does each. No
// provider is «as now»: the chat's own agent decides.
import { getPage, sendBody } from '@/next/core/net/http';
import { platformPath, runtimePath } from '@/next/core/net/machine';
import { EXT_EFFORTS, type IExtModel, type TExtEffort } from '@/next/features/settings/ext-providers-api';
import { answer, bool, list, num, obj, opt, str } from '@/next/features/settings/platform-answer';
import type { MachineId } from '@/next/protocol/v2';

const TIMEOUT_MS = 10_000;

/** `provider` is a connected provider's id; for a feature with `agents` also an agent's (SMART_AGENTS) or SMART_OFF. */
export interface ISmartChoice {
  provider: string;
  model: string;
  /** The level of reasoning the model is asked for; absent or '': the model's own. */
  effort?: TExtEffort | '';
}

export const SMART_OFF = 'off';
export const SMART_AGENTS = ['claude', 'codex'] as const;
export type TSmartAgent = (typeof SMART_AGENTS)[number];

export interface ISmartFeature {
  id: string;
  title: string;
  description: string;
  /** The feature already goes through the picked provider; one that is not keeps the choice for the day it does. */
  live: boolean;
  /** The feature may also be given a model of an agent of the machine, on its plan. */
  agents: boolean;
  /** The feature may also be turned off (the summaries). */
  offable: boolean;
  /** null: as now. */
  choice: ISmartChoice | null;
  /** Why the last call to the provider failed, in the runtime's words; null when it went well or was not made. */
  failure: string | null;
}

export interface ISmartProvider {
  id: string;
  name: string;
  models: readonly IExtModel[];
}

export interface ISmartView {
  features: readonly ISmartFeature[];
  providers: readonly ISmartProvider[];
}

const isEffort = (v: unknown): v is TExtEffort =>
  typeof v === 'string' && (EXT_EFFORTS as readonly string[]).includes(v);

const readModel = (v: unknown): IExtModel => {
  const o = obj(v, 'model');
  const id = str(o.id, 'model.id');
  return {
    id,
    label: opt(o.label) || id,
    efforts: Array.isArray(o.efforts) ? o.efforts.filter(isEffort) : [],
  };
};

const readFailure = (v: unknown): string | null => {
  if (v === undefined || v === null) return null;
  const o = obj(v, 'last');
  if (bool(o.ok, 'last.ok')) return null;
  num(o.at, 'last.at');
  return opt(o.message) || opt(o.reason);
};

const readFeature = (v: unknown): ISmartFeature => {
  const o = obj(v, 'feature');
  const provider = opt(o.provider);
  return {
    id: str(o.id, 'id'),
    title: str(o.title, 'title'),
    description: str(o.description, 'description'),
    live: bool(o.live, 'live'),
    agents: o.agents === true,
    // A runtime before HRN-1492 let the agents' models only to the summaries, which may be off too.
    offable: o.offable === true || (o.offable === undefined && o.agents === true),
    choice:
      provider === ''
        ? null
        : { provider, model: opt(o.model), ...(isEffort(o.effort) ? { effort: o.effort } : {}) },
    failure: readFailure(o.last),
  };
};

const readProvider = (v: unknown): ISmartProvider => {
  const o = obj(v, 'provider');
  return { id: str(o.id, 'id'), name: str(o.name, 'name'), models: list(o.models, 'models', readModel) };
};

/** GET <platform>/background-tasks as the runtime answers it. */
export const readSmartView = answer((o): ISmartView => ({
  features: list(o.features, 'features', readFeature),
  providers: list(o.providers, 'providers', readProvider),
}));

const url = (machine: MachineId, rest = '') => runtimePath(platformPath(`/background-tasks${rest}`), machine);

export const loadSmart = (machine: MachineId) => getPage(url(machine), readSmartView, TIMEOUT_MS);

/** Gives the feature to a provider's model, or back to the chat's agent (null). The answer only says it was taken. */
export const pickSmart = (machine: MachineId, feature: string, choice: ISmartChoice | null) =>
  sendBody(
    'PUT',
    url(machine, `/${encodeURIComponent(feature)}`),
    choice === null ? { provider: null } : choice,
    answer(() => true),
    TIMEOUT_MS,
  );
