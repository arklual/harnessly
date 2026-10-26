import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { findChats, chatsByTitle } from '@/next/features/composer/chat-sources';
import { conversationsStore } from '@/next/stores/conversations';
import { flush } from '@/next/core/store';
import { setFetch } from '@/next/core/net/http';
import type { Conversation } from '@/next/protocol/v2';

const conv = (id: `tab-${string}`, title: string, lastActivityAt: number, machineId = 'm1'): Conversation =>
  ({ id, title, lastActivityAt, machineId }) as unknown as Conversation;

beforeEach(() => {
  for (const c of [
    conv('tab-1', 'Релиз 1.80: чеклист', 30),
    conv('tab-2', 'Ёлка на главной', 20),
    conv('tab-3', 'Database moves', 10),
    conv('tab-4', 'Other machine release', 40, 'm2'),
  ])
    conversationsStore.set(c.id, c);
  flush();
});
afterEach(() => {
  for (const k of [...conversationsStore.keys()]) conversationsStore.delete(k);
  flush();
  vi.restoreAllMocks();
});

describe('chatsByTitle', () => {
  it('lists the chats of the machine whose title has every word, the newest first, without the open one', () => {
    expect(chatsByTitle('m1', null, '').map((p) => p.ref.id)).toEqual(['tab-1', 'tab-2', 'tab-3']);
    expect(chatsByTitle('m1', 'tab-1', '').map((p) => p.ref.id)).toEqual(['tab-2', 'tab-3']);
    expect(chatsByTitle('m1', null, 'релиз чек').map((p) => p.ref.id)).toEqual(['tab-1']);
  });

  it('reads «ё» as «е» and ignores case', () => {
    expect(chatsByTitle('m1', null, 'елка').map((p) => p.ref.id)).toEqual(['tab-2']);
    expect(chatsByTitle('m1', null, 'DATABASE').map((p) => p.ref.id)).toEqual(['tab-3']);
  });

  it('makes a chat reference of each', () => {
    expect(chatsByTitle('m1', null, 'database')[0].ref).toEqual({
      kind: 'chat',
      id: 'tab-3',
      title: 'Database moves',
      target: '',
    });
  });
});

describe('findChats', () => {
  const answer = (hits: unknown[]) =>
    setFetch(() =>
      Promise.resolve(
        new Response(JSON.stringify({ query: 'q', hits, indexing: false }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    );

  it('adds the chats whose messages hold the words after the ones whose title does', async () => {
    answer([
      {
        convId: 'tab-3',
        entryId: 'c:00000000-0000-4000-8000-000000000001#0',
        turnId: null,
        at: 5,
        role: 'agent',
        snippet: 'the release went out',
      },
      {
        convId: 'tab-1',
        entryId: 'c:00000000-0000-4000-8000-000000000002#0',
        turnId: null,
        at: 4,
        role: 'user',
        snippet: 'release again',
      },
    ]);
    const picks = await findChats('m1', null, 'releas', new AbortController().signal);
    expect(picks.map((p) => [p.ref.id, p.snippet])).toEqual([
      ['tab-3', 'the release went out'],
      ['tab-1', 'release again'],
    ]);
  });

  it('puts a chat found by its title first, and once', async () => {
    answer([
      {
        convId: 'tab-3',
        entryId: 'c:00000000-0000-4000-8000-000000000001#0',
        turnId: null,
        at: 5,
        role: 'agent',
        snippet: 'the database was moved',
      },
      {
        convId: 'tab-1',
        entryId: 'c:00000000-0000-4000-8000-000000000002#0',
        turnId: null,
        at: 4,
        role: 'user',
        snippet: 'the database again',
      },
    ]);
    const picks = await findChats('m1', null, 'database', new AbortController().signal);
    expect(picks.map((p) => [p.ref.id, p.snippet])).toEqual([
      ['tab-3', null],
      ['tab-1', 'the database again'],
    ]);
  });

  it('keeps the titles when the machine cannot search', async () => {
    setFetch(() => Promise.resolve(new Response('{}', { status: 404 })));
    const picks = await findChats('m1', null, 'database', new AbortController().signal);
    expect(picks.map((p) => p.ref.id)).toEqual(['tab-3']);
  });

  it('asks the machine for nothing under two letters', async () => {
    const fetched = vi.fn();
    setFetch((...args) => {
      fetched(...args);
      return Promise.resolve(new Response('{}', { status: 404 }));
    });
    const picks = await findChats('m1', null, 'd', new AbortController().signal);
    expect(fetched).not.toHaveBeenCalled();
    expect(picks.length).toBeGreaterThan(0);
  });
});
