import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { held } from '@/next/core/net/http';
import { connectionStore } from '@/next/stores/connection';
import {
  ACTIVE_POLL_MS,
  DONE_MS,
  HOLD_STEP_MS,
  ROLLOUT,
  rolloutStore,
  setRolloutFetch,
  stopRollout,
  watchRollout,
} from '@/next/features/rollout/rollout';
import type { MachineId } from '@/next/protocol/v2';

const json = (v: unknown, status = 200) =>
  new Response(JSON.stringify(v), { status, headers: { 'Content-Type': 'application/json' } });

let hub: () => Response | Promise<Response>;
const asked: string[] = [];

beforeEach(() => {
  vi.useFakeTimers();
  asked.length = 0;
  hub = () => json({ active: false });
  setRolloutFetch(async (url) => {
    asked.push(url);
    return hub();
  });
});
afterEach(() => {
  stopRollout();
  for (const m of [...connectionStore.keys()]) connectionStore.delete(m);
  vi.useRealTimers();
});

const flush = async (ms = 0) => {
  await vi.advanceTimersByTimeAsync(ms);
};

describe('rollout', () => {
  it('shows while the hub says so, then «done» for a moment, then nothing', async () => {
    hub = () => json({ active: true });
    watchRollout();
    await flush();
    expect(asked[0]).toMatch(/\/api\/v2\/rollout$/);
    expect(rolloutStore.get()).toBe(ROLLOUT.active);

    hub = () => json({ active: false });
    await flush(ACTIVE_POLL_MS);
    expect(rolloutStore.get()).toBe(ROLLOUT.done);
    await flush(DONE_MS);
    expect(rolloutStore.get()).toBe(ROLLOUT.idle);
  });

  it('waits for the machines’ links before it says «done»', async () => {
    const m = 'm1' as MachineId;
    connectionStore.set(m, { k: 'offline', retryAt: Date.now() + 3000, cause: 'network' });
    hub = () => json({ active: true });
    watchRollout();
    await flush();
    hub = () => json({ active: false });
    await flush(ACTIVE_POLL_MS);
    expect(rolloutStore.get()).toBe(ROLLOUT.active);
    connectionStore.set(m, { k: 'live', since: Date.now() });
    await flush(10); // the link that came back asks the hub at once
    expect(rolloutStore.get()).toBe(ROLLOUT.done);
  });

  it('reads a gateway without the hub behind it as a rollout, and a hub without the route as none', async () => {
    hub = () => new Response('<html>503</html>', { status: 503 });
    watchRollout();
    await flush();
    expect(rolloutStore.get()).toBe(ROLLOUT.active);
    stopRollout();

    hub = () => new Response('not found', { status: 404 });
    watchRollout();
    await flush();
    expect(rolloutStore.get()).toBe(ROLLOUT.idle);
  });

  it('asks the hub when a live link drops', async () => {
    const m = 'm1' as MachineId;
    watchRollout();
    await flush();
    connectionStore.set(m, { k: 'live', since: Date.now() });
    await flush();
    const before = asked.length;
    connectionStore.set(m, { k: 'connecting', attempt: 1 });
    await flush(2_000);
    expect(asked.length).toBeGreaterThan(before);
  });
});

describe('a request across the gap', () => {
  const answers = (...list: (Response | 'network')[]) => {
    let i = 0;
    const send = vi.fn(async () => list[Math.min(i++, list.length - 1)]);
    return send;
  };

  it('is sent again while a rollout is under way', async () => {
    hub = () => json({ active: true });
    watchRollout();
    await flush();
    const send = answers(new Response('<html>502</html>', { status: 502 }), 'network', json({ ok: true }));
    const got = held(send, 'GET', 20_000);
    await flush(HOLD_STEP_MS * 3);
    const res = await got;
    expect(send).toHaveBeenCalledTimes(3);
    expect(typeof res !== 'string' && res.status).toBe(200);
  });

  it('answers as before without a rollout', async () => {
    watchRollout();
    await flush();
    const send = answers(new Response('<html>502</html>', { status: 502 }), json({ ok: true }));
    const res = held(send, 'GET', 20_000);
    await flush(HOLD_STEP_MS);
    expect(((await res) as Response).status).toBe(502);
    expect(send).toHaveBeenCalledTimes(1);
  });

  it('sends a change again only when it surely never reached the machine', async () => {
    hub = () => json({ active: true });
    watchRollout();
    await flush();
    const cut = answers(new Response('<html>502</html>', { status: 502 }), json({ ok: true }));
    const a = held(cut, 'POST', 20_000);
    await flush(HOLD_STEP_MS);
    expect(((await a) as Response).status).toBe(502);

    const offline = answers(json({ error: 'machine_offline', message: 'x' }, 502), json({ ok: true }));
    const b = held(offline, 'POST', 20_000);
    await flush(HOLD_STEP_MS * 2);
    expect(((await b) as Response).status).toBe(200);
  });
});
