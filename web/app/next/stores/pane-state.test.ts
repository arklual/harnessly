// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  capKept,
  chatPaneOf,
  chatPanes,
  EMPTY_PANE,
  MAX_CHATS,
  MAX_PENDING,
  PANE_STATE_KEY,
  parseKept,
  pruneKept,
  queueRequest,
  resetChatPanes,
  serializeKept,
  updateKept,
  type IChatPane,
  type IKeptPane,
  type TKept,
} from '@/next/stores/pane-state';
import type { TTab } from '@/next/features/pane/pane-tabs';

const BROWSER: TTab = {
  id: 'p1',
  kind: 'browser',
  hist: [{ target: { kind: 'port', port: 4400 }, path: '/', text: 'localhost:4400' }],
  hi: 0,
};
const FILES: TTab = { id: 'p2', kind: 'files', path: 'src/app', file: false };
const DESIGN: TTab = { id: 'p3', kind: 'designly', design: 'afisha' };
const SHELL: TTab = { id: 'p4', kind: 'shell', terminal: 'term-7' };

const pane = (patch: Partial<IChatPane> = {}): IChatPane => ({ ...EMPTY_PANE, ...patch });
const kept = (machine: string | null, at: number, patch: Partial<IChatPane> = {}): IKeptPane => ({
  ...pane(patch),
  machine,
  at,
});

beforeEach(() => {
  localStorage.clear();
  resetChatPanes();
});
afterEach(() => {
  vi.useRealTimers();
  resetChatPanes();
});

describe('saved panes', () => {
  it('a chat with nothing kept has the closed empty pane', () => {
    expect(chatPaneOf('c1')).toBe(EMPTY_PANE);
    expect(chatPaneOf(null)).toBe(EMPTY_PANE);
  });

  it('comes back as it was written: open, width, tabs in order, the one in front, each place', () => {
    const before: TKept = new Map([
      [
        'c1',
        kept('m1', 5, {
          open: true,
          width: 620,
          maxed: true,
          tabs: [FILES, BROWSER, DESIGN, SHELL],
          active: 'p2',
          unread: ['p3'],
          view: { 'site:afisha:tab': 't1' },
        }),
      ],
    ]);
    const after = parseKept(serializeKept(before));
    expect(after.get('c1')).toEqual(before.get('c1'));
  });

  it('reads nothing from another version, broken JSON or none', () => {
    expect(parseKept(null).size).toBe(0);
    expect(parseKept('{').size).toBe(0);
    expect(parseKept(JSON.stringify({ v: 99, chats: { c1: kept('m1', 1) } })).size).toBe(0);
  });

  it('leaves out tabs that are not ours and keeps the rest', () => {
    const raw = JSON.stringify({
      v: 1,
      chats: {
        c1: {
          ...kept('m1', 1, { tabs: [FILES], active: 'p2', unread: ['p2', 'gone'] }),
          tabs: [FILES, { id: 'x', kind: 'nope' }, { id: 'y', kind: 'browser', hist: 'no', hi: 0 }],
        },
        c2: { tabs: 'broken' },
      },
    });
    const read = parseKept(raw);
    expect(read.get('c1')?.tabs).toEqual([FILES]);
    expect(read.get('c1')?.unread).toEqual(['p2']);
    expect(read.has('c2')).toBe(false);
  });

  it('keeps the most recent chats only', () => {
    const many: TKept = new Map(
      Array.from({ length: MAX_CHATS + 5 }, (_, i) => [`c${i}`, kept('m1', i)] as const),
    );
    const capped = capKept(many);
    expect(capped.size).toBe(MAX_CHATS);
    expect(capped.has('c0')).toBe(false);
    expect(capped.has(`c${MAX_CHATS + 4}`)).toBe(true);
    expect(parseKept(serializeKept(many)).size).toBe(MAX_CHATS);
  });
});

describe('updating a chat', () => {
  it('touches only that chat and makes it the most recent', () => {
    const start: TKept = new Map([['c1', kept('m1', 1, { open: true, tabs: [FILES], active: 'p2' })]]);
    const next = updateKept(
      start,
      'c2',
      'm1',
      (p) => ({ ...p, open: true, tabs: [BROWSER], active: 'p1' }),
      9,
    );
    expect(next.get('c1')).toBe(start.get('c1'));
    expect(next.get('c2')).toMatchObject({ open: true, active: 'p1', machine: 'm1', at: 9 });
  });

  it('is the same map when nothing changed', () => {
    const start: TKept = new Map([['c1', kept('m1', 1)]]);
    expect(updateKept(start, 'c1', 'm1', (p) => p, 5)).toBe(start);
    expect(updateKept(start, 'c9', 'm1', (p) => p, 5)).toBe(start);
  });
});

describe('forgetting deleted chats', () => {
  const start: TKept = new Map([
    ['c1', kept('m1', 1)],
    ['c2', kept('m1', 2)],
    ['c3', kept('m2', 3)],
  ]);

  it('drops a chat its machine no longer lists', () => {
    const next = pruneKept(start, { listed: (m) => m === 'm1', exists: (c) => c === 'c1' });
    expect([...next.keys()]).toEqual(['c1', 'c3']);
  });

  it('waits for the machine’s own list before dropping anything', () => {
    expect(pruneKept(start, { listed: () => false, exists: () => false })).toBe(start);
  });

  it('is the same map when every chat is still there', () => {
    expect(pruneKept(start, { listed: () => true, exists: () => true })).toBe(start);
  });
});

describe('requests for a chat that is not on screen', () => {
  it('wait in order', () => {
    const one = queueRequest(EMPTY_PANE, { k: 'browser', target: 'localhost:4400', reload: false });
    const two = queueRequest(one, { k: 'legacy', kind: 'gitlab', target: '!12', reload: false });
    expect(two.pending.map((x) => x.k)).toEqual(['browser', 'legacy']);
    expect(EMPTY_PANE.pending).toEqual([]);
  });

  it('keep the newest few', () => {
    let p: IChatPane = EMPTY_PANE;
    for (let i = 0; i < MAX_PENDING + 3; i += 1)
      p = queueRequest(p, { k: 'files', target: `/w/f${i}.ts`, reload: false });
    expect(p.pending).toHaveLength(MAX_PENDING);
    expect(p.pending[0]).toMatchObject({ target: '/w/f3.ts' });
  });

  it('do not change the pane of the chat on screen', () => {
    chatPanes.update('shown', 'm1', (p) => ({ ...p, open: true, tabs: [FILES], active: 'p2' }));
    chatPanes.queue('away', 'm1', { k: 'browser', target: 'example.com', reload: false });
    expect(chatPaneOf('shown')).toMatchObject({ open: true, active: 'p2', pending: [] });
    expect(chatPaneOf('away')).toMatchObject({ open: false, tabs: [] });
    expect(chatPaneOf('away').pending).toHaveLength(1);
  });

  it('survive a reload', () => {
    chatPanes.queue('away', 'm1', { k: 'design', target: 'afisha', reload: false });
    chatPanes.flush();
    resetChatPanes();
    expect(chatPaneOf('away').pending).toEqual([{ k: 'design', target: 'afisha', reload: false }]);
  });
});

describe('the store', () => {
  it('writes a moment after a change and reads it back on the next page', () => {
    vi.useFakeTimers();
    chatPanes.update('c1', 'm1', (p) => ({ ...p, open: true, width: 700, tabs: [BROWSER], active: 'p1' }));
    chatPanes.update('c2', 'm1', (p) => ({ ...p, open: false, tabs: [FILES], active: 'p2' }));
    expect(localStorage.getItem(PANE_STATE_KEY)).toBeNull();
    vi.advanceTimersByTime(400);
    expect(localStorage.getItem(PANE_STATE_KEY)).not.toBeNull();
    resetChatPanes();
    expect(chatPaneOf('c1')).toMatchObject({ open: true, width: 700, active: 'p1' });
    expect(chatPaneOf('c2')).toMatchObject({ open: false, active: 'p2' });
  });

  it('keeps working when the storage throws', () => {
    const get = vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('denied');
    });
    const set = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('full');
    });
    chatPanes.update('c1', 'm1', (p) => ({ ...p, open: true }));
    chatPanes.flush();
    expect(chatPaneOf('c1').open).toBe(true);
    get.mockRestore();
    set.mockRestore();
  });

  it('prunes through the store', () => {
    chatPanes.update('c1', 'm1', (p) => ({ ...p, open: true }));
    chatPanes.update('c2', 'm1', (p) => ({ ...p, open: true }));
    chatPanes.prune({ listed: () => true, exists: (c) => c === 'c2' });
    expect(chatPaneOf('c1')).toBe(EMPTY_PANE);
    expect(chatPaneOf('c2').open).toBe(true);
  });
});
