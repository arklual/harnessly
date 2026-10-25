import { afterEach, describe, expect, it } from 'vitest';
import {
  applyConvSnapshot,
  applyOlderTurns,
  dropConversationData,
  entryStore,
  feedStore,
  key,
  resetFeed,
  turnStore,
} from '@/next/stores/conversation';
import type { ConvSnapshot, Entry, Turn } from '@/next/protocol/v2';

const CONV = 'tab-reset';

afterEach(() => dropConversationData(CONV));

const turnOf = (id: string, startedAt: number): Turn => ({
  id,
  author: { provider: 'codex', role: null },
  startedAt,
  endedAt: startedAt + 5,
  durationMs: 5,
  toolCount: 1,
  stepCount: 2,
  status: 'done',
  failure: null,
  answerEntryId: null,
  files: [],
});

const prompt = (turnId: string, at: number, text: string): Entry => ({
  t: 'user',
  id: turnId,
  turnId,
  at,
  rev: 1,
  text,
  attachments: [],
  clientId: null,
});

const step = (turnId: string, id: string, at: number): Entry => ({
  t: 'think',
  id,
  turnId,
  at,
  rev: 1,
  text: 'step',
});

const snapshot = (turns: Turn[], entries: Entry[]): ConvSnapshot => ({
  turns,
  entries,
  beforeTurnId: null,
  now: null,
  queue: { conversationId: CONV, rev: 1, items: [], paused: null },
  questions: [],
  permissions: [],
  background: [],
  messages: [],
});

describe('the feed after a rewind', () => {
  it('keeps the turns both branches share with their steps, drops the rest and the older pages', () => {
    const a = turnOf('x:10#0', 10);
    const b = turnOf('x:90#0', 30);
    applyConvSnapshot(
      CONV,
      'local',
      'replace',
      snapshot([a, b], [prompt(a.id, 10, 'a'), step(a.id, 'x:20#0', 11), prompt(b.id, 30, 'b')]),
      1,
    );
    applyOlderTurns(CONV, [turnOf('x:1#0', 1)], [], 'x:1#0');
    // The new branch's turn has the place, and so the id, of the turn it replaced.
    const b2 = turnOf('x:90#0', 50);
    resetFeed(CONV, [a, b2], [prompt(a.id, 10, 'a'), prompt(b2.id, 50, 'b again')], null);

    const feed = feedStore.get(CONV)!;
    expect(feed.rows.map((r) => [r.id, r.at])).toEqual([
      ['x:10#0', 10],
      ['x:90#0', 50],
    ]);
    expect(feed.older).toBe(false);
    expect(turnStore.get(key(CONV, a.id))?.entryIds).toEqual([a.id, 'x:20#0']);
    expect(turnStore.get(key(CONV, b2.id))?.turn?.startedAt).toBe(50);
    expect(entryStore.get(key(CONV, b2.id))).toMatchObject({ text: 'b again' });
    expect(turnStore.has(key(CONV, 'x:1#0'))).toBe(false);
  });

  it('changes nothing when the same reset comes again', () => {
    const a = turnOf('x:10#0', 10);
    applyConvSnapshot(CONV, 'local', 'replace', snapshot([a], [prompt(a.id, 10, 'a')]), 1);
    resetFeed(CONV, [a], [prompt(a.id, 10, 'a')], null);
    const feed = feedStore.get(CONV);
    const view = turnStore.get(key(CONV, a.id));
    resetFeed(CONV, [a], [prompt(a.id, 10, 'a')], null);
    expect(feedStore.get(CONV)).toBe(feed);
    expect(turnStore.get(key(CONV, a.id))).toBe(view);
  });
});
