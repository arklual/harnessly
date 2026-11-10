// HRN-905: a finished turn the snapshot cut (its newest steps only) says it holds part of its steps, so unfolded it
// loads the rest; the transcript's interrupt line is no step («Остановлено» is said under the turn).
import { afterEach, describe, expect, it } from 'vitest';
import { ItemsBuilder } from '@/next/bindings/feed-items';
import type { TurnItem } from '@/next/features/chat/types';
import { applyConvSnapshot, dropConversationData } from '@/next/stores/conversation';
import type { Entry, Turn } from '@/next/protocol/v2';

const CONV = 'tab-905';
const TURN = 'c:1#0';

afterEach(() => dropConversationData(CONV));

const done = (stepCount: number, toolCount: number, status: Turn['status'] = 'done'): Turn => ({
  id: TURN,
  author: { provider: 'claude', role: null },
  startedAt: 1,
  endedAt: 9,
  durationMs: 8,
  toolCount,
  stepCount,
  status,
  failure: null,
  answerEntryId: 'c:5#0',
  files: [],
});

const prompt: Entry = {
  t: 'user',
  id: TURN,
  turnId: TURN,
  at: 1,
  rev: 1,
  text: 'go',
  attachments: [],
  clientId: null,
};
const said: Entry = { t: 'say', id: 'c:2#0', turnId: TURN, at: 2, rev: 1, md: 'Looking at the feed first.' };
const ran: Entry = {
  t: 'tool',
  id: 'c:3#0',
  turnId: TURN,
  at: 3,
  rev: 1,
  toolUseId: 'toolu_1',
  tool: 'Bash',
  summary: '$ ls',
  ok: true,
  output: { text: 'a.txt', truncated: null },
};
const stopped: Entry = { t: 'interrupt', id: 'c:4#0', turnId: TURN, at: 4, rev: 1 };
const answer: Entry = { t: 'say', id: 'c:5#0', turnId: TURN, at: 5, rev: 1, md: 'Done.' };

const turnOf = (t: Turn, entries: Entry[]): TurnItem => {
  applyConvSnapshot(
    CONV,
    'local',
    'replace',
    {
      turns: [t],
      entries,
      beforeTurnId: null,
      now: null,
      queue: { conversationId: CONV, items: [], paused: null, rev: 1 },
      questions: [],
      permissions: [],
      background: [],
      messages: [],
    },
    1,
  );
  const item = new ItemsBuilder(CONV, 'local').snapshot()?.find((x) => x.kind === 'turn');
  if (item?.kind !== 'turn') throw new Error('no turn');
  return item;
};

describe('the steps a finished turn holds', () => {
  it('a turn the snapshot cut to its newest steps is partial: unfolded, the rest loads', () => {
    const item = turnOf(done(3, 1), [prompt, ran, answer]);
    expect(item.steps?.map((e) => e.id)).toEqual(['c:3#0']);
    expect(item.partial).toBe(true);
  });

  it('a turn that holds every step is not', () => {
    const item = turnOf(done(3, 1), [prompt, said, ran, answer]);
    expect(item.steps?.map((e) => e.id)).toEqual(['c:2#0', 'c:3#0']);
    expect(item.partial).toBeUndefined();
  });

  it('a turn that holds none waits for its page, as before', () => {
    const item = turnOf(done(3, 1), [prompt, answer]);
    expect(item.steps).toBeNull();
    expect(item.partial).toBeUndefined();
  });

  it('a stopped turn keeps its thoughts and words, and the interrupt line is not one of its steps', () => {
    const item = turnOf(done(4, 1, 'interrupted'), [prompt, said, ran, stopped, answer]);
    expect(item.steps?.map((e) => e.id)).toEqual(['c:2#0', 'c:3#0']);
    expect(item.partial).toBeUndefined();
    expect(item.answer?.md).toBe('Done.');
  });
});
