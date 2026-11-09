import { afterEach, describe, expect, it } from 'vitest';
import {
  applyConvSnapshot,
  dropConversationData,
  feedStore,
  importCentralFeed,
} from '@/next/stores/conversation';
import type { ConvSnapshot, Entry, Turn } from '@/next/protocol/v2';

// HRN-879, spec v10: the feed is drawn from the cluster's copy whatever the machine does; the machine's own snapshot
// has the last word.

const CONV = 'tab-central';

afterEach(() => dropConversationData(CONV));

const turnOf = (id: string, startedAt: number): Turn => ({
  id,
  author: { provider: 'claude', role: null },
  startedAt,
  endedAt: startedAt + 5,
  durationMs: 5,
  toolCount: 0,
  stepCount: 1,
  status: 'done',
  failure: null,
  answerEntryId: null,
  files: [],
});

const prompt = (turnId: string, at: number): Entry => ({
  t: 'user',
  id: turnId,
  turnId,
  at,
  rev: 1,
  text: turnId,
  attachments: [],
  clientId: null,
});

const snapshot = (turns: Turn[], entries: Entry[]): ConvSnapshot => ({
  turns,
  entries,
  beforeTurnId: null,
  queue: { conversationId: CONV, items: [], paused: null, rev: 1 },
  messages: [],
  questions: [],
  permissions: [],
  background: [],
  now: null,
});

describe('the feed from the cluster copy', () => {
  it('draws the copy at once, as kept, with where older turns are', () => {
    const drawn = importCentralFeed(CONV, 'a', {
      turns: [turnOf('t1', 10), turnOf('t2', 20)],
      entries: [prompt('t1', 10), prompt('t2', 20)],
      beforeTurnId: 't0',
    });
    expect(drawn).toBe(true);
    const f = feedStore.get(CONV)!;
    expect(f.loaded).toBe(true);
    expect(f.source).toBe('cached');
    expect(f.beforeTurnId).toBe('t0');
    expect(f.rows.filter((r) => r.k === 'turn').map((r) => r.id)).toEqual(['t1', 't2']);
  });

  it('never goes over what the machine said', () => {
    applyConvSnapshot(
      CONV,
      'a',
      'replace',
      snapshot([turnOf('t2', 20), turnOf('t3', 30)], [prompt('t2', 20), prompt('t3', 30)]),
      1,
    );
    const drawn = importCentralFeed(CONV, 'a', {
      turns: [turnOf('t1', 10)],
      entries: [prompt('t1', 10)],
      beforeTurnId: 't0',
    });
    expect(drawn).toBe(false);
    const f = feedStore.get(CONV)!;
    expect(f.source).toBe('live');
    expect(f.beforeTurnId).toBeNull();
    expect(f.rows.some((r) => r.id === 't1')).toBe(false);
  });

  it('gives way to the machine snapshot that comes after', () => {
    importCentralFeed(CONV, 'a', {
      turns: [turnOf('t1', 10)],
      entries: [prompt('t1', 10)],
      beforeTurnId: null,
    });
    applyConvSnapshot(
      CONV,
      'a',
      'replace',
      snapshot([turnOf('t1', 10), turnOf('t2', 20)], [prompt('t1', 10), prompt('t2', 20)]),
      1,
    );
    const f = feedStore.get(CONV)!;
    expect(f.source).toBe('live');
    expect(f.rows.filter((r) => r.k === 'turn').map((r) => r.id)).toEqual(['t1', 't2']);
  });
});
