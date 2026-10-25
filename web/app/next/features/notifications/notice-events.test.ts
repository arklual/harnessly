import { describe, expect, it } from 'vitest';
import { fixture } from '@/next/core/testing/fake-runtime';
import { noticeEventOf } from '@/next/features/notifications/notice-events';
import type { Conversation } from '@/next/protocol/v2';

const base = fixture<{ e: { conv: Conversation } }>('ev.conversation_upsert').e.conv;
const turn = (patch: Partial<NonNullable<Conversation['lastTurn']>>) => ({
  id: 'u-1',
  status: 'done' as const,
  startedAt: 100,
  endedAt: 200,
  durationMs: 100,
  toolCount: 0,
  snippet: 'Сборка зелёная',
  ...patch,
});
const conv = (patch: Partial<Conversation>): Conversation => ({
  ...base,
  state: { k: 'ready', since: 200 },
  gate: null,
  seenAt: 0,
  lastTurn: null,
  bgAgents: undefined,
  ...patch,
});

describe('what in a chat makes a notice', () => {
  it('is nothing for a chat at work or read', () => {
    expect(
      noticeEventOf(conv({ state: { k: 'working', since: 1, turnId: null, compacting: false } }), true),
    ).toBe(null);
    expect(noticeEventOf(conv({ lastTurn: turn({}), seenAt: 300 }), true)).toBe(null);
  });

  it('is a question that waits, keyed by when it began to wait', () => {
    const event = noticeEventOf(conv({ state: { k: 'waiting-question', since: 50, questionId: 'q' } }), true);
    expect(event).toMatchObject({ kind: 'waiting', key: `${base.id}:question:50` });
  });

  it('is a finished unread turn with the start of its answer', () => {
    const event = noticeEventOf(conv({ lastTurn: turn({}) }), true);
    expect(event).toMatchObject({ kind: 'done', key: `${base.id}:u-1:done`, text: 'Сборка зелёная' });
  });

  it('tells a limit from another failure', () => {
    const limited = noticeEventOf(
      conv({ lastTurn: turn({ status: 'failed', failure: 'rate-limit' }) }),
      true,
    );
    const broken = noticeEventOf(conv({ lastTurn: turn({ status: 'failed', failure: 'api-error' }) }), true);
    expect(limited?.kind).toBe('limit');
    expect(broken?.kind).toBe('failed');
    expect(limited?.key).not.toBe(broken?.key);
  });
});
