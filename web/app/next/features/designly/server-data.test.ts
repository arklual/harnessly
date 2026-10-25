import { beforeAll, describe, expect, it } from 'vitest';
import type { INetworkEntry } from '@/next/core/net/preview-devtools';
import {
  allServerRows,
  lastAt,
  noSources,
  readCheck,
  readPageLog,
  serverErrors,
  serverRows,
  type ICheckReport,
} from '@/next/features/designly/server-data';
import { pinLocale } from '@/next/i18n';

beforeAll(() => pinLocale('en'));

const net = (over: Partial<INetworkEntry> & { id: number }): INetworkEntry => ({
  at: 1000,
  method: 'GET',
  url: 'http://127.0.0.1:4800/a/shop/p/Main.dc.html',
  host: '127.0.0.1:4800',
  type: 'document',
  status: 200,
  size: 2048,
  durationMs: 38,
  thirdParty: false,
  blocked: false,
  error: null,
  ...over,
});

const ALL = { clearedAt: 0, type: 'all', query: '' };
const BOARD = 'Main.dc.html';

const check: ICheckReport = {
  startedAt: 5000,
  finishedAt: 6000,
  failure: null,
  boards: [
    {
      path: BOARD,
      status: 'error',
      shots: [
        {
          view: 'desktop',
          status: 'error',
          errors: ['TypeError: price\n  at x'],
          failedRequests: ['GET /api/p 500'],
        },
        { view: 'phone', status: 'blank', errors: [], failedRequests: [] },
      ],
    },
    {
      path: 'Other.dc.html',
      status: 'error',
      shots: [{ view: 'desktop', status: 'error', errors: ['not ours'], failedRequests: [] }],
    },
  ],
};

describe('allServerRows', () => {
  it('has nothing for a runtime that recorded nothing', () => {
    expect(allServerRows(noSources(), BOARD)).toEqual([]);
  });

  it("shows the board's requests to the runtime like a server log, and leaves out the requests to other sites and the library script's own channels", () => {
    const rows = allServerRows(
      {
        ...noSources(),
        requests: [
          net({ id: 1 }),
          net({ id: 2, url: 'http://127.0.0.1:4800/api/x?y=1', status: 500, durationMs: 312 }),
          net({ id: 3, url: 'https://cdn.example.com/f.woff2', thirdParty: true }),
          net({ id: 4, url: 'http://127.0.0.1:4800/__lib/events?a=shop', status: null, error: 'reset' }),
          net({ id: 5, url: 'http://127.0.0.1:4800/__tk/bridge.js' }),
        ],
      },
      BOARD,
    );
    expect(rows.map((r) => r.cells[1])).toEqual([
      'GET /a/shop/p/Main.dc.html → 200 · 38 ms',
      'GET /api/x?y=1 → 500 · 312 ms',
    ]);
    expect(rows.map((r) => r.lv)).toEqual(['', 'error']);
    expect(rows[0].cells[0]).toMatch(/^\d\d:\d\d:\d\d$/);
  });

  it("takes the page's log by its kinds, and names the page when it is another board", () => {
    const rows = allServerRows(
      {
        ...noSources(),
        pageLog: [
          { at: 2000, kind: 'error', page: 'Main.dc.html', message: 'TypeError: x\n    at a.js:1' },
          { at: 3000, kind: 'slow', page: 'Other%20one.dc.html', message: 'Load 4000 ms' },
          { at: 4000, kind: 'longtask', page: 'Main.dc.html?v=1', message: 'Main thread busy 300 ms' },
        ],
      },
      BOARD,
    );
    expect(rows.map((r) => [r.lv, r.cells[1]])).toEqual([
      ['error', 'TypeError: x'],
      ['warn', 'Load 4000 ms (Other one.dc.html)'],
      ['warn', 'Main thread busy 300 ms'],
    ]);
    expect(rows[0].detail).toContain('at a.js:1');
  });

  it("takes the last check's problems on this board only, at the time the check finished", () => {
    const rows = allServerRows({ ...noSources(), check }, BOARD);
    expect(rows.map((r) => [r.lv, r.cells[1]])).toEqual([
      ['error', 'check · desktop: TypeError: price'],
      ['error', 'check · desktop: GET /api/p 500'],
      ['warn', 'check · phone: the page is empty'],
    ]);
    expect(rows.every((r) => r.at === 6000)).toBe(true);
    expect(
      allServerRows({ ...noSources(), check: { ...check, failure: 'no browser', boards: [] } }, BOARD)[0]
        .cells[1],
    ).toBe('check · no browser');
  });

  it('orders what comes from different places by time', () => {
    const rows = allServerRows(
      {
        requests: [net({ id: 1, at: 3000 })],
        pageLog: [{ at: 1000, kind: 'error', page: BOARD, message: 'first' }],
        check: { ...check, boards: [], failure: 'last', finishedAt: 9000 },
      },
      BOARD,
    );
    expect(rows.map((r) => r.at)).toEqual([1000, 3000, 9000]);
  });
});

describe('serverRows and the badge', () => {
  const rows = allServerRows(
    {
      requests: [net({ id: 1, at: 1000 }), net({ id: 2, at: 2000, status: 404 })],
      pageLog: [],
      check: null,
    },
    BOARD,
  );

  it('filters by the chip and by words', () => {
    expect(serverRows(rows, { ...ALL, type: 'error' })).toHaveLength(1);
    expect(serverRows(rows, { ...ALL, query: 'main.dc' })).toHaveLength(2);
    expect(serverRows(rows, { ...ALL, query: 'nope' })).toHaveLength(0);
  });

  it('what was cleared stays hidden, and what comes after it shows', () => {
    const cleared = lastAt(rows);
    expect(serverRows(rows, { ...ALL, clearedAt: cleared })).toHaveLength(0);
    expect(serverErrors(rows, 0)).toBe(1);
    expect(serverErrors(rows, cleared)).toBe(0);
  });
});

describe('what the runtime answers', () => {
  it('reads a log with a malformed entry left out', () => {
    expect(readPageLog([{ at: 1, kind: 'error', page: 'a', message: 'm' }, 7, null])).toEqual([
      { at: 1, kind: 'error', page: 'a', message: 'm' },
    ]);
    expect(readPageLog(null)).toEqual([]);
  });

  it('reads a report whose empty lists came as null, and a design never checked as null', () => {
    const report = readCheck({
      startedAt: 1,
      finishedAt: null,
      failure: null,
      boards: [
        {
          path: 'a',
          status: 'ok',
          shots: [{ view: 'desktop', status: 'ok', errors: null, failedRequests: null }],
        },
      ],
    });
    expect(report?.boards[0].shots[0]).toEqual({
      view: 'desktop',
      status: 'ok',
      errors: [],
      failedRequests: [],
    });
    expect(report?.finishedAt).toBeNull();
    expect(readCheck(null)).toBeNull();
  });
});
