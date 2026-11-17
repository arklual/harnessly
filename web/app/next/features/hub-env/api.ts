// What the hub handed this machine, skills and MCP servers, and how the machine agent applied each (HRN-767,
// cmd/runtime/routes_hubenv.go). The runtime sends metadata only: never an item's source.
import { getPage, sendBody, sendPage, type PageOutcome } from '@/next/core/net/http';
import { platformPath, runtimePath } from '@/next/core/net/machine';
import type { MachineId } from '@/next/protocol/v2';

const TIMEOUT_MS = 10_000;

export const HUB_ENV_KINDS = ['skill', 'mcp'] as const;
export type HubEnvKind = (typeof HUB_ENV_KINDS)[number];
/** The highest layer that puts the item in. */
export const HUB_ENV_LAYERS = ['org', 'tenant', 'team', 'user'] as const;
export type HubEnvLayer = (typeof HUB_ENV_LAYERS)[number];
export const HUB_ENV_STATUSES = ['ok', 'error', 'pending', 'off'] as const;
export type HubEnvStatus = (typeof HUB_ENV_STATUSES)[number];

export interface IHubEnvItem {
  itemId: string;
  kind: HubEnvKind;
  name: string;
  layer: HubEnvLayer;
  pinned: boolean;
  status: HubEnvStatus;
  message: string;
  /** The person's own item: theirs to remove. */
  own: boolean;
  /** Switched off from here: not on the machine. */
  off: boolean;
}

export interface IHubEnv {
  /** The runtime shows the section at all (RUNTIME_HUB_ENV_UI). */
  enabled: boolean;
  /** The runtime takes the personal switch (RUNTIME_HUB_ENV_SWITCH). */
  canSwitch: boolean;
  /** The person may add their own skill or MCP server (RUNTIME_HUB_ENV_OWN). */
  canOwn: boolean;
  items: IHubEnvItem[];
}

const obj = (v: unknown, what: string): Record<string, unknown> => {
  if (typeof v !== 'object' || v === null || Array.isArray(v)) throw new Error(`hub-env: ${what}`);
  return v as Record<string, unknown>;
};
const str = (v: unknown, what: string): string => {
  if (typeof v !== 'string') throw new Error(`hub-env: ${what}`);
  return v;
};
const oneOf = <T extends string>(list: readonly T[], v: unknown, what: string): T => {
  const found = list.find((item) => item === v);
  if (found === undefined) throw new Error(`hub-env: ${what}`);
  return found;
};

const readItem = (v: unknown): IHubEnvItem => {
  const o = obj(v, 'item');
  return {
    itemId: str(o.itemId, 'itemId'),
    kind: oneOf(HUB_ENV_KINDS, o.kind, 'kind'),
    name: str(o.name, 'name'),
    layer: oneOf(HUB_ENV_LAYERS, o.layer, 'layer'),
    pinned: o.pinned === true,
    status: oneOf(HUB_ENV_STATUSES, o.status, 'status'),
    message: typeof o.message === 'string' ? o.message : '',
    own: o.own === true,
    off: o.off === true,
  };
};

export const readHubEnv = (v: unknown): IHubEnv => {
  const o = obj(v, 'answer');
  if (!Array.isArray(o.items)) throw new Error('hub-env: items');
  return {
    enabled: o.enabled === true,
    canSwitch: o.canSwitch === true,
    canOwn: o.canOwn === true,
    items: o.items.map(readItem),
  };
};

/** What the runtime says to a change of the own layer: the hub's words when it refused (`error`, `message`). */
export type HubEnvAnswer = { done: true } | { done: false; message: string };

const readAnswer = (v: unknown): HubEnvAnswer => {
  if (typeof v === 'object' && v !== null && !Array.isArray(v) && 'error' in v) {
    const message = (v as { message?: unknown }).message;
    return { done: false, message: typeof message === 'string' ? message : '' };
  }
  return { done: true };
};

const path = (machine: MachineId, rest = '') => runtimePath(platformPath(`/hub-env${rest}`), machine);

export const loadHubEnv = (machine: MachineId): Promise<PageOutcome<IHubEnv>> =>
  getPage(path(machine), readHubEnv, TIMEOUT_MS);

/**
 * The personal switch of an item that is not pinned. The hub keeps it (PUT /api/me/env/{itemId}); the runtime answers
 * 501 until the hub takes a machine's request for it, and then the page shows the failure.
 */
export const switchHubEnv = (machine: MachineId, itemId: string, off: boolean) =>
  sendBody('PUT', path(machine, `/${encodeURIComponent(itemId)}`), { off }, readAnswer, TIMEOUT_MS);

export type OwnSource =
  | { body: string }
  | { transport: 'http'; url: string }
  | { transport: 'stdio'; command: string; args?: string[] };

export interface IOwnItem {
  kind: HubEnvKind;
  name: string;
  hint: string;
  source: OwnSource;
}

/** The person's own skill or MCP server (POST /hub-env/own). */
export const addOwnHubEnv = (machine: MachineId, item: IOwnItem) =>
  sendBody(
    'POST',
    path(machine, '/own'),
    { kind: item.kind, name: item.name, ...(item.hint ? { hint: item.hint } : {}), source: item.source },
    readAnswer,
    TIMEOUT_MS,
  );

/** Takes the person's own item out (DELETE /hub-env/own/{id}). */
export const removeOwnHubEnv = (machine: MachineId, itemId: string) =>
  sendPage('DELETE', path(machine, `/own/${encodeURIComponent(itemId)}`), readAnswer, TIMEOUT_MS);

/** The words of why a change was not taken, or null when it was: the hub's own message, else `fallback`. */
export const refusalOf = (
  res: { ok: true; value: HubEnvAnswer } | { r: string },
  fallback: string,
): string | null => {
  if (!('ok' in res)) return fallback;
  return res.value.done ? null : res.value.message || fallback;
};
