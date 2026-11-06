// What the hub handed this machine, skills and MCP servers, and how the machine agent applied each (HRN-767,
// cmd/runtime/routes_hubenv.go). The runtime sends metadata only: never an item's source.
import { getPage, sendBody, type PageOutcome } from '@/next/core/net/http';
import { platformPath, runtimePath } from '@/next/core/net/machine';
import type { MachineId } from '@/next/protocol/v2';

const TIMEOUT_MS = 10_000;

export const HUB_ENV_KINDS = ['skill', 'mcp'] as const;
export type HubEnvKind = (typeof HUB_ENV_KINDS)[number];
/** The highest layer that puts the item in. */
export const HUB_ENV_LAYERS = ['org', 'tenant', 'team'] as const;
export type HubEnvLayer = (typeof HUB_ENV_LAYERS)[number];
export const HUB_ENV_STATUSES = ['ok', 'error', 'pending'] as const;
export type HubEnvStatus = (typeof HUB_ENV_STATUSES)[number];

export interface IHubEnvItem {
  itemId: string;
  kind: HubEnvKind;
  name: string;
  layer: HubEnvLayer;
  pinned: boolean;
  status: HubEnvStatus;
  message: string;
}

export interface IHubEnv {
  /** The runtime shows the section at all (RUNTIME_HUB_ENV_UI). */
  enabled: boolean;
  /** The runtime takes the personal switch (RUNTIME_HUB_ENV_SWITCH). */
  canSwitch: boolean;
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
  };
};

export const readHubEnv = (v: unknown): IHubEnv => {
  const o = obj(v, 'answer');
  if (!Array.isArray(o.items)) throw new Error('hub-env: items');
  return { enabled: o.enabled === true, canSwitch: o.canSwitch === true, items: o.items.map(readItem) };
};

const path = (machine: MachineId, rest = '') => runtimePath(platformPath(`/hub-env${rest}`), machine);

export const loadHubEnv = (machine: MachineId): Promise<PageOutcome<IHubEnv>> =>
  getPage(path(machine), readHubEnv, TIMEOUT_MS);

/**
 * The personal switch of an item that is not pinned. The hub keeps it (PUT /api/me/env/{itemId}); the runtime answers
 * 501 until the hub takes a machine's request for it, and then the page shows the failure.
 */
export const switchHubEnv = (machine: MachineId, itemId: string, off: boolean) =>
  sendBody(
    'PUT',
    path(machine, `/${encodeURIComponent(itemId)}`),
    { off },
    (v) => obj(v, 'answer'),
    TIMEOUT_MS,
  );
