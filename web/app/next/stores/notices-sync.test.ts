// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from 'vitest';
import {
  dismissAllNotices,
  dismissNotice,
  noticesSnapshot,
  pushNotice,
  setNoticesPerson,
  type INotice,
} from './notices';
import { startNoticesSync, type INoticesStream } from './notices-sync';

const notice = (key: string): INotice => ({
  key,
  chatId: 'c1',
  kind: 'done',
  icon: 'x',
  title: 't',
  text: 'x',
  at: 1,
});

const settle = () => new Promise((r) => setTimeout(r, 10));

const setup = (items: object[] = []) => {
  const sent: Array<{ path: string; body: Record<string, unknown> }> = [];
  const fns: Record<string, (e: { data: string }) => void> = {};
  const fetchImpl = (async (url: string, init?: RequestInit) => {
    const path = new URL(url, 'http://x').pathname;
    if (init?.method === 'POST') sent.push({ path, body: JSON.parse(String(init.body)) });
    return new Response(JSON.stringify({ items, reads: [], settings: { rev: 0 } }), { status: 200 });
  }) as unknown as typeof fetch;
  const stream = (): INoticesStream => ({
    addEventListener: (t, fn) => void (fns[t] = fn),
    close: () => undefined,
  });
  const emit = (type: string, body: object, from = 'B') =>
    fns[type]?.({ data: JSON.stringify({ type, from, ...body }) });
  return { sent, fetchImpl, stream, emit };
};

describe('notices sync (HRN-1421)', () => {
  beforeEach(() => {
    localStorage.clear();
    setNoticesPerson('p');
    dismissAllNotices();
  });

  it('a base with the machine in its query keeps the paths before the query (no ?machine=X/items?person=)', async () => {
    const urls: string[] = [];
    const s = setup();
    const fetchImpl = (async (url: string, init?: RequestInit) => {
      urls.push(url);
      return s.fetchImpl(url, init);
    }) as unknown as typeof fetch;
    const streams: string[] = [];
    const stop = startNoticesSync({
      base: '/n?machine=m1',
      person: 'p@x',
      fetchImpl,
      stream: (url) => (streams.push(url), s.stream()),
      device: 'A',
    });
    await settle();
    stop();
    expect(urls[0]).toBe('/n?machine=m1&person=p%40x');
    expect(streams).toEqual(['/n/stream?machine=m1&person=p%40x']);
  });

  it('loads what the database holds, and a notice read on another device goes from this one', async () => {
    const n = notice('k1');
    const s = setup([{ key: 'k1', chatId: 'c1', at: 1, doc: n }]);
    const stop = startNoticesSync({
      base: '/n',
      person: 'p',
      fetchImpl: s.fetchImpl,
      stream: s.stream,
      device: 'A',
    });
    await settle();
    expect(noticesSnapshot().map((x) => x.key)).toEqual(['k1']);
    s.emit('read', { keys: ['k1'] });
    expect(noticesSnapshot()).toEqual([]);
    expect(s.sent.filter((x) => x.path.endsWith('/read'))).toEqual([]); // not sent back
    stop();
  });

  it('sends a new notice and a read made here; a notice of another device appears', async () => {
    const s = setup();
    const stop = startNoticesSync({
      base: '/n',
      person: 'p',
      fetchImpl: s.fetchImpl,
      stream: s.stream,
      device: 'A',
    });
    await settle();
    pushNotice(notice('k2'));
    await settle();
    dismissNotice('k2');
    await settle();
    expect(s.sent.map((x) => x.path.replace('/n', ''))).toEqual(['/items', '/read']);
    expect(s.sent[1]?.body.keys).toEqual(['k2']);
    s.emit('new', { item: { key: 'k3', chatId: 'c', at: 2, doc: notice('k3') } });
    expect(noticesSnapshot().map((x) => x.key)).toEqual(['k3']);
    stop();
  });
});
