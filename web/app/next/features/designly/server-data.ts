// What the «Server» tab shows of a board: the library server is the runtime itself, so its record is what the runtime
// knows of this board — the requests the board's page made to it (the preview slot's record), what the page logged by
// its own script (errors, long tasks, slow loads) and the last check of the board in a headless browser. Nothing here is
// made up: a kind of data the runtime does not keep (a build log, an import log, a terminal) is not on the tab, and the
// tab says so. No React here.
import type { INetworkEntry } from '@/next/core/net/preview-devtools';
import {
  isFailed,
  statusText,
  timeText,
  visible,
  type IDevFilter,
  type IDevRow,
} from '@/next/features/designly/devtools-data';
import { locale, t } from '@/next/i18n';

/** One report of the board's page to the library (`/design/library/log`). */
export interface IPageLogEntry {
  at: number;
  /** `error`, `rejection`, `longtask` or `slow`. */
  kind: string;
  /** The board's address in the project, as the page told it. */
  page: string;
  message: string;
}

export interface ICheckShot {
  view: string;
  status: string;
  errors: string[];
  failedRequests: string[];
}

export interface ICheckBoard {
  path: string;
  status: string;
  shots: ICheckShot[];
}

/** The last check of a design (`/design/library/check`), as far as the tab reads it. */
export interface ICheckReport {
  startedAt: number;
  finishedAt: number | null;
  failure: string | null;
  boards: ICheckBoard[];
}

const strings = (v: unknown): string[] =>
  Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : [];
const num = (v: unknown) => (typeof v === 'number' && Number.isFinite(v) ? v : 0);
const text = (v: unknown) => (typeof v === 'string' ? v : '');
const list = (v: unknown): Record<string, unknown>[] =>
  Array.isArray(v) ? v.filter((x): x is Record<string, unknown> => !!x && typeof x === 'object') : [];

/** The library's log as it comes: a malformed entry is left out. */
export const readPageLog = (raw: unknown): IPageLogEntry[] =>
  list(raw).map((e) => ({ at: num(e.at), kind: text(e.kind), page: text(e.page), message: text(e.message) }));

/** The check's report as it comes (a Go slice that was empty may come as null); null when the design was never checked. */
export const readCheck = (raw: unknown): ICheckReport | null => {
  if (!raw || typeof raw !== 'object') return null;
  const r = raw as Record<string, unknown>;
  return {
    startedAt: num(r.startedAt),
    finishedAt: typeof r.finishedAt === 'number' ? r.finishedAt : null,
    failure: typeof r.failure === 'string' && r.failure !== '' ? r.failure : null,
    boards: list(r.boards).map((b) => ({
      path: text(b.path),
      status: text(b.status),
      shots: list(b.shots).map((s) => ({
        view: text(s.view),
        status: text(s.status),
        errors: strings(s.errors),
        failedRequests: strings(s.failedRequests),
      })),
    })),
  };
};

export interface IServerSources {
  /** The slot's network record; the requests to other sites are not the server's. */
  requests: readonly INetworkEntry[];
  pageLog: readonly IPageLogEntry[];
  check: ICheckReport | null;
}

export const noSources = (): IServerSources => ({ requests: [], pageLog: [], check: null });

export interface IServerRow extends IDevRow {
  /** When it happened, on the runtime's clock. */
  at: number;
}

const clock = (at: number) =>
  at === 0
    ? ''
    : new Intl.DateTimeFormat(locale(), {
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
        hour12: false,
      }).format(at);

/** A board's address without its query, decoded: the page tells it encoded, the library lists it as it is on disk. */
const boardFile = (path: string) => {
  const bare = path.split('?')[0];
  try {
    return decodeURIComponent(bare);
  } catch {
    return bare;
  }
};

const sameBoard = (a: string, b: string) => boardFile(a) === boardFile(b);

/** The library script's own channels (live updates, state, log) and the slot's bridge are not the board's requests. */
const SERVICE_PATH = /^\/__(lib|tk)\//;

const pathOf = (url: string) => {
  try {
    const u = new URL(url);
    return `${u.pathname}${u.search}`;
  } catch {
    return url;
  }
};

const firstLine = (text: string) => text.split('\n')[0];

const LOG_LEVEL: Record<string, IDevRow['lv']> = {
  error: 'error',
  rejection: 'error',
  longtask: 'warn',
  slow: 'warn',
};

const requestRow = (e: INetworkEntry): IServerRow => {
  const status = statusText(e);
  const took = e.durationMs === null ? '' : ` · ${timeText(e.durationMs)}`;
  const text = `${e.method} ${pathOf(e.url)} → ${status}${took}`;
  return {
    key: `req:${e.id}`,
    at: e.at,
    lv: isFailed(e) ? 'error' : '',
    cells: [clock(e.at), text],
    detail: text,
    title: e.url,
  };
};

const logRow = (e: IPageLogEntry, board: string, index: number): IServerRow => {
  const where = sameBoard(e.page, board) ? '' : ` (${boardFile(e.page)})`;
  const text = `${firstLine(e.message)}${where}`;
  return {
    key: `log:${e.at}:${index}`,
    at: e.at,
    lv: LOG_LEVEL[e.kind] ?? '',
    cells: [clock(e.at), text],
    detail: `${e.message}${where}`,
    title: e.message,
  };
};

const SHOT_VIEWS = new Set(['desktop', 'phone']);
const viewText = (view: string) => (SHOT_VIEWS.has(view) ? t(`next.site.dev.server.view.${view}`) : view);

/** The problems of the last check on this board, one row each; the check's time is their time. */
const checkRows = (report: ICheckReport, board: string): IServerRow[] => {
  const at = report.finishedAt ?? report.startedAt;
  const rows: IServerRow[] = [];
  const add = (key: string, lv: IDevRow['lv'], text: string) => {
    const line = t('next.site.dev.server.check', { text });
    rows.push({ key: `check:${key}`, at, lv, cells: [clock(at), line], detail: line, title: text });
  };
  if (report.failure !== null) add('failure', 'error', report.failure);
  for (const found of report.boards.filter((b) => sameBoard(b.path, board))) {
    for (const shot of found.shots) {
      const view = viewText(shot.view);
      shot.errors.forEach((text, i) => add(`${shot.view}:e${i}`, 'error', `${view}: ${firstLine(text)}`));
      shot.failedRequests.forEach((text, i) => add(`${shot.view}:r${i}`, 'error', `${view}: ${text}`));
      if (shot.status === 'blank')
        add(`${shot.view}:blank`, 'warn', `${view}: ${t('next.site.dev.server.blank')}`);
    }
  }
  return rows;
};

/** Every row the runtime has for the board, oldest first. */
export const allServerRows = (sources: IServerSources, board: string): IServerRow[] => {
  const rows = [
    ...sources.requests.filter((e) => !e.thirdParty && !SERVICE_PATH.test(pathOf(e.url))).map(requestRow),
    ...sources.pageLog.map((e, i) => logRow(e, board, i)),
    ...(sources.check ? checkRows(sources.check, board) : []),
  ];
  return rows.sort((a, b) => a.at - b.at);
};

const group = (row: IServerRow) => (row.lv === 'error' ? 'error' : 'info');

export const serverRows = (rows: readonly IServerRow[], f: IDevFilter): IServerRow[] =>
  rows.filter((row) => visible(row.at, group(row), row.cells.join(' '), f));

/** The newest time among the rows: «clear» hides what is there now and leaves what comes after. */
export const lastAt = (rows: readonly IServerRow[]) => rows.reduce((max, row) => Math.max(max, row.at), 0);

export const serverErrors = (rows: readonly IServerRow[], clearedAt: number) =>
  rows.filter((row) => row.at > clearedAt && row.lv === 'error').length;
