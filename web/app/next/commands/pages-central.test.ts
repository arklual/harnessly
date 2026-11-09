import { beforeEach, describe, expect, it, vi } from 'vitest';

// HRN-879, spec v10: the history of a chat (older pages, a turn's steps) is read from the cluster's copy whatever the
// machine does; the machine is asked only for what the copy does not have yet, and never when it is off.

const net = vi.hoisted(() => ({ getPage: vi.fn() }));
vi.mock('@/next/core/net/http', () => ({ getPage: net.getPage, sendBody: vi.fn(), sendPage: vi.fn() }));
vi.mock('@/next/core/net/machine', () => ({
  gatewayPath: (p: string) => `/gw${p}`,
  machineApi: (m: string) => ({ http: (p: string) => `/m/${m}${p}` }),
}));

import { loadEntries, loadOlderTurns, loadOutput, loadRewound, searchMessages } from '@/next/commands/pages';
import { centralStore } from '@/next/stores/central';
import { connectionStore } from '@/next/stores/connection';

const page = {
  turns: [],
  entries: [],
  beforeTurnId: null,
  conversation: {},
  machineId: 'a',
  machine: 'online',
};
const urls = () => net.getPage.mock.calls.map((c) => c[0] as string);

const keep = (off: string[] = []) =>
  centralStore.set({
    enabled: true,
    off: new Set(off),
    moved: new Set(),
    names: new Map(),
    on: new Set(['a']),
  });

describe('a chat history from the cluster copy', () => {
  beforeEach(() => {
    net.getPage.mockReset();
    for (const m of [...connectionStore.keys()]) connectionStore.delete(m);
  });

  it('reads older turns from the copy while the cluster still calls the machine online', async () => {
    keep();
    connectionStore.set('a', { k: 'stale' } as never);
    net.getPage.mockResolvedValue({ ok: true, value: page });
    const res = await loadOlderTurns('a', 'tab-1', 't5');
    expect('ok' in res).toBe(true);
    expect(urls()).toEqual(['/gw/history/conversations/tab-1?limit=20&before=t5']);
  });

  it('asks a machine that is there for a chat the copy does not have yet', async () => {
    keep();
    connectionStore.set('a', { k: 'live' } as never);
    net.getPage
      .mockResolvedValueOnce({ r: 'failed', code: 'http', status: 404 })
      .mockResolvedValueOnce({ ok: true, value: { turns: [], entries: [], beforeTurnId: null } });
    const res = await loadOlderTurns('a', 'tab-1', 't5');
    expect('ok' in res).toBe(true);
    expect(urls()[1]).toBe('/m/a/conversations/tab-1/turns?before=t5&limit=20');
  });

  it('does not ask a machine that is off', async () => {
    keep(['a']);
    net.getPage.mockResolvedValue({ r: 'failed', code: 'http', status: 404 });
    const res = await loadOlderTurns('a', 'tab-1', 't5');
    expect('ok' in res).toBe(false);
    expect(urls()).toHaveLength(1);
  });

  it('reads a turn steps from the copy, and the turn under way from the machine', async () => {
    keep();
    connectionStore.set('a', { k: 'live' } as never);
    const step = { t: 'think', id: 'e1', turnId: 't1', at: 1, rev: 1, text: 'x' };
    net.getPage.mockResolvedValueOnce({ ok: true, value: { turnId: 't1', entries: [step] } });
    await loadEntries('a', 'tab-1', 't1');
    expect(urls()).toEqual(['/gw/history/conversations/tab-1/turns/t1/entries']);

    net.getPage.mockReset();
    net.getPage
      .mockResolvedValueOnce({ ok: true, value: { turnId: 't9', entries: [] } })
      .mockResolvedValueOnce({ ok: true, value: { turnId: 't9', entries: [step] } });
    await loadEntries('a', 'tab-1', 't9');
    expect(urls()).toEqual([
      '/gw/history/conversations/tab-1/turns/t9/entries',
      '/m/a/conversations/tab-1/turns/t9/entries',
    ]);
  });

  it('goes to the machine as before when the copy is off for the person', async () => {
    centralStore.set({ enabled: false, off: new Set(), moved: new Set(), names: new Map(), on: new Set() });
    connectionStore.set('a', { k: 'live' } as never);
    net.getPage.mockResolvedValue({ ok: true, value: { turns: [], entries: [], beforeTurnId: null } });
    await loadOlderTurns('a', 'tab-1', 't5');
    expect(urls()).toEqual(['/m/a/conversations/tab-1/turns?before=t5&limit=20']);
  });

  it('asks a machine that is there for turns older than what the copy keeps of a chat it does not hold whole', async () => {
    keep();
    connectionStore.set('a', { k: 'live' } as never);
    net.getPage
      .mockResolvedValueOnce({ ok: true, value: { ...page, complete: false } })
      .mockResolvedValueOnce({ ok: true, value: { turns: [], entries: [], beforeTurnId: null } });
    await loadOlderTurns('a', 'tab-1', 't0');
    expect(urls()[1]).toBe('/m/a/conversations/tab-1/turns?before=t0&limit=20');
  });

  it('reads a tool whole output from the copy, and from the machine only what the copy has not whole yet', async () => {
    keep();
    connectionStore.set('a', { k: 'live' } as never);
    const whole = { entryId: 'e1', output: { text: 'all of it', truncated: null } };
    net.getPage.mockResolvedValueOnce({ ok: true, value: whole });
    expect(await loadOutput('a', 'tab-1', 'e1')).toEqual({ ok: true, value: whole });
    expect(urls()).toEqual(['/gw/history/conversations/tab-1/entries/e1/output']);

    net.getPage.mockReset();
    const head = { entryId: 'e2', output: { text: 'the head', truncated: { shown: 8, total: 9000 } } };
    net.getPage
      .mockResolvedValueOnce({ ok: true, value: head })
      .mockResolvedValueOnce({ ok: true, value: whole });
    await loadOutput('a', 'tab-1', 'e2');
    expect(urls()).toEqual([
      '/gw/history/conversations/tab-1/entries/e2/output',
      '/m/a/conversations/tab-1/entries/e2/output',
    ]);

    // The machine is off: what the copy has is all there is, and the machine is not asked.
    keep(['a']);
    connectionStore.set('a', { k: 'stale' } as never);
    net.getPage.mockReset();
    net.getPage.mockResolvedValueOnce({ ok: true, value: head });
    expect(await loadOutput('a', 'tab-1', 'e2')).toEqual({ ok: true, value: head });
    expect(urls()).toHaveLength(1);
  });

  it('ends the feed at what the copy keeps while the machine is off', async () => {
    keep(['a']);
    net.getPage.mockResolvedValue({ ok: true, value: { ...page, complete: false } });
    const res = await loadOlderTurns('a', 'tab-1', 't0');
    expect('ok' in res).toBe(true);
    expect(urls()).toHaveLength(1);
  });

  it('searches the copy, and the machine only when the copy finds nothing and the machine is there', async () => {
    keep();
    connectionStore.set('a', { k: 'live' } as never);
    const hit = { convId: 'tab-1', entryId: 'e1', turnId: 't1', at: 1, role: 'user', snippet: 'ёлка' };
    net.getPage.mockResolvedValueOnce({ ok: true, value: { query: 'ёлка', hits: [hit], indexing: false } });
    await searchMessages('a', 'ёлка', 30);
    expect(urls()).toEqual([`/gw/history/search?q=${encodeURIComponent('ёлка')}&limit=30&machine=a`]);

    net.getPage.mockReset();
    net.getPage
      .mockResolvedValueOnce({ ok: true, value: { query: 'x', hits: [], indexing: false } })
      .mockResolvedValueOnce({ ok: true, value: { query: 'x', hits: [hit], indexing: false } });
    await searchMessages('a', 'x', 30);
    expect(urls()[1]).toBe('/m/a/search?q=x&limit=30');

    net.getPage.mockReset();
    keep(['a']);
    connectionStore.set('a', { k: 'stale' } as never);
    net.getPage.mockResolvedValueOnce({ ok: true, value: { query: 'x', hits: [], indexing: false } });
    await searchMessages('a', 'x', 30);
    expect(urls()).toHaveLength(1);
  });

  it('reads the turns a rewind left behind from the copy, with the machine off too', async () => {
    keep(['a']);
    net.getPage.mockResolvedValueOnce({
      ok: true,
      value: { turns: [{ id: 't2' }], entries: [], beforeTurnId: null },
    });
    await loadRewound('a', 'tab-1', 0);
    expect(urls()).toEqual(['/gw/history/conversations/tab-1/rewinds/0/turns']);
  });
});
