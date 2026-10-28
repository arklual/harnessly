import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { storeSink } from '@/next/core/realtime/apply';
import { hub } from '@/next/core/realtime/hub';
import { fixture, fixtureNames } from '@/next/core/testing/fake-runtime';
import { bootFake, settle, useFakeTime } from '@/next/core/testing/setup';
import { applyMessage, convMessagesStore, messagesStore, putLocal } from '@/next/stores/messages';
import {
  backgroundStore,
  entryStore,
  feedStore,
  gatesStore,
  key,
  nowStore,
  queueStore,
  turnStore,
} from '@/next/stores/conversation';
import { conversationsStore } from '@/next/stores/conversations';
import { favoritesStore } from '@/next/stores/favorites';
import { defaultCwdStore, projectsStore } from '@/next/stores/projects';
import type {
  ClientId,
  ConvChannel,
  ConvEvent,
  ConvId,
  ConvSnapshot,
  ConvsEvent,
  Event,
  EvFrame,
  Message,
} from '@/next/protocol/v2';

const CONV = 'tab-k3Jx9QmZ' as ConvId;
const CH = `conv:${CONV}` as const;
const TURN = 'c:3f1d2a9e-7c41-4b8e-9a51-0d6c2b7e8f10#0';
const ev = <T = ConvEvent>(name: string) => fixture<{ e: T }>(name).e;

beforeEach(useFakeTime);
afterEach(() => {
  hub.stop();
  vi.useRealTimers();
});

async function openChat() {
  const boot = await bootFake();
  hub.openConversation(boot.m, CONV);
  await settle();
  return boot;
}

describe('apply', () => {
  it('takes every event fixture of the contract; the same event again changes nothing', async () => {
    const { rt } = await openChat();
    const names = fixtureNames().filter((n) => n.startsWith('ev.'));
    expect(names.length).toBeGreaterThanOrEqual(30);
    const snap = () => [
      feedStore.get(CONV),
      turnStore.get(key(CONV, TURN)),
      queueStore.get(CONV),
      backgroundStore.get(CONV),
      gatesStore.get(CONV),
      nowStore.get(CONV),
      ...conversationsStore.values(),
      ...messagesStore.values(),
      ...[...entryStore.keys()].map((k) => entryStore.get(k)),
    ];
    for (const n of names) {
      const f = fixture<EvFrame>(n);
      const ch = f.ch === 'machine' || f.ch === 'convs' ? f.ch : CH;
      rt.emit(ch, f.e as Event);
      const once = snap();
      rt.emit(ch, f.e as Event);
      const twice = snap();
      expect(twice.length, n).toBe(once.length);
      twice.forEach((v, i) => expect(v, `${n} [${i}]`).toBe(once[i]));
    }
  });

  it('replaces the live reply in place and clears «Сейчас» at the end of its turn', async () => {
    const { rt } = await openChat();
    expect(nowStore.get(CONV)?.turnId).toBe(TURN);
    const ids = turnStore.get(key(CONV, TURN))!.entryIds;
    const at = ids.indexOf(`live:${TURN}`);
    rt.emit(CH, ev('ev.entry_replace'));
    const next = turnStore.get(key(CONV, TURN))!.entryIds;
    expect(next[at]).toBe('c:7d1e2f3a-4b5c-4d6e-8f70-8192a3b4c5d6#0');
    expect(entryStore.has(key(CONV, `live:${TURN}`))).toBe(false);
    rt.emit(CH, ev('ev.turn_finished'));
    expect(nowStore.get(CONV)).toBeUndefined();
    // A late now of a finished turn does not bring it back.
    rt.emit(CH, ev('ev.now'));
    expect(nowStore.get(CONV)).toBeUndefined();
  });

  it('applies conversation records by rev only', async () => {
    const { rt } = await openChat();
    const st = ev<Extract<ConvsEvent, { type: 'conversation_state' }>>('ev.conversation_state');
    const before = conversationsStore.get(st.id)!;
    rt.emit('convs', { ...st, rev: before.rev - 1 });
    expect(conversationsStore.get(st.id)).toBe(before);
    rt.emit('convs', { ...st, rev: before.rev + 1 });
    expect(conversationsStore.get(st.id)!.state).toEqual(st.state);
  });

  it('makes a terminal quota marker visible before the coalesced conversation upsert', async () => {
    const { rt } = await openChat();
    const failure = ev<Extract<ConvEvent, { type: 'quota_exhausted' }>>('ev.quota_exhausted');
    expect(conversationsStore.get(CONV)?.terminalFailure).toBeUndefined();
    rt.emit(CH, failure);
    expect(conversationsStore.get(CONV)?.terminalFailure).toEqual(failure.failure);
  });
});

describe('a conversation belongs to the machine of its channel', () => {
  const OTHER = 'laptop';

  it('does not let another machine overwrite, patch or delete its record', async () => {
    const { rt, m } = await openChat();
    const up = ev<Extract<ConvsEvent, { type: 'conversation_upsert' }>>('ev.conversation_upsert');
    const mine = conversationsStore.get(up.conv.id)!;
    expect(mine.machineId).toBe(m);
    const forged = { ...up.conv, title: 'forged', rev: mine.rev + 10 };
    storeSink.event(OTHER, { ch: 'convs', e: { type: 'conversation_upsert', conv: forged } } as never);
    storeSink.event(OTHER, {
      ch: 'convs',
      e: {
        type: 'conversation_state',
        id: mine.id,
        state: { k: 'stopped', since: 1, resumable: false },
        rev: mine.rev + 11,
      },
    } as never);
    storeSink.event(OTHER, { ch: 'convs', e: { type: 'conversation_removed', id: mine.id } } as never);
    expect(conversationsStore.get(mine.id)).toBe(mine);
    // The snapshot of the other machine does not drop the record either, nor take it over.
    storeSink.snapshot(OTHER, {
      ch: 'convs',
      data: { conversations: [forged], projects: [], defaultCwd: '/' },
    } as never);
    expect(conversationsStore.get(mine.id)).toBe(mine);
    rt.emit('convs', up);
    expect(conversationsStore.get(mine.id)).toBe(mine);
  });

  it('stores a record under the machine of the channel that brought it', async () => {
    await openChat();
    const up = ev<Extract<ConvsEvent, { type: 'conversation_upsert' }>>('ev.conversation_upsert');
    const fresh = { ...up.conv, id: 'tab-newOne01' as ConvId, machineId: 'someone-else' };
    storeSink.event(OTHER, { ch: 'convs', e: { type: 'conversation_upsert', conv: fresh } } as never);
    expect(conversationsStore.get(fresh.id)?.machineId).toBe(OTHER);
  });

  it('ignores the conversation channel of a machine that does not own it', async () => {
    const { rt } = await openChat();
    const feed = feedStore.get(CONV)!;
    const turn = turnStore.get(key(CONV, TURN))!;
    storeSink.event(OTHER, { ch: CH, e: ev('ev.turn_finished') } as never);
    storeSink.event(OTHER, { ch: CH, e: ev('ev.queue_changed') } as never);
    expect(feedStore.get(CONV)).toBe(feed);
    expect(turnStore.get(key(CONV, TURN))).toBe(turn);
    expect(nowStore.get(CONV)?.turnId).toBe(TURN);
    // The owner still gets through.
    rt.emit(CH, ev('ev.turn_finished'));
    expect(nowStore.get(CONV)).toBeUndefined();
  });

  it('ignores prompt_state and message_upsert of another machine for a message it does not own', async () => {
    const { rt, m } = await openChat();
    const msg = ev<{ type: 'message_upsert'; message: Message }>('ev.message_upsert').message;
    rt.emit(CH, { type: 'message_upsert', message: msg });
    const mine = messagesStore.get(msg.clientId)!;
    expect(mine.machineId).toBe(m);
    storeSink.event(OTHER, {
      ch: CH,
      e: { type: 'message_upsert', message: { ...msg, text: 'forged', rev: msg.rev + 5 } },
    } as never);
    storeSink.event(OTHER, {
      ch: CH,
      e: { type: 'prompt_state', clientId: msg.clientId, state: { k: 'cancelled' }, rev: msg.rev + 6 },
    } as never);
    expect(messagesStore.get(msg.clientId)).toBe(mine);
  });
});

describe('projects come with the convs channel', () => {
  it('the snapshot lists them with the default folder, project_upsert adds one, a later snapshot drops the gone', () => {
    const M = 'm-projects' as never;
    const p = (id: string, name: string) => ({
      id,
      machineId: 'x',
      path: `/w/${name}`,
      name,
      git: true,
      createdAt: 1,
    });
    storeSink.snapshot(M, {
      ch: 'convs',
      data: { conversations: [], projects: [p('p1', 'b'), p('p2', 'a')], defaultCwd: '/w' },
    } as never);
    expect(projectsStore.get('p1')?.machineId).toBe(M);
    expect(defaultCwdStore.get(M)).toBe('/w');
    storeSink.event(M, { ch: 'convs', e: { type: 'project_upsert', project: p('p3', 'c') } } as never);
    expect(projectsStore.get('p3')?.name).toBe('c');
    storeSink.snapshot(M, {
      ch: 'convs',
      data: { conversations: [], projects: [p('p3', 'c')], defaultCwd: '/w' },
    } as never);
    expect(projectsStore.get('p1')).toBeUndefined();
    expect(projectsStore.get('p3')).toBeDefined();
  });
});

describe('the favorites layout comes with the convs channel', () => {
  it('the snapshot carries it, favorites_upsert replaces it whole, a snapshot without one clears it', () => {
    const M = 'm-favorites' as never;
    const layout = (chats: string[]) => ({
      groups: [{ id: 'fg-aaaa', name: 'Релизы', chats }],
      loose: ['tab-b'],
    });
    storeSink.snapshot(M, {
      ch: 'convs',
      data: { conversations: [], projects: [], defaultCwd: '/w', favorites: layout(['tab-a']) },
    } as never);
    expect(favoritesStore.get(M)?.groups[0]?.chats).toEqual(['tab-a']);
    storeSink.event(M, {
      ch: 'convs',
      e: { type: 'favorites_upsert', favorites: layout(['tab-a', 'tab-c']) },
    } as never);
    expect(favoritesStore.get(M)?.groups[0]?.chats).toEqual(['tab-a', 'tab-c']);
    storeSink.snapshot(M, {
      ch: 'convs',
      data: { conversations: [], projects: [], defaultCwd: '/w' },
    } as never);
    expect(favoritesStore.get(M)).toBeUndefined();
  });
});

describe('a snapshot settles the messages that ended while the page was away', () => {
  // The shape of the four messages that stood in the queue for hours on prod (tab-dBSEBV): sent with pictures while the
  // agent was busy, kept in the browser's chat cache as queued{agent-busy}, answered meanwhile. The runtime had them as
  // done, but its snapshot lists the finished ones of the last ten minutes only, so no frame ever named them again.
  const stuck = (clientId: string, text: string, images: number, rev: number): Message => ({
    clientId: clientId as ClientId,
    conversationId: CONV,
    text,
    attachments: Array.from({ length: images }, (_, i) => ({
      id: `up_X6BYL6XCIC2LTT${i}L`,
      name: `CleanShot 2026-10-25 at 5.16.3${i} AM@2x.png`,
      kind: 'image' as const,
      mime: 'image/png',
    })),
    mode: 'queue',
    kind: 'prompt',
    createdAt: 1792894190386,
    attempts: 0,
    state: { k: 'queued', position: 1, reason: 'agent-busy', wait: null },
    rev,
  });

  it('forgets a not-over message of the runtime the snapshot no longer lists, and keeps the rest', async () => {
    const { m } = await openChat();
    const memory = stuck(
      '01a170d2-4226-7110-a953-4ec030b1c0cc',
      'И ещё, у меня почти полностью закончилась память',
      1,
      4,
    );
    const settings = stuck(
      '01a170db-b45b-769c-a4ab-0236cf95cf75',
      'Давай сделаем что Базово настройки',
      4,
      5,
    );
    const listed = stuck('01a1722f-9735-7006-ad75-bffbd3983ccd', 'А почему это застряло', 1, 3);
    for (const msg of [memory, settings, listed]) applyMessage(m, msg);
    // Written on this page, the runtime has not had it yet: it is the page's own and waits for its answer.
    putLocal({
      ...messagesStore.get(listed.clientId)!,
      clientId: '01a17230-0000-7000-8000-000000000001' as ClientId,
      rev: 0,
      state: { k: 'submitting' },
    });
    const conv = fixture<{ ch: ConvChannel; data: ConvSnapshot }>('snapshot.conv');
    storeSink.snapshot(m, { ch: CH, mode: 'replace', data: { ...conv.data, messages: [listed] } } as never);
    expect(messagesStore.get(memory.clientId)).toBeUndefined();
    expect(messagesStore.get(settings.clientId)).toBeUndefined();
    expect(messagesStore.get(listed.clientId)?.state.k).toBe('queued');
    expect(messagesStore.get('01a17230-0000-7000-8000-000000000001' as ClientId)?.state.k).toBe('submitting');
    const left = convMessagesStore.get(CONV) ?? [];
    expect(left).not.toContain(memory.clientId);
    expect(left).not.toContain(settings.clientId);
    expect(left).toContain(listed.clientId);
  });

  it('leaves the messages of another machine alone', async () => {
    const { m } = await openChat();
    const memory = stuck(
      '01a170d2-4226-7110-a953-4ec030b1c0cc',
      'И ещё, у меня почти полностью закончилась память',
      1,
      4,
    );
    applyMessage(m, memory);
    const conv = fixture<{ ch: ConvChannel; data: ConvSnapshot }>('snapshot.conv');
    storeSink.snapshot(
      'laptop' as never,
      { ch: CH, mode: 'replace', data: { ...conv.data, messages: [] } } as never,
    );
    expect(messagesStore.get(memory.clientId)?.state.k).toBe('queued');
  });
});
