import { beforeEach, describe, expect, it, vi } from 'vitest';

// HRN-879: a chat opened on a machine whose chats the cluster keeps is drawn from the copy, and the machine's channel goes
// on from the copy's newest entry (a snapshot "after"), not from nothing (its whole tail).

const net = vi.hoisted(() => ({ getPage: vi.fn() }));
vi.mock('@/next/core/net/http', () => ({ getPage: net.getPage, sendBody: vi.fn(), sendPage: vi.fn() }));
vi.mock('@/next/core/net/machine', () => ({
  gatewayPath: (p: string) => `/gw${p}`,
  machineApi: (m: string) => ({ http: (p: string) => `/m/${m}${p}` }),
}));

import { noteCentralCursor, waitsForCentral } from '@/next/core/realtime/central-first';
import { forgetResume, noteLastEntry, resumeOf } from '@/next/core/realtime/cursors';
import { centralStore, loadCentralChat } from '@/next/stores/central';
import { connectionStore } from '@/next/stores/connection';
import { machinesStore } from '@/next/stores/machines';

const keep = (off: string[] = []) =>
  centralStore.set({
    enabled: true,
    off: new Set(off),
    moved: new Set(),
    names: new Map(),
    on: new Set(['a']),
  });
const entry = (id: string) => ({ t: 'say', id, turnId: 't1', at: 1, rev: 1, text: 'x' });
const chat = (ids: string[]) => ({
  ok: true,
  value: {
    turns: [],
    entries: ids.map(entry),
    beforeTurnId: null,
    conversation: {},
    machineId: 'a',
    machine: 'online',
    complete: true,
  },
});

describe('the machine channel of a chat the copy keeps', () => {
  beforeEach(() => {
    net.getPage.mockReset();
    forgetResume('a', 'conv:tab-1');
    for (const m of [...connectionStore.keys()]) connectionStore.delete(m);
    machinesStore.set('a', { features: ['snapshot-after'] } as never);
  });

  it('goes on from the copy newest entry', async () => {
    keep();
    connectionStore.set('a', { k: 'live' } as never);
    expect(waitsForCentral('a', 'tab-1')).toBe(true);
    net.getPage.mockResolvedValue(chat(['c:e1#0', 'c:e2#0', 'live:x']));
    await noteCentralCursor('a', 'tab-1');
    expect(resumeOf('a', 'conv:tab-1').lastEntryId).toBe('c:e2#0');
    expect(waitsForCentral('a', 'tab-1')).toBe(false);
  });

  it('does not wait for a runtime whose snapshot after an entry leaves out the turns it does not touch', () => {
    keep();
    machinesStore.set('a', { features: [] } as never);
    expect(waitsForCentral('a', 'tab-1')).toBe(false);
  });

  it('does not wait when the page keeps the chat or the copy does not keep the machine', () => {
    keep();
    noteLastEntry('a', 'conv:tab-1', 'c:e9#0');
    expect(waitsForCentral('a', 'tab-1')).toBe(false);
    forgetResume('a', 'conv:tab-1');
    centralStore.set({ enabled: true, off: new Set(), moved: new Set(), names: new Map(), on: new Set() });
    expect(waitsForCentral('a', 'tab-1')).toBe(false);
  });

  it('notes nothing for a machine that is off, a copy with nothing, or a copy that does not answer in time', async () => {
    keep(['a']);
    net.getPage.mockResolvedValue(chat(['c:e1#0']));
    await noteCentralCursor('a', 'tab-1');
    expect(resumeOf('a', 'conv:tab-1').lastEntryId).toBeNull();

    keep();
    connectionStore.set('a', { k: 'live' } as never);
    net.getPage.mockResolvedValue(chat([]));
    await noteCentralCursor('a', 'tab-1');
    expect(resumeOf('a', 'conv:tab-1').lastEntryId).toBeNull();

    net.getPage.mockReturnValue(new Promise(() => {}));
    await noteCentralCursor('a', 'tab-1', 10);
    expect(resumeOf('a', 'conv:tab-1').lastEntryId).toBeNull();
  });

  it('asks the copy once for the chat newest page its feed and its channel both want', async () => {
    net.getPage.mockResolvedValue(chat(['c:e1#0']));
    await Promise.all([loadCentralChat('tab-2'), loadCentralChat('tab-2')]);
    expect(net.getPage).toHaveBeenCalledTimes(1);
    await loadCentralChat('tab-2', 'c:t1#0');
    expect(net.getPage).toHaveBeenCalledTimes(2);
  });
});
