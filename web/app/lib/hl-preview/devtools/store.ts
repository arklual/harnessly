import {
  DEFAULT_SETTINGS,
  MAX_BLOCKED,
  MAX_PATTERN,
  THROTTLE_PRESETS,
  type IConsoleEntry,
  type IDevtoolsSettings,
  type IDevtoolsSnapshot,
  type INetworkEntry,
  type ISlotSummary,
  type IThrottle,
  type TDevtoolsTarget,
} from '@/lib/hl-preview/devtools/types';

/**
 * The runtime's DevTools record per preview slot, in memory: the proxy and the page's bridge write, the
 * panel, the CLI and the MCP server read. Rings, not logs: the newest NETWORK_LIMIT requests and CONSOLE_LIMIT
 * console entries per slot. The custom server (tsup) and Next's API routes are separate bundles of one process, so
 * the record lives on `globalThis`.
 */

export const NETWORK_LIMIT = 1000;
export const CONSOLE_LIMIT = 500;
const MAX_TEXT = 4000;
const MAX_STACK = 4000;
const MAX_URL = 2048;
const MAX_SHORT = 255;
/** Slot records kept at once: a person has a handful of slots, the cap only bounds what a forged origin could add. */
const SLOT_LIMIT = 32;
/** The gateway's slot hosts (`deploy/hl/preview-gateway.mjs` `slotOrigin`). */
const SLOT_ORIGIN = /^https:\/\/ai-[a-z][a-z0-9]{1,15}-[1-9]\.[a-z0-9-]+(\.[a-z0-9-]+)+$/;

interface ISlotRecord {
  slot: string;
  target: TDevtoolsTarget | null;
  lastSeen: number;
  settings: IDevtoolsSettings;
  network: INetworkEntry[];
  console: IConsoleEntry[];
  /** Shared by both rings, so one `since` covers them. */
  seq: number;
}

const g = globalThis as unknown as { __ptPreviewDevtools?: Map<string, ISlotRecord> };
const slots = (): Map<string, ISlotRecord> => {
  if (!g.__ptPreviewDevtools) g.__ptPreviewDevtools = new Map();
  return g.__ptPreviewDevtools;
};

const clipText = (value: string, max: number) => (value.length > max ? `${value.slice(0, max - 1)}…` : value);

const evictOldest = () => {
  const all = slots();
  if (all.size <= SLOT_LIMIT) return;
  const oldest = Array.from(all.values())
    .sort((a, b) => a.lastSeen - b.lastSeen)
    .slice(0, all.size - SLOT_LIMIT);
  for (const record of oldest) all.delete(record.slot);
};

const recordOf = (slot: string): ISlotRecord => {
  let record = slots().get(slot);
  if (!record) {
    record = {
      slot,
      target: null,
      lastSeen: Date.now(),
      settings: DEFAULT_SETTINGS,
      network: [],
      console: [],
      seq: 0,
    };
    slots().set(slot, record);
    evictOldest();
  }
  return record;
};

/** The proxy names the slot's current target with every request it admits. */
export const touchSlot = (slot: string, target: TDevtoolsTarget): void => {
  const record = recordOf(slot);
  record.target = target;
  record.lastSeen = Date.now();
};

export const getSettings = (slot: string): IDevtoolsSettings =>
  slots().get(slot)?.settings ?? DEFAULT_SETTINGS;

/** What the slot shows now, as the proxy last named it; null before its first request. */
export const getTarget = (slot: string): TDevtoolsTarget | null => slots().get(slot)?.target ?? null;

const finite = (value: unknown, max: number): number | null =>
  typeof value === 'number' && Number.isFinite(value) && value >= 0 ? Math.min(Math.round(value), max) : null;

/** A preset fills in its numbers; `custom` keeps the given ones. Unknown input → null. */
export const parseThrottle = (value: unknown): IThrottle | null => {
  if (!value || typeof value !== 'object') return null;
  const raw = value as Record<string, unknown>;
  if (
    typeof raw.preset === 'string' &&
    raw.preset !== 'custom' &&
    Object.hasOwn(THROTTLE_PRESETS, raw.preset)
  ) {
    return THROTTLE_PRESETS[raw.preset as keyof typeof THROTTLE_PRESETS];
  }
  if (raw.preset !== 'custom') return null;
  const latencyMs = finite(raw.latencyMs, 60_000);
  const downKbps = finite(raw.downKbps, 10_000_000);
  const upKbps = finite(raw.upKbps, 10_000_000);
  if (latencyMs === null || downKbps === null || upKbps === null) return null;
  return { preset: 'custom', latencyMs, downKbps, upKbps };
};

export const parseBlocked = (value: unknown): string[] | null => {
  if (!Array.isArray(value)) return null;
  const patterns = value
    .filter((item): item is string => typeof item === 'string')
    .map((item) => item.trim())
    .filter((item) => item.length > 0 && item.length <= MAX_PATTERN);
  return Array.from(new Set(patterns)).slice(0, MAX_BLOCKED);
};

/** Validates a partial change; the error names the field in words. */
export const parseSettingsPatch = (
  value: unknown,
): { ok: true; patch: Partial<IDevtoolsSettings> } | { ok: false; error: string } => {
  if (!value || typeof value !== 'object') return { ok: false, error: 'A settings object is required' };
  const raw = value as Record<string, unknown>;
  const patch: Partial<IDevtoolsSettings> = {};
  if (raw.throttle !== undefined) {
    const throttle = parseThrottle(raw.throttle);
    if (!throttle)
      return {
        ok: false,
        error:
          'throttle: preset is none, fast-4g, slow-4g, 3g, offline, or custom with latencyMs, downKbps, upKbps',
      };
    patch.throttle = throttle;
  }
  if (raw.blocked !== undefined) {
    const blocked = parseBlocked(raw.blocked);
    if (!blocked) return { ok: false, error: `blocked: a list of address patterns, at most ${MAX_BLOCKED}` };
    patch.blocked = blocked;
  }
  if (raw.disableCache !== undefined) {
    if (typeof raw.disableCache !== 'boolean') return { ok: false, error: 'disableCache: true or false' };
    patch.disableCache = raw.disableCache;
  }
  return { ok: true, patch };
};

export const setSettings = (slot: string, patch: Partial<IDevtoolsSettings>): IDevtoolsSettings => {
  const record = recordOf(slot);
  record.settings = { ...record.settings, ...patch };
  return record.settings;
};

/**
 * `*` patterns are matched piece by piece with indexOf, never as a RegExp: a pattern of many stars against a long URL
 * would backtrack for seconds on every request of the slot. Each piece is found once, left to right: linear in the URL.
 */
const wildcardMatches = (url: string, pattern: string): boolean => {
  const pieces = pattern.split('*').filter(Boolean);
  let from = 0;
  for (const piece of pieces) {
    const at = url.indexOf(piece, from);
    if (at < 0) return false;
    from = at + piece.length;
  }
  return true;
};

/** The first pattern found anywhere in the full URL, case-insensitively; `*` stands for any run of characters. */
export const blockedBy = (url: string, patterns: readonly string[]): string | null => {
  const lower = url.toLowerCase();
  return patterns.find((pattern) => wildcardMatches(lower, pattern.toLowerCase())) ?? null;
};

const push = <T>(ring: T[], item: T, limit: number) => {
  ring.push(item);
  if (ring.length > limit) ring.splice(0, ring.length - limit);
};

export type TNewNetworkEntry = Omit<INetworkEntry, 'id'>;

const clipOptional = (value: string | null | undefined, max: number) =>
  typeof value === 'string' ? clipText(value, max) : value;

/** Every string of an entry is bounded here, whoever wrote it: the bridge's reports are the page's own words. */
const clipNetwork = <T extends Partial<INetworkEntry>>(entry: T): T => ({
  ...entry,
  ...(entry.url !== undefined ? { url: clipText(entry.url, MAX_URL) } : {}),
  ...(entry.method !== undefined ? { method: clipText(entry.method, 16) } : {}),
  ...(entry.host !== undefined ? { host: clipText(entry.host, MAX_SHORT) } : {}),
  ...(entry.mime !== undefined ? { mime: clipOptional(entry.mime, MAX_SHORT) } : {}),
  ...(entry.error !== undefined ? { error: clipOptional(entry.error, MAX_SHORT) } : {}),
});

export const recordRequest = (slot: string, entry: TNewNetworkEntry): number => {
  const record = recordOf(slot);
  record.lastSeen = Date.now();
  const id = ++record.seq;
  push(record.network, { ...clipNetwork(entry), id }, NETWORK_LIMIT);
  return id;
};

/**
 * Fills in the answer of a pending request. It moves to the end of the ring with a fresh id, so `since` readers
 * see it again once it is finished.
 */
export const finishRequest = (
  slot: string,
  id: number,
  patch: Partial<Omit<INetworkEntry, 'id' | 'at' | 'url'>>,
): void => {
  const record = slots().get(slot);
  if (!record) return;
  const index = record.network.findIndex((entry) => entry.id === id);
  if (index < 0) return;
  const [entry] = record.network.splice(index, 1);
  push(record.network, { ...entry, ...clipNetwork(patch), id: ++record.seq }, NETWORK_LIMIT);
};

export type TNewConsoleEntry = Omit<IConsoleEntry, 'id' | 'count'>;

export const recordConsole = (slot: string, entry: TNewConsoleEntry): void => {
  const record = recordOf(slot);
  record.lastSeen = Date.now();
  const text = clipText(entry.text, MAX_TEXT);
  const stack = entry.stack === null ? null : clipText(entry.stack, MAX_STACK);
  const url = entry.url === null ? null : clipText(entry.url, MAX_URL);
  const last = record.console[record.console.length - 1];
  if (
    last &&
    last.level === entry.level &&
    last.source === entry.source &&
    last.text === text &&
    last.stack === stack
  ) {
    record.console.pop();
    push(
      record.console,
      { ...last, at: entry.at, url, count: last.count + 1, id: ++record.seq },
      CONSOLE_LIMIT,
    );
    return;
  }
  push(record.console, { ...entry, text, stack, url, id: ++record.seq, count: 1 }, CONSOLE_LIMIT);
};

export const clearSlot = (slot: string): void => {
  const record = slots().get(slot);
  if (!record) return;
  record.network = [];
  record.console = [];
};

export const readSlot = (slot: string, since = 0): IDevtoolsSnapshot => {
  const record = slots().get(slot);
  if (!record) return { slot, target: null, settings: DEFAULT_SETTINGS, network: [], console: [], cursor: 0 };
  return {
    slot,
    target: record.target,
    settings: record.settings,
    network: record.network.filter((entry) => entry.id > since),
    console: record.console.filter((entry) => entry.id > since),
    cursor: record.seq,
  };
};

const isError = (entry: INetworkEntry) =>
  entry.blocked || entry.error !== null || (entry.status !== null && entry.status >= 400);

/** Newest first. */
export const listSlots = (): ISlotSummary[] =>
  Array.from(slots().values())
    .sort((a, b) => b.lastSeen - a.lastSeen)
    .map((record) => ({
      slot: record.slot,
      target: record.target,
      lastSeen: record.lastSeen,
      settings: record.settings,
      requests: record.network.length,
      errors:
        record.network.filter(isError).length +
        record.console.filter((entry) => entry.level === 'error').length,
    }));

/** A slot the caller named, or the one the person used last: agents rarely know the slot's name. */
export const resolveSlot = (asked: string | null | undefined): string | null => {
  if (asked) {
    const normalized = asked.replace(/\/+$/, '').toLowerCase();
    return slots().has(normalized) || SLOT_ORIGIN.test(normalized) ? normalized : null;
  }
  return listSlots()[0]?.slot ?? null;
};

/** Tests only. */
export const resetDevtools = (): void => {
  slots().clear();
};
