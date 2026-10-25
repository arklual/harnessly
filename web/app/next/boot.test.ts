import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { sendMessage } from '@/next/commands';
import { memoryKV, startPersistence, type KV } from '@/next/core/persist';
import { hub } from '@/next/core/realtime/hub';
import { FakeRuntime } from '@/next/core/testing/fake-runtime';
import { resetCore } from '@/next/core/testing/reset';
import { bootFake, fakeEnv, settle, useFakeTime } from '@/next/core/testing/setup';
import { bootNext, shutdownNext } from '@/next/boot';
import { connectionStore } from '@/next/stores/connection';
import { convsLoadedStore } from '@/next/stores/conversations';
import { machinesStore } from '@/next/stores/machines';
import { draftsStore, setDraft } from '@/next/stores/drafts';
import { localMeSource, meStore, type MeSource } from '@/next/stores/me';
import { messagesStore } from '@/next/stores/messages';
import type { ConvId } from '@/next/protocol/v2';

beforeEach(useFakeTime);
afterEach(() => {
  void shutdownNext();
  hub.stop();
  vi.useRealTimers();
});

describe('bootNext', () => {
  it('me → machines → hello → a socket per v2 machine', async () => {
    const rt = new FakeRuntime();
    await bootFake({ rt, start: false });
    const booting = bootNext({
      kv: () => memoryKV(),
      lastPerson: null,
      hub: { socket: rt.socket, env: fakeEnv(), random: () => 0.5, probe: async () => 'network' },
    });
    await settle();
    await vi.advanceTimersByTimeAsync(300);
    const st = await booting;
    await settle();
    expect(st).toEqual({ k: 'ready', machines: [rt.machineId] });
    // No /api/v2/me yet (stage E): the local stand-in answers.
    expect(meStore.get().k).toBe('ready');
    expect(machinesStore.get(rt.machineId)?.runtime).toBe('v2');
    expect(connectionStore.get(rt.machineId)?.k).toBe('live');
    expect(convsLoadedStore.has(rt.machineId)).toBe(true);
  });

  const bootWithHello = async (hello: FakeRuntime['hello']) => {
    const rt = new FakeRuntime();
    rt.hello = hello;
    await bootFake({ rt, start: false });
    const booting = bootNext({
      kv: () => memoryKV(),
      lastPerson: null,
      hub: { socket: rt.socket, env: fakeEnv(), random: () => 0.5, probe: async () => 'network' },
    });
    await settle();
    await vi.advanceTimersByTimeAsync(300);
    return { rt, st: await booting };
  };

  it('a machine that did not answer /hello is not «outdated»: it stays and its socket tries', async () => {
    const { rt, st } = await bootWithHello('network');
    expect(st).toEqual({ k: 'ready', machines: [rt.machineId] });
    // the socket's welcome then tells what the runtime is
    expect(machinesStore.get(rt.machineId)?.runtime).toBe('v2');
  });

  it('a machine that answers /hello as an older runtime is «outdated» and not started', async () => {
    const { rt, st } = await bootWithHello('older');
    expect(st).toEqual({ k: 'ready', machines: [] });
    expect(machinesStore.get(rt.machineId)?.runtime).toBe('outdated');
  });
});

const CONV = 'tab-k3Jx9QmZ' as ConvId;
const bootOpts = (rt: FakeRuntime, kv: (person: string) => KV, me: MeSource, lastPerson: string | null) => ({
  kv,
  me,
  lastPerson,
  hub: { socket: rt.socket, env: fakeEnv(), random: () => 0.5, probe: async () => 'network' as const },
});

/** Person a closed the page with a draft and a message that never got its answer. */
async function leaveUnsent(kvs: Map<string, ReturnType<typeof memoryKV>>) {
  const rt = new FakeRuntime();
  const { m } = await bootFake({ rt });
  const kv = memoryKV();
  kvs.set('a@x.ru', kv);
  const p = startPersistence(kv, { person: 'a@x.ru' });
  await p.ready;
  setDraft(CONV, { text: 'черновик А', attachments: [] });
  rt.hang = true;
  const id = sendMessage(m, { conversationId: CONV, text: 'письмо А' });
  await vi.advanceTimersByTimeAsync(400);
  await p.flushNow();
  // The page is closed while the send has no answer; here its attempt ends at cmdTimeoutMs.
  await vi.advanceTimersByTimeAsync(12000);
  p.stop();
  hub.stop();
  resetCore();
  return id;
}

describe('whose data the page keeps', () => {
  it('starts the outbox only after me, and only for the person it was written for', async () => {
    const kvs = new Map<string, ReturnType<typeof memoryKV>>();
    const id = await leaveUnsent(kvs);
    const rt = new FakeRuntime();
    await bootFake({ rt, start: false });
    const openKV = (person: string) => {
      if (!kvs.has(person)) kvs.set(person, memoryKV());
      return kvs.get(person)!;
    };
    let release!: (s: Awaited<ReturnType<MeSource['load']>>) => void;
    const me: MeSource = {
      load: () => new Promise((r) => (release = r)),
      patch: async () => ({ r: 'failed' }),
    };
    const booting = bootNext(bootOpts(rt, openKV, me, 'a@x.ru'));
    await vi.advanceTimersByTimeAsync(400);
    // The cache of the last person draws, but nothing of their outbox is back or sent while me is unknown.
    expect(draftsStore.get(CONV)?.text).toBe('черновик А');
    expect(messagesStore.has(id)).toBe(false);
    expect(rt.commands).toHaveLength(0);
    release(await localMeSource({ email: 'a@x.ru' }).load());
    await vi.advanceTimersByTimeAsync(400);
    await booting;
    await settle();
    expect(messagesStore.get(id)).toBeDefined();
    expect(rt.commands.filter((c) => c.op === 'send' && c.body.opId === id).length).toBeGreaterThanOrEqual(1);
  });

  it('is a clean start for another person: no draft, no outbox, nothing sent in the old name', async () => {
    const kvs = new Map<string, ReturnType<typeof memoryKV>>();
    const id = await leaveUnsent(kvs);
    const rt = new FakeRuntime();
    await bootFake({ rt, start: false });
    const opened: string[] = [];
    const openKV = (person: string) => {
      opened.push(person);
      if (!kvs.has(person)) kvs.set(person, memoryKV());
      return kvs.get(person)!;
    };
    const booting = bootNext(bootOpts(rt, openKV, localMeSource({ email: 'b@x.ru' }), 'a@x.ru'));
    await vi.advanceTimersByTimeAsync(400);
    await booting;
    await settle();
    expect(opened).toEqual(['a@x.ru', 'b@x.ru']);
    expect(draftsStore.has(CONV)).toBe(false);
    expect(messagesStore.has(id)).toBe(false);
    expect(rt.commands.filter((c) => c.op === 'send')).toHaveLength(0);
    // Person a's own database still has their message for the next time they sign in.
    expect(kvs.get('a@x.ru')!.dump().outbox.has(id)).toBe(true);
    expect(kvs.get('b@x.ru')!.dump().outbox.size).toBe(0);
  });

  it('empties the stores when the page lets the person go', async () => {
    const rt = new FakeRuntime();
    await bootFake({ rt, start: false });
    const booting = bootNext(bootOpts(rt, () => memoryKV(), localMeSource({ email: 'a@x.ru' }), null));
    await vi.advanceTimersByTimeAsync(400);
    await booting;
    await settle();
    setDraft(CONV, { text: 'x', attachments: [] });
    expect(machinesStore.size).toBeGreaterThan(0);
    await shutdownNext();
    expect(draftsStore.size).toBe(0);
    expect(machinesStore.size).toBe(0);
    expect(meStore.get().k).toBe('loading');
  });
});
