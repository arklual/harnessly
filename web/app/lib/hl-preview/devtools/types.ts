/**
 * DevTools of the right panel's browser: what a preview slot's page did on the network and in its console,
 * and the conditions it runs under (throttling, blocked addresses). One record per slot origin, kept by the runtime;
 * the panel, the CLI and the MCP server all read and change the same record.
 */

export const THROTTLE_PRESET = {
  none: 'none',
  fast4g: 'fast-4g',
  slow4g: 'slow-4g',
  g3: '3g',
  offline: 'offline',
  custom: 'custom',
} as const;
export type TThrottlePreset = (typeof THROTTLE_PRESET)[keyof typeof THROTTLE_PRESET];

export interface IThrottle {
  preset: TThrottlePreset;
  /** Added before the response headers of every request, as Chrome adds it to the round trip. */
  latencyMs: number;
  /** 0 = unlimited. Shared by all the slot's requests, as one connection would share it. */
  downKbps: number;
  upKbps: number;
}

/** Chrome DevTools' own presets (Chrome 125+ names), so a number seen here means the same as there. */
export const THROTTLE_PRESETS: Readonly<Record<Exclude<TThrottlePreset, 'custom'>, IThrottle>> = {
  none: { preset: 'none', latencyMs: 0, downKbps: 0, upKbps: 0 },
  'fast-4g': { preset: 'fast-4g', latencyMs: 165, downKbps: 9000, upKbps: 1500 },
  'slow-4g': { preset: 'slow-4g', latencyMs: 563, downKbps: 1600, upKbps: 750 },
  '3g': { preset: '3g', latencyMs: 2000, downKbps: 400, upKbps: 400 },
  offline: { preset: 'offline', latencyMs: 0, downKbps: 0, upKbps: 0 },
};

export interface IDevtoolsSettings {
  throttle: IThrottle;
  /**
   * Blocked addresses: a substring of the full URL, or a pattern with `*` (any run of characters). `posthog`,
   * `*.google-analytics.com/*`, `/api/orders`. Same-site requests are refused by the proxy; requests to other sites
   * by the page's bridge (fetch, XHR, sendBeacon, and scripts/images it can see).
   */
  blocked: string[];
  /** The proxy answers with `no-store` and skips its asset cache: every load is a cold one. */
  disableCache: boolean;
}

export const DEFAULT_SETTINGS: IDevtoolsSettings = {
  throttle: THROTTLE_PRESETS.none,
  blocked: [],
  disableCache: false,
};

export const MAX_BLOCKED = 50;
export const MAX_PATTERN = 300;

export const RESOURCE_TYPE = {
  document: 'document',
  script: 'script',
  style: 'style',
  image: 'image',
  font: 'font',
  media: 'media',
  fetch: 'fetch',
  xhr: 'xhr',
  beacon: 'beacon',
  websocket: 'websocket',
  manifest: 'manifest',
  other: 'other',
} as const;
export type TResourceType = (typeof RESOURCE_TYPE)[keyof typeof RESOURCE_TYPE];

/** Who saw the request: the runtime's proxy (the target site and its neighbours) or the page's bridge (other sites). */
export type TRequestSource = 'proxy' | 'page';

export interface INetworkEntry {
  /** Grows by one per slot; the cursor of `since`. */
  id: number;
  /** ms since epoch, when the request started. */
  at: number;
  source: TRequestSource;
  method: string;
  /** The real address: the target's (localhost:4200, example.com) for proxied requests, never the slot's. */
  url: string;
  host: string;
  type: TResourceType;
  /** null while pending, and for a request that never got an answer (blocked, failed, offline). */
  status: number | null;
  mime: string | null;
  /** Bytes of the body as transferred; null when unknown (opaque cross-origin, pending). */
  size: number | null;
  /** From start to the end of the body; null while pending. */
  durationMs: number | null;
  /** Another site than the target: analytics, CDNs, APIs. */
  thirdParty: boolean;
  blocked: boolean;
  /** Throttling was on when it ran. */
  throttled: boolean;
  /** Network failure in words (`ECONNREFUSED`, `offline`, `aborted`); null when an answer came. */
  error: string | null;
}

export const CONSOLE_LEVEL = { error: 'error', warn: 'warn', info: 'info', log: 'log' } as const;
export type TConsoleLevel = (typeof CONSOLE_LEVEL)[keyof typeof CONSOLE_LEVEL];

/** `console`: console.* of the page; `exception`: window.onerror; `rejection`: an unhandled promise; `network`: a failed request. */
export type TConsoleSource = 'console' | 'exception' | 'rejection' | 'network';

export interface IConsoleEntry {
  id: number;
  at: number;
  level: TConsoleLevel;
  source: TConsoleSource;
  text: string;
  stack: string | null;
  /** The page it happened on (target address). */
  url: string | null;
  /** Identical entries in a row fold into one, as in Chrome. */
  count: number;
}

export type TDevtoolsTarget = { kind: 'port'; port: number } | { kind: 'url'; origin: string };

export interface ISlotSummary {
  /** `https://ai-<handle>-<n>.<domain>`: the key of everything here. */
  slot: string;
  target: TDevtoolsTarget | null;
  /** Last request or report of the page, ms since epoch. */
  lastSeen: number;
  settings: IDevtoolsSettings;
  requests: number;
  errors: number;
}

export interface IDevtoolsSnapshot {
  slot: string;
  target: TDevtoolsTarget | null;
  settings: IDevtoolsSettings;
  network: INetworkEntry[];
  console: IConsoleEntry[];
  /** Pass back as `since` to get only what came after. */
  cursor: number;
}

/** A line of the dev server's terminal that looks like a problem (or all lines, when asked). */
export interface IServerLogLine {
  text: string;
  level: 'error' | 'warn' | 'info';
}

export interface IServerLog {
  /** Where the lines came from: the tmux pane running the port's process, its log file, or nothing found. */
  source:
    | { kind: 'tmux'; session: string; pane: string; command: string }
    | { kind: 'file'; path: string; command: string }
    | null;
  lines: IServerLogLine[];
  /** Why there is no source, in words: an external site, a port nobody listens on, output to nowhere. */
  reason: string | null;
}
