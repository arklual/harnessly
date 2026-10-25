// What the site's DevTools keep of the page between polls, and the rows they draw of it: the console and the network of
// the page's preview slot, filtered by the type chips and the search field. The runtime's record is the truth; this is
// only how a person reads it. No React here.
import type {
  IConsoleEntry,
  IDevtoolsSettings,
  IDevtoolsSnapshot,
  INetworkEntry,
} from '@/next/core/net/preview-devtools';
import { locale, plural, t } from '@/next/i18n';

/** The runtime keeps the same rings; the panel never holds more than it could be sent. */
const NETWORK_KEEP = 1000;
const CONSOLE_KEEP = 500;
/** Rows drawn at once: the newest ones, as the end of the log is what a person looks at. */
export const RENDER_LIMIT = 300;

export const DEV_TAB = {
  elements: 'elements',
  console: 'console',
  network: 'network',
  server: 'server',
} as const;
export type TDevTab = (typeof DEV_TAB)[keyof typeof DEV_TAB];
/** The tabs in the design's order. */
export const DEV_TABS: readonly TDevTab[] = [
  DEV_TAB.elements,
  DEV_TAB.console,
  DEV_TAB.network,
  DEV_TAB.server,
];

/** The chips of a tab: `all` and the groups its rows fall into. The elements have none: they are a tree, not rows. */
export const DEV_TYPES = {
  elements: [],
  console: ['all', 'error', 'warn', 'info'],
  network: ['all', 'fetch', 'doc', 'js', 'css', 'img', 'font'],
  server: ['all', 'error'],
} as const satisfies Record<TDevTab, readonly string[]>;

export const DEFAULT_SETTINGS: IDevtoolsSettings = {
  throttle: { preset: 'none', latencyMs: 0, downKbps: 0, upKbps: 0 },
  blocked: [],
  disableCache: false,
};

export interface IDevtoolsLog {
  /** The slot this record is of: a log of another page is not shown under the new one. */
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

export const isPending = (e: INetworkEntry) =>
  e.status === null && e.durationMs === null && e.error === null && !e.blocked;

export const isFailed = (e: INetworkEntry) =>
  e.blocked || e.error !== null || (e.status !== null && e.status >= 400);

const sameRequest = (a: INetworkEntry, b: INetworkEntry) =>
  a.at === b.at && a.url === b.url && a.method === b.method;

/**
 * A finished request comes back with a new id and nothing else to tie it to its pending row: the start time, the address
 * and the method do. Each arrival replaces at most one pending row, the oldest such.
 */
export const mergeNetwork = (known: readonly INetworkEntry[], incoming: readonly INetworkEntry[]) => {
  const next = [...known];
  const ids = new Set(next.map((e) => e.id));
  for (const entry of incoming) {
    if (ids.has(entry.id)) continue;
    const superseded = next.findIndex(
      (old) => isPending(old) && old.id < entry.id && sameRequest(old, entry),
    );
    if (superseded >= 0) next.splice(superseded, 1);
    next.push(entry);
    ids.add(entry.id);
  }
  return next.length > NETWORK_KEEP ? next.slice(next.length - NETWORK_KEEP) : next;
};

const sameMessage = (a: IConsoleEntry, b: IConsoleEntry) =>
  a.level === b.level && a.source === b.source && a.text === b.text;

/** A repeat folds into the last entry on the runtime, which comes back with a new id and a bigger count. */
export const mergeConsole = (known: readonly IConsoleEntry[], incoming: readonly IConsoleEntry[]) => {
  const next = [...known];
  for (const entry of incoming) {
    if (next.some((old) => old.id === entry.id)) continue;
    const last = next[next.length - 1];
    if (entry.count > 1 && last && last.count < entry.count && sameMessage(last, entry)) next.pop();
    next.push(entry);
  }
  return next.length > CONSOLE_KEEP ? next.slice(next.length - CONSOLE_KEEP) : next;
};

/** One poll's answer onto what is known. A cursor behind the one asked with means the runtime started over. */
export const applySnapshot = (log: IDevtoolsLog, snapshot: IDevtoolsSnapshot, since: number) => {
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

export interface IDevRow {
  key: string;
  lv: 'error' | 'warn' | '';
  cells: readonly string[];
  /** The row in a line, as it goes into the chat. */
  detail: string;
  /** The whole address or text, for a long row's tooltip. */
  title: string;
}

export interface IDevFilter {
  /** Entries up to this id were cleared by the person. */
  clearedAt: number;
  type: string;
  /** Lower-cased, trimmed. */
  query: string;
}

const consoleGroup = (e: IConsoleEntry) => (e.level === 'log' ? 'info' : e.level);

const NETWORK_GROUP: Record<string, string> = {
  fetch: 'fetch',
  xhr: 'fetch',
  beacon: 'fetch',
  websocket: 'fetch',
  document: 'doc',
  script: 'js',
  style: 'css',
  image: 'img',
  font: 'font',
};
const networkGroup = (e: INetworkEntry) => NETWORK_GROUP[e.type] ?? 'other';

/** The address as the name of a row: the file or the path asked for; another site's request keeps its host. */
export const nameOf = (e: INetworkEntry) => {
  let url: URL;
  try {
    url = new URL(e.url);
  } catch {
    return e.url;
  }
  const path = url.pathname.split('/').filter(Boolean).at(-1) ?? '/';
  const name = `${path}${url.search}`;
  return e.thirdParty ? `${url.host}/${name}`.replace(/\/\/$/, '/') : name;
};

const number = (n: number, digits: number) =>
  new Intl.NumberFormat(locale(), { maximumFractionDigits: digits }).format(n);

export const sizeText = (bytes: number | null) => {
  if (bytes === null) return '—';
  if (bytes < 1024) return t('next.site.dev.bytes', { n: bytes });
  const kb = bytes / 1024;
  if (kb < 1024) return t('next.site.dev.kb', { n: number(kb, kb < 10 ? 1 : 0) });
  return t('next.site.dev.mb', { n: number(kb / 1024, 1) });
};

export const timeText = (ms: number | null) => (ms === null ? '—' : t('next.site.dev.ms', { n: ms }));

export const statusText = (e: INetworkEntry) => {
  if (e.blocked) return t('next.site.dev.blocked');
  if (e.error !== null) return t('next.site.dev.failed');
  if (e.status === null) return '…';
  return String(e.status);
};

/** The kinds the dictionary has a word for; anything else is «other». */
const KINDS = new Set([...Object.keys(NETWORK_GROUP), 'media', 'manifest']);
const kindText = (e: INetworkEntry) => t(`next.site.dev.kind.${KINDS.has(e.type) ? e.type : 'other'}`);

export const visible = (id: number, group: string, text: string, f: IDevFilter) =>
  id > f.clearedAt &&
  (f.type === 'all' || f.type === group) &&
  (f.query === '' || text.toLowerCase().includes(f.query));

const SOURCE_KEY: Record<string, string> = {
  exception: 'next.site.dev.exception',
  rejection: 'next.site.dev.rejection',
  network: 'next.site.dev.netError',
};

export const consoleRows = (entries: readonly IConsoleEntry[], f: IDevFilter): IDevRow[] =>
  entries
    .filter((e) => visible(e.id, consoleGroup(e), e.text, f))
    .slice(-RENDER_LIMIT)
    .map((e) => {
      const first = SOURCE_KEY[e.source];
      const where = e.count > 1 ? `×${e.count}` : first ? t(first) : '';
      let lv: IDevRow['lv'] = '';
      if (e.level === 'error' || e.level === 'warn') lv = e.level;
      return { key: String(e.id), lv, cells: [e.text, where], detail: e.text, title: e.text };
    });

export const networkRows = (entries: readonly INetworkEntry[], f: IDevFilter): IDevRow[] =>
  entries
    .filter((e) => visible(e.id, networkGroup(e), e.url, f))
    .slice(-RENDER_LIMIT)
    .map((e) => {
      const name = nameOf(e);
      const status = statusText(e);
      const time = timeText(e.durationMs);
      return {
        key: String(e.id),
        lv: isFailed(e) ? 'error' : '',
        cells: [name, status, kindText(e), sizeText(e.size), time],
        detail: `${e.method} ${e.url} → ${status}${e.durationMs === null ? '' : ` · ${time}`}`,
        title: e.url,
      };
    });

/** The errors a tab's badge counts: what is on the tab now, not cleared. */
export const errorCount = (log: IDevtoolsLog, tab: TDevTab, clearedAt: number) => {
  if (tab === DEV_TAB.console)
    return log.console.filter((e) => e.id > clearedAt && e.level === 'error').length;
  if (tab === DEV_TAB.network) return log.network.filter((e) => e.id > clearedAt && isFailed(e)).length;
  return 0;
};

/** «8 requests · 412 KB transferred · 312 ms»: the network tab's foot. */
export const networkFoot = (entries: readonly INetworkEntry[], clearedAt: number) => {
  const shown = entries.filter((e) => e.id > clearedAt);
  if (shown.length === 0) return '';
  const bytes = shown.reduce((sum, e) => sum + (e.size ?? 0), 0);
  const slowest = shown.reduce((max, e) => Math.max(max, e.durationMs ?? 0), 0);
  return t('next.site.dev.foot', {
    requests: plural(shown.length, 'next.site.dev.requests'),
    size: sizeText(bytes),
    ms: slowest,
  });
};
