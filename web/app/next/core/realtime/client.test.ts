import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { hub } from '@/next/core/realtime/hub';
import { BACKOFF_MS, DEAD_MS, STALE_MS } from '@/next/core/realtime/client';
import { fixture, FakeRuntime, type FakeSocket } from '@/next/core/testing/fake-runtime';
import { bootFake, settle, useFakeTime } from '@/next/core/testing/setup';
import { connectionStore, secondsToRetry } from '@/next/stores/connection';
import { entryStore, feedStore, key, turnStore } from '@/next/stores/conversation';
import { conversationsStore } from '@/next/stores/conversations';
import type { ClientFrame, ConvEvent, ConvId, Entry } from '@/next/protocol/v2';

const CONV = 'tab-k3Jx9QmZ' as ConvId;
const CH = `conv:${CONV}` as const;
const TURN = 'c:3f1d2a9e-7c41-4b8e-9a51-0d6c2b7e8f10#0';

const ev = <T = ConvEvent>(name: string) => fixture<{ e: T }>(name).e;

/** A new step of the live turn with an id the snapshot does not have. */
const step = (n: number): ConvEvent => {
  const e = ev<Extract<ConvEvent, { type: 'step' }>>('ev.step');
  return { ...e, entry: { ...e.entry, id: `x:${30000 + n}#0`, at: e.entry.at + n } as Entry, counts: null };
};

const conn = (m: string) => connectionStore.get(m)!;
const subs = (s: FakeSocket) => s.sent.filter((f): f is Extract<ClientFrame, { f: 'sub' }> => f.f === 'sub');
const lastSocket = (rt: FakeRuntime) => rt.opened[rt.opened.length - 1];
const turnEntries = () => turnStore.get(key(CONV, TURN))?.entryIds ?? [];

beforeEach(useFakeTime);
afterEach(() => {
  hub.stop();
  vi.useRealTimers();
});

describe('RealtimeClient', () => {
  it('says hello with the device and subscribes machine and convs on welcome', async () => {
    const { rt, m } = await bootFake();
    const s = lastSocket(rt);
    expect(s.sent[0]).toMatchObject({ f: 'hello', proto: 2, app: 'next/0.1.0' });
    expect((s.sent[0] as { device: string }).device).toMatch(/^dev-/);
    expect(
      subs(s)
        .map((f) => f.ch)
        .sort(),
    ).toEqual(['convs', 'machine']);
    expect(conn(m).k).toBe('live');
    expect(conversationsStore.size).toBe(10);
  });

  it('goes live → stale → offline → live with the feed kept, resuming by afterSeq', async () => {
    const { rt, m } = await bootFake();
    const release = hub.openConversation(m, CONV);
    await settle();
    rt.emit(CH, step(1));
    expect(turnEntries()).toContain('x:30001#0');
    const kept = entryStore.get(key(CONV, 'x:30001#0'));
    const rowsBefore = feedStore.get(CONV)!.rows;

    rt.silence();
    await vi.advanceTimersByTimeAsync(STALE_MS);
    expect(conn(m).k).toBe('stale');

    rt.down = true;
    rt.emit(CH, step(2)); // the page misses these
    rt.emit(CH, step(3));
    await vi.advanceTimersByTimeAsync(DEAD_MS - STALE_MS);
    await settle();
    const off = conn(m);
    expect(off.k).toBe('offline');
    expect(secondsToRetry(off, Date.now())).toBe(BACKOFF_MS[0] / 1000);

    rt.down = false;
    await vi.advanceTimersByTimeAsync(BACKOFF_MS[0]);
    await settle();
    expect(conn(m).k).toBe('live');

    const resume = subs(lastSocket(rt)).find((f) => f.ch === CH)!;
    expect(resume).toMatchObject({ epoch: rt.epoch, afterSeq: 1 });
    expect(turnEntries()).toEqual(expect.arrayContaining(['x:30001#0', 'x:30002#0', 'x:30003#0']));
    // No reload: the objects already on the page are the same objects.
    expect(entryStore.get(key(CONV, 'x:30001#0'))).toBe(kept);
    expect(feedStore.get(CONV)!.rows).toBe(rowsBefore);
    release();
  });

  it('backs off 3 → 6 → 12 → 20 → 20 s with ±20 % jitter, and welcome resets it', async () => {
    const rt = new FakeRuntime();
    rt.down = true;
    const { m } = await bootFake({ rt });
    const delays: number[] = [];
    for (let i = 0; i < 5; i += 1) {
      const c = conn(m);
      expect(c.k).toBe('offline');
      const wait = (c as { retryAt: number }).retryAt - Date.now();
      delays.push(wait);
      await vi.advanceTimersByTimeAsync(wait);
      await settle();
    }
    expect(delays).toEqual([3000, 6000, 12000, 20000, 20000]);

    rt.down = false;
    await vi.advanceTimersByTimeAsync((conn(m) as { retryAt: number }).retryAt - Date.now());
    await settle();
    expect(conn(m).k).toBe('live');
    rt.down = true;
    rt.dropAll();
    await settle();
    expect((conn(m) as { retryAt: number }).retryAt - Date.now()).toBe(3000);
  });

  it('jitters within ±20 %', async () => {
    const rt = new FakeRuntime();
    rt.down = true;
    const { m } = await bootFake({ rt, start: false });
    hub.configure({ socket: rt.socket, random: () => 0, probe: async () => 'network' });
    hub.start([m], m);
    await settle();
    expect((conn(m) as { retryAt: number }).retryAt - Date.now()).toBe(2400);
  });

  it('reconnects at once on online, on visible and on «Сейчас»', async () => {
    const rt = new FakeRuntime();
    rt.down = true;
    const { m, env } = await bootFake({ rt });
    expect(conn(m).k).toBe('offline');
    rt.down = false;
    env.fire('online');
    await settle();
    expect(conn(m).k).toBe('live');

    rt.down = true;
    rt.dropAll();
    await settle();
    rt.down = false;
    env.fire('visible');
    await settle();
    expect(conn(m).k).toBe('live');

    rt.down = true;
    rt.dropAll();
    await settle();
    rt.down = false;
    hub.reconnectNow(m);
    await settle();
    expect(conn(m).k).toBe('live');
  });

  it('after a restart resumes by afterEntryId: a snapshot after the cursor, rows kept', async () => {
    const { rt, m } = await bootFake();
    hub.openConversation(m, CONV);
    await settle();
    const before = entryStore.get(key(CONV, 'c:3f1d2a9e-7c41-4b8e-9a51-0d6c2b7e8f11#0'));
    rt.restart('ep-after-restart');
    await vi.advanceTimersByTimeAsync(BACKOFF_MS[0]);
    await settle();
    const resume = subs(lastSocket(rt)).find((f) => f.ch === CH)!;
    expect(resume.afterEntryId).toBe(rt.lastEntryId(CONV));
    expect(resume.epoch).toBe('ep-01JB7Q0000');
    expect(entryStore.get(key(CONV, 'c:3f1d2a9e-7c41-4b8e-9a51-0d6c2b7e8f11#0'))).toBe(before);
    // Events of the new epoch apply from the snapshot's seq on.
    rt.emit(CH, step(9));
    expect(turnEntries()).toContain('x:30009#0');
  });

  it('takes a full snapshot when the runtime does not know the cursor, merging by id', async () => {
    const { rt, m } = await bootFake();
    hub.openConversation(m, CONV);
    await settle();
    const before = entryStore.get(key(CONV, 'c:3f1d2a9e-7c41-4b8e-9a51-0d6c2b7e8f11#0'));
    // The ring lost everything and the transcript has no such entry: only a replace can answer.
    rt.convs.get(CONV)!.entries = rt.convs.get(CONV)!.entries.filter((e) => e.id !== rt.lastEntryId(CONV));
    rt.ringSize = 0;
    rt.emit(CH, step(1));
    rt.emit(CH, step(2));
    rt.restart('ep-other');
    await vi.advanceTimersByTimeAsync(BACKOFF_MS[0]);
    await settle();
    expect(conn(m).k).toBe('live');
    expect(entryStore.get(key(CONV, 'c:3f1d2a9e-7c41-4b8e-9a51-0d6c2b7e8f11#0'))).toBe(before);
    expect(feedStore.get(CONV)!.source).toBe('live');
  });

  it('drops events at or below the snapshot seq and re-subscribes on a gap', async () => {
    const { rt, m } = await bootFake();
    rt.ring(CH).seq = 40;
    hub.openConversation(m, CONV);
    await settle();
    const s = lastSocket(rt);
    // At or below the snapshot: already in it.
    s.deliver({ f: 'ev', ch: CH, seq: 40, e: step(1) });
    expect(turnEntries()).not.toContain('x:30001#0');
    // A gap: 41 never came. The page does not apply 42 out of order; it asks again from 40, and the runtime —
    // its ring lacks 41 — answers with a snapshot that has the step.
    rt.skip(CH);
    const delivered: string[] = [];
    const on = s.onmessage!;
    s.onmessage = (m) => {
      delivered.push(JSON.parse(String(m.data)).f);
      on(m);
    };
    rt.emit(CH, step(2));
    const resub = subs(s).filter((f) => f.ch === CH);
    expect(resub).toHaveLength(2);
    expect(resub[1]).toMatchObject({ epoch: rt.epoch, afterSeq: 40 });
    expect(delivered).toEqual(['ev', 'snapshot']);
    expect(turnEntries()).toContain('x:30002#0');
  });

  it('stops retrying on unauthorized', async () => {
    const { rt, m } = await bootFake();
    lastSocket(rt).deliver({ f: 'error', code: 'unauthorized' });
    await settle();
    const c = conn(m);
    expect(c).toMatchObject({ k: 'offline', cause: 'unauthorized' });
    expect(secondsToRetry(c, Date.now())).toBeNull();
    const opened = rt.opened.length;
    await vi.advanceTimersByTimeAsync(60000);
    expect(rt.opened.length).toBe(opened);
  });

  it('pings every 15 s and stays live while the runtime answers', async () => {
    const { rt, m } = await bootFake();
    await vi.advanceTimersByTimeAsync(61000);
    expect(lastSocket(rt).sent.filter((f) => f.f === 'ping').length).toBe(4);
    expect(conn(m).k).toBe('live');
    expect(rt.opened.length).toBe(1);
  });

  it('counts holders of a channel: the last release unsubscribes', async () => {
    const { rt, m } = await bootFake();
    const a = hub.openConversation(m, CONV);
    const b = hub.openConversation(m, CONV);
    await settle();
    const s = lastSocket(rt);
    expect(subs(s).filter((f) => f.ch === CH)).toHaveLength(1);
    a();
    expect(s.sent.some((f) => f.f === 'unsub')).toBe(false);
    b();
    expect(s.sent.filter((f) => f.f === 'unsub')).toEqual([{ f: 'unsub', ch: CH }]);
  });
});
