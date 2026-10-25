// The DevTools record of a preview slot (/api/platform/preview/devtools): what the slot's page did on the network and in
// its console, and the conditions it runs under. The runtime keeps one record per slot origin;
// the pane, the CLI and the agents' MCP read the same one. Only the fields the site's panel draws are typed here.
import { postJson, runtimeJson } from '@/next/core/net/runtime-json';
import { platformPath } from '@/next/core/net/machine';
import type { MachineId } from '@/next/protocol/v2';

export const THROTTLE = {
  none: 'none',
  fast4g: 'fast-4g',
  slow4g: 'slow-4g',
  g3: '3g',
  offline: 'offline',
} as const;
export type TThrottlePreset = (typeof THROTTLE)[keyof typeof THROTTLE] | 'custom';

export interface IDevtoolsSettings {
  throttle: { preset: TThrottlePreset; latencyMs: number; downKbps: number; upKbps: number };
  blocked: string[];
  /** The proxy answers with `no-store` and skips its asset cache: every load is a cold one. */
  disableCache: boolean;
}

export interface INetworkEntry {
  /** Grows by one per slot; the cursor of `since`. */
  id: number;
  at: number;
  method: string;
  /** The real address: the target's for proxied requests, never the slot's. */
  url: string;
  host: string;
  type: string;
  /** null while pending, and for a request that never got an answer. */
  status: number | null;
  size: number | null;
  durationMs: number | null;
  thirdParty: boolean;
  blocked: boolean;
  /** Network failure in words; null when an answer came. */
  error: string | null;
}

export type TConsoleLevel = 'error' | 'warn' | 'info' | 'log';

export interface IConsoleEntry {
  id: number;
  at: number;
  level: TConsoleLevel;
  /** `console`: console.*; `exception`: window.onerror; `rejection`: an unhandled promise; `network`: a failed request. */
  source: string;
  text: string;
  /** Identical entries in a row fold into one, as in Chrome. */
  count: number;
}

export interface IDevtoolsSnapshot {
  settings: IDevtoolsSettings;
  network: INetworkEntry[];
  console: IConsoleEntry[];
  /** Pass back as `since` to get only what came after. */
  cursor: number;
}

const slotQuery = (slot: string, more: Record<string, string> = {}) =>
  new URLSearchParams({ slot, ...more }).toString();

export const fetchDevtools = (m: MachineId, slot: string, since: number, signal?: AbortSignal) =>
  runtimeJson<IDevtoolsSnapshot>(
    m,
    platformPath(`/preview/devtools?${slotQuery(slot, { since: String(since) })}`),
    { signal, cache: 'no-store' },
  );

/** The settings the runtime answers with are the ones the page runs under now. */
export const patchDevtoolsSettings = async (
  m: MachineId,
  slot: string,
  patch: { throttle?: { preset: TThrottlePreset }; disableCache?: boolean },
) => {
  const answer = await runtimeJson<{ settings: IDevtoolsSettings }>(
    m,
    platformPath(`/preview/devtools?${slotQuery(slot)}`),
    {
      ...postJson(patch),
      method: 'PATCH',
    },
  );
  return answer.settings;
};
