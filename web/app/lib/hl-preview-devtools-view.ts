import { locale, plural, t } from '@/lib/i18n';
import {
  DEFAULT_SETTINGS,
  MAX_PATTERN,
  THROTTLE_PRESETS,
  type IConsoleEntry,
  type IDevtoolsSettings,
  type IDevtoolsSnapshot,
  type INetworkEntry,
  type IThrottle,
  type TConsoleLevel,
  type TResourceType,
  type TThrottlePreset,
} from '@/lib/hl-preview/devtools/types';

/** What the panel's DevTools drawer keeps of a slot between polls, and how it shows it. No React here. */

/** The runtime keeps the same rings; the panel never holds more than it could be sent. */
export const NETWORK_KEEP = 1000;
export const CONSOLE_KEEP = 500;
/** Rows drawn at once: the newest ones, as the end of the log is what a person looks at. */
export const RENDER_LIMIT = 500;

export interface IDevtoolsLog {
  slot: string | null;
  settings: IDevtoolsSettings;
  network: INetworkEntry[];
  console: IConsoleEntry[];
  cursor: number;
}

export const emptyLog = (slot: string | null): IDevtoolsLog => ({
  slot,
  settings: DEFAULT_SETTINGS,
  network: [],
  console: [],
  cursor: 0,
});

export const isPending = (entry: INetworkEntry): boolean =>
  entry.status === null && entry.durationMs === null && entry.error === null && !entry.blocked;

export const isFailed = (entry: INetworkEntry): boolean =>
  entry.blocked || entry.error !== null || (entry.status !== null && entry.status >= 400);

const sameRequest = (a: INetworkEntry, b: INetworkEntry) =>
  a.at === b.at && a.url === b.url && a.method === b.method;

/**
 * A finished request comes back with a new id and nothing else to tie it to its pending row: the start time, the address
 * and the method do. Each arrival replaces at most one pending row, the oldest such.
 */
export const mergeNetwork = (
  known: readonly INetworkEntry[],
  incoming: readonly INetworkEntry[],
  keep = NETWORK_KEEP,
): INetworkEntry[] => {
  const next = [...known];
  const ids = new Set(next.map((entry) => entry.id));
  for (const entry of incoming) {
    if (ids.has(entry.id)) continue;
    const superseded = next.findIndex(
      (old) => isPending(old) && old.id < entry.id && sameRequest(old, entry),
    );
    if (superseded >= 0) next.splice(superseded, 1);
    next.push(entry);
    ids.add(entry.id);
  }
  return next.length > keep ? next.slice(next.length - keep) : next;
};

const sameMessage = (a: IConsoleEntry, b: IConsoleEntry) =>
  a.level === b.level && a.source === b.source && a.text === b.text && a.stack === b.stack;

/** A repeat folds into the last entry on the runtime, which comes back with a new id and a bigger count. */
export const mergeConsole = (
  known: readonly IConsoleEntry[],
  incoming: readonly IConsoleEntry[],
  keep = CONSOLE_KEEP,
): IConsoleEntry[] => {
  const next = [...known];
  for (const entry of incoming) {
    if (next.some((old) => old.id === entry.id)) continue;
    const last = next[next.length - 1];
    if (entry.count > 1 && last && last.count < entry.count && sameMessage(last, entry)) next.pop();
    next.push(entry);
  }
  return next.length > keep ? next.slice(next.length - keep) : next;
};

/**
 * One poll's answer onto what is known. A cursor behind the one asked with means the runtime started over (restart):
 * what is known belongs to a record that no longer exists, so `restart` is true and the caller asks again from 0.
 */
export const applySnapshot = (
  log: IDevtoolsLog,
  snapshot: IDevtoolsSnapshot,
  since: number,
): { log: IDevtoolsLog; restart: boolean } => {
  if (snapshot.cursor < since)
    return { log: { ...emptyLog(log.slot), settings: snapshot.settings }, restart: true };
  return {
    log: {
      slot: log.slot,
      settings: snapshot.settings,
      network: mergeNetwork(log.network, snapshot.network),
      console: mergeConsole(log.console, snapshot.console),
      cursor: snapshot.cursor,
    },
    restart: false,
  };
};

/** Network conditions differ from the person's own: worth a dot on the toggle so a slowed page is not forgotten. */
export const conditionsChanged = (settings: IDevtoolsSettings): boolean =>
  settings.throttle.preset !== 'none' || settings.blocked.length > 0;

export const THROTTLE_LABELS: Record<TThrottlePreset, string> = {
  none: t('lib.hlPreviewDevtoolsView.throttleNone'),
  'fast-4g': t('lib.hlPreviewDevtoolsView.throttleFast4g'),
  'slow-4g': t('lib.hlPreviewDevtoolsView.throttleSlow4g'),
  '3g': '3G',
  offline: t('lib.hlPreviewDevtoolsView.throttleOffline'),
  custom: t('lib.hlPreviewDevtoolsView.throttleCustom'),
};

/** The decimal separator of the page's language: `1,5` in Russian, `1.5` in English. */
const decimal = (value: number, digits = 1): string =>
  value.toFixed(digits).replace(/\.0+$/, '').replace('.', (1.5).toLocaleString(locale()).charAt(1));

export const formatKbps = (kbps: number): string => {
  if (kbps === 0) return t('lib.hlPreviewDevtoolsView.unlimited');
  return kbps >= 1000
    ? t('lib.hlPreviewDevtoolsView.mbps', { value: decimal(kbps / 1000) })
    : t('lib.hlPreviewDevtoolsView.kbps', { value: kbps });
};

/** "165 ms · ↓ 9 Mbit/s · ↑ 1.5 Mbit/s". */
export const describeThrottle = (throttle: IThrottle): string => {
  if (throttle.preset === 'offline') return t('lib.hlPreviewDevtoolsView.networkOffline');
  if (throttle.preset === 'none') return t('lib.hlPreviewDevtoolsView.ownConnection');
  return `${t('lib.hlPreviewDevtoolsView.ms', { value: throttle.latencyMs })} · ↓ ${formatKbps(throttle.downKbps)} · ↑ ${formatKbps(throttle.upKbps)}`;
};

export const throttlePresetOptions = (): IThrottle[] => Object.values(THROTTLE_PRESETS);

export const formatBytes = (bytes: number | null): string => {
  if (bytes === null) return '—';
  if (bytes < 1024) return t('lib.hlPreviewDevtoolsView.bytes', { value: bytes });
  if (bytes < 1024 * 1024) return t('lib.hlPreviewDevtoolsView.kilobytes', { value: decimal(bytes / 1024) });
  return t('lib.hlPreviewDevtoolsView.megabytes', { value: decimal(bytes / (1024 * 1024)) });
};

export const formatDuration = (ms: number | null): string => {
  if (ms === null) return '—';
  if (ms < 1) return t('lib.hlPreviewDevtoolsView.underOneMs');
  if (ms < 1000) return t('lib.hlPreviewDevtoolsView.ms', { value: Math.round(ms) });
  return t('lib.hlPreviewDevtoolsView.seconds', { value: decimal(ms / 1000, 2) });
};

const pad = (value: number, size = 2) => String(value).padStart(size, '0');

/** Local clock time with milliseconds, as a log line shows it. */
export const formatClock = (at: number): string => {
  const date = new Date(at);
  return `${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}.${pad(date.getMilliseconds(), 3)}`;
};

export const TYPE_LABELS: Record<TResourceType, string> = {
  document: t('lib.hlPreviewDevtoolsView.typeDocument'),
  script: 'js',
  style: 'css',
  image: t('lib.hlPreviewDevtoolsView.typeImage'),
  font: t('lib.hlPreviewDevtoolsView.typeFont'),
  media: t('lib.hlPreviewDevtoolsView.typeMedia'),
  fetch: 'fetch',
  xhr: 'xhr',
  beacon: 'beacon',
  websocket: 'ws',
  manifest: 'manifest',
  other: t('lib.hlPreviewDevtoolsView.typeOther'),
};

/** What the status column says: the code, or why there is none. */
export const statusLabel = (entry: INetworkEntry): string => {
  if (entry.blocked) return t('lib.hlPreviewDevtoolsView.statusBlocked');
  if (entry.status !== null) return String(entry.status);
  if (entry.error !== null) return t('lib.hlPreviewDevtoolsView.statusFailed');
  return '…';
};

/** What the time column says: a pending request is still running, one that never got an answer has no time. */
export const timeLabel = (entry: INetworkEntry): string =>
  isPending(entry) ? '…' : formatDuration(entry.durationMs);

/** How long a request has taken so far: a pending one runs up to `now`, one that never got an answer took no time. */
const elapsed = (entry: INetworkEntry, now: number): number =>
  entry.durationMs ?? (isPending(entry) ? Math.max(0, now - entry.at) : 0);

/** The short name of a request: its path and query, the host only for the bare root. */
export const requestName = (entry: INetworkEntry): string => {
  try {
    const url = new URL(entry.url);
    const path = `${url.pathname}${url.search}`;
    return path === '/' ? url.host : path;
  } catch {
    return entry.url;
  }
};

export const NETWORK_FILTER = {
  all: 'all',
  fetch: 'fetch',
  document: 'document',
  script: 'script',
  style: 'style',
  image: 'image',
  font: 'font',
  thirdParty: 'third-party',
  blocked: 'blocked',
  failed: 'failed',
} as const;
export type TNetworkFilter = (typeof NETWORK_FILTER)[keyof typeof NETWORK_FILTER];

export const NETWORK_FILTER_LABELS: Record<TNetworkFilter, string> = {
  all: t('lib.hlPreviewDevtoolsView.filterAll'),
  fetch: 'Fetch/XHR',
  document: t('lib.hlPreviewDevtoolsView.filterDocument'),
  script: 'JS',
  style: 'CSS',
  image: t('lib.hlPreviewDevtoolsView.filterImage'),
  font: t('lib.hlPreviewDevtoolsView.filterFont'),
  'third-party': t('lib.hlPreviewDevtoolsView.filterThirdParty'),
  blocked: t('lib.hlPreviewDevtoolsView.filterBlocked'),
  failed: t('lib.hlPreviewDevtoolsView.filterFailed'),
};

const byKind: Record<TNetworkFilter, (entry: INetworkEntry) => boolean> = {
  all: () => true,
  fetch: (entry) => entry.type === 'fetch' || entry.type === 'xhr' || entry.type === 'beacon',
  document: (entry) => entry.type === 'document',
  script: (entry) => entry.type === 'script',
  style: (entry) => entry.type === 'style',
  image: (entry) => entry.type === 'image',
  font: (entry) => entry.type === 'font',
  'third-party': (entry) => entry.thirdParty,
  blocked: (entry) => entry.blocked,
  failed: isFailed,
};

/**
 * The text filter as in Chrome: every word must be found in the address, a word with a leading `-` must not be.
 * Case does not matter.
 */
export const textMatcher = (query: string): ((text: string) => boolean) => {
  const words = query
    .toLowerCase()
    .split(/\s+/)
    .filter((word) => word.length > 0 && word !== '-');
  if (words.length === 0) return () => true;
  return (text) => {
    const lower = text.toLowerCase();
    return words.every((word) =>
      word.startsWith('-') ? !lower.includes(word.slice(1)) : lower.includes(word),
    );
  };
};

/** Requests in the order they started, as Chrome lists them. */
export const filterNetwork = (
  entries: readonly INetworkEntry[],
  kind: TNetworkFilter,
  query: string,
): INetworkEntry[] => {
  const matches = textMatcher(query);
  return entries
    .filter((entry) => byKind[kind](entry) && matches(entry.url))
    .sort((a, b) => a.at - b.at || a.id - b.id);
};

export const CONSOLE_FILTER = { all: 'all', errors: 'errors', warnings: 'warnings' } as const;
export type TConsoleFilter = (typeof CONSOLE_FILTER)[keyof typeof CONSOLE_FILTER];

const levelFits: Record<TConsoleFilter, (level: TConsoleLevel) => boolean> = {
  all: () => true,
  errors: (level) => level === 'error',
  warnings: (level) => level === 'error' || level === 'warn',
};

export const filterConsole = (
  entries: readonly IConsoleEntry[],
  filter: TConsoleFilter,
  query: string,
): IConsoleEntry[] => {
  const matches = textMatcher(query);
  return entries.filter(
    (entry) => levelFits[filter](entry.level) && matches(`${entry.text}\n${entry.url ?? ''}`),
  );
};

/** The newest `limit` items and how many were left out. */
export const tail = <T>(
  items: readonly T[],
  limit = RENDER_LIMIT,
): { shown: readonly T[]; hidden: number } =>
  items.length > limit
    ? { shown: items.slice(items.length - limit), hidden: items.length - limit }
    : { shown: items, hidden: 0 };

export interface INetworkTotals {
  requests: number;
  bytes: number;
  /** From the first start to the last end, ms; 0 without finished requests. */
  spanMs: number;
}

export const networkTotals = (entries: readonly INetworkEntry[]): INetworkTotals => {
  let bytes = 0;
  let first = Infinity;
  let last = -Infinity;
  for (const entry of entries) {
    bytes += entry.size ?? 0;
    first = Math.min(first, entry.at);
    if (entry.durationMs !== null) last = Math.max(last, entry.at + entry.durationMs);
  }
  return { requests: entries.length, bytes, spanMs: last > first ? last - first : 0 };
};

export interface IWaterfall {
  start: number;
  span: number;
}

/** The time window the waterfall column spreads over: pending requests reach up to `now`. */
export const waterfallWindow = (entries: readonly INetworkEntry[], now: number): IWaterfall => {
  if (entries.length === 0) return { start: now, span: 1 };
  let start = Infinity;
  let end = -Infinity;
  for (const entry of entries) {
    start = Math.min(start, entry.at);
    end = Math.max(end, entry.at + elapsed(entry, now));
  }
  return { start, span: Math.max(1, end - start) };
};

/** Left offset and width of one bar, in percent of the window; at least a sliver so a fast request stays visible. */
export const waterfallBar = (
  entry: INetworkEntry,
  window: IWaterfall,
  now: number,
): { left: number; width: number } => {
  const duration = elapsed(entry, now);
  const left = ((entry.at - window.start) / window.span) * 100;
  const width = Math.max(0.6, (duration / window.span) * 100);
  return { left: Math.min(left, 99.4), width: Math.min(width, 100 - Math.min(left, 99.4)) };
};

export interface IDomainRow {
  host: string;
  requests: number;
  /** null when no request of the host told its size (opaque cross-origin answers). */
  bytes: number | null;
  failed: number;
  blocked: number;
  thirdParty: boolean;
}

/** Who the page talks to: one row per host, the busiest first. Third parties are analytics, CDNs and other APIs. */
export const groupByDomain = (entries: readonly INetworkEntry[]): IDomainRow[] => {
  const rows = new Map<string, IDomainRow>();
  for (const entry of entries) {
    const row = rows.get(entry.host) ?? {
      host: entry.host,
      requests: 0,
      bytes: null,
      failed: 0,
      blocked: 0,
      thirdParty: entry.thirdParty,
    };
    row.requests += 1;
    if (entry.size !== null) row.bytes = (row.bytes ?? 0) + entry.size;
    if (isFailed(entry)) row.failed += 1;
    if (entry.blocked) row.blocked += 1;
    rows.set(entry.host, row);
  }
  return Array.from(rows.values()).sort(
    (a, b) => b.requests - a.requests || (b.bytes ?? 0) - (a.bytes ?? 0) || a.host.localeCompare(b.host),
  );
};

/** "Block address": the address without its query, so the same call with other parameters is blocked too. */
export const addressPattern = (url: string): string => {
  let pattern = url;
  try {
    const parsed = new URL(url);
    pattern = `${parsed.origin}${parsed.pathname}`;
  } catch {
    pattern = url.split(/[?#]/)[0];
  }
  return pattern.slice(0, MAX_PATTERN);
};

/** "Block domain": every request to the host, and only to it. */
export const domainPattern = (host: string): string => `*://${host}/*`.slice(0, MAX_PATTERN);

/** Adds a pattern once; the runtime trims and dedupes again. */
export const withPattern = (blocked: readonly string[], pattern: string): string[] => {
  const trimmed = pattern.trim();
  return trimmed && !blocked.includes(trimmed) ? [...blocked, trimmed] : [...blocked];
};

export const withoutPattern = (blocked: readonly string[], pattern: string): string[] =>
  blocked.filter((item) => item !== pattern);

/** Requests counted in the page's language: "1 request", "5 requests". */
export const pluralRequests = (count: number): string => plural(count, 'lib.hlPreviewDevtoolsView.requests');
