import { beforeAll, describe, expect, it } from 'vitest';
import type { IConsoleEntry, INetworkEntry } from '@/next/core/net/preview-devtools';
import {
  applySnapshot,
  consoleRows,
  emptyLog,
  errorCount,
  mergeConsole,
  mergeNetwork,
  nameOf,
  networkFoot,
  networkRows,
  sizeText,
} from '@/next/features/designly/devtools-data';
import { pinLocale } from '@/next/i18n';

beforeAll(() => pinLocale('en'));

const net = (over: Partial<INetworkEntry> & { id: number }): INetworkEntry => ({
  at: 1000,
  method: 'GET',
  url: 'http://localhost:4200/main.js',
  host: 'localhost:4200',
  type: 'script',
  status: 200,
  size: 2048,
  durationMs: 40,
  thirdParty: false,
  blocked: false,
  error: null,
  ...over,
});

const log = (over: Partial<IConsoleEntry> & { id: number }): IConsoleEntry => ({
  at: 1000,
  level: 'log',
  source: 'console',
  text: 'hello',
  count: 1,
  ...over,
});

const ALL = { clearedAt: 0, type: 'all', query: '' };

describe('mergeNetwork', () => {
  it('a finished request takes the place of its pending row', () => {
    const pending = net({ id: 1, status: null, size: null, durationMs: null });
    const done = net({ id: 2 });
    expect(mergeNetwork([pending], [done]).map((e) => e.id)).toEqual([2]);
  });

  it('a row it has already is not added twice', () => {
    const row = net({ id: 1 });
    expect(mergeNetwork([row], [row])).toHaveLength(1);
  });
});

describe('mergeConsole', () => {
  it('a repeat folds into the last entry with its bigger count', () => {
    const first = log({ id: 1 });
    const again = log({ id: 2, count: 2 });
    expect(mergeConsole([first], [again])).toEqual([again]);
  });
});

describe('applySnapshot', () => {
  const settings = emptyLog(null).settings;

  it('a cursor behind the asked one means the runtime started over', () => {
    const known = { ...emptyLog('s'), cursor: 9, console: [log({ id: 9 })] };
    const answer = applySnapshot(known, { settings, network: [], console: [], cursor: 2 }, 9);
    expect(answer.restart).toBe(true);
    expect(answer.log.console).toEqual([]);
  });

  it('news go after what is known', () => {
    const known = { ...emptyLog('s'), cursor: 1, console: [log({ id: 1 })] };
    const answer = applySnapshot(
      known,
      { settings, network: [], console: [log({ id: 2, text: 'b' })], cursor: 2 },
      1,
    );
    expect(answer.log.console.map((e) => e.id)).toEqual([1, 2]);
    expect(answer.log.cursor).toBe(2);
  });
});

describe('consoleRows', () => {
  const entries = [
    log({ id: 1, text: 'started' }),
    log({ id: 2, level: 'warn', text: 'slow image' }),
    log({ id: 3, level: 'error', source: 'exception', text: 'TypeError: price' }),
    log({ id: 4, level: 'error', text: 'again', count: 3 }),
  ];

  it('marks warnings and errors and says where an error came from, or how many times', () => {
    const rows = consoleRows(entries, ALL);
    expect(rows.map((r) => r.lv)).toEqual(['', 'warn', 'error', 'error']);
    expect(rows[2].cells[1]).toBe('exception');
    expect(rows[3].cells[1]).toBe('×3');
  });

  it('filters by the chip, by words and by what was cleared', () => {
    expect(consoleRows(entries, { ...ALL, type: 'error' }).map((r) => r.key)).toEqual(['3', '4']);
    expect(consoleRows(entries, { ...ALL, type: 'info' }).map((r) => r.key)).toEqual(['1']);
    expect(consoleRows(entries, { ...ALL, query: 'image' }).map((r) => r.key)).toEqual(['2']);
    expect(consoleRows(entries, { ...ALL, clearedAt: 3 }).map((r) => r.key)).toEqual(['4']);
  });
});

describe('networkRows', () => {
  const entries = [
    net({ id: 1, url: 'http://localhost:4200/', type: 'document' }),
    net({ id: 2, url: 'http://localhost:4200/api/tickets?event=812', type: 'fetch', status: 500, size: 300 }),
    net({ id: 3, url: 'https://cdn.example.com/a/font.woff2', type: 'font', thirdParty: true }),
    net({ id: 4, status: null, size: null, durationMs: null }),
  ];

  it('names the row by its file and calls a failed one an error', () => {
    const rows = networkRows(entries, ALL);
    expect(rows[1].cells[0]).toBe('tickets?event=812');
    expect(rows[1].lv).toBe('error');
    expect(rows[1].detail).toContain('GET http://localhost:4200/api/tickets?event=812 → 500');
    expect(rows[2].cells[0]).toBe('cdn.example.com/font.woff2');
    expect(rows[3].cells[1]).toBe('…');
  });

  it('filters by the group of the type', () => {
    expect(networkRows(entries, { ...ALL, type: 'fetch' }).map((r) => r.key)).toEqual(['2']);
    expect(networkRows(entries, { ...ALL, type: 'font' }).map((r) => r.key)).toEqual(['3']);
  });

  it('counts failed requests for the badge and sums the foot', () => {
    const state = { ...emptyLog('s'), network: entries, console: [] };
    expect(errorCount(state, 'network', 0)).toBe(1);
    expect(errorCount(state, 'network', 2)).toBe(0);
    expect(networkFoot(entries, 0)).toContain('4 requests');
    expect(networkFoot(entries, 4)).toBe('');
  });
});

describe('nameOf', () => {
  it('the root is a slash, the query stays', () => {
    expect(nameOf(net({ id: 1, url: 'http://localhost:4200/' }))).toBe('/');
    expect(nameOf(net({ id: 1, url: 'http://localhost:4200/a/b.css?v=2' }))).toBe('b.css?v=2');
  });
});

describe('sizeText', () => {
  it('bytes, kilobytes, megabytes', () => {
    expect(sizeText(null)).toBe('—');
    expect(sizeText(512)).toBe('512 B');
    expect(sizeText(4300)).toBe('4.2 KB');
    expect(sizeText(412 * 1024)).toBe('412 KB');
    expect(sizeText(3 * 1024 * 1024)).toBe('3 MB');
  });
});
