import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { loadBootstrap, type BootBody } from '@/lib/boot-start';

const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json' } });

const memory = (init: Record<string, string> = {}) => {
  const m = new Map(Object.entries(init));
  return { getItem: (k: string) => m.get(k) ?? null, setItem: (k: string, v: string) => void m.set(k, v), m };
};

/** A request that never answers until its signal is aborted, like a hung machine behind the hub. */
const hung = (signals: AbortSignal[]) => (signal: AbortSignal) =>
  new Promise<Response>((_, reject) => {
    signals.push(signal);
    signal.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')));
  });

describe('loadBootstrap', () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  it('draws once with the start data when the machine answers in time', async () => {
    const data: [BootBody, boolean][] = [];
    const storage = memory();
    loadBootstrap(
      async () => json({ owner: 'ann@example.com', initialConfig: {} }),
      {
        onData: (b, m) => data.push([b, m]),
        onSignedOut: () => undefined,
      },
      { storage },
    );
    await vi.advanceTimersByTimeAsync(5000);
    expect(data).toEqual([[{ owner: 'ann@example.com', initialConfig: {} }, false]]);
    expect(storage.m.get('hl-boot-owner')).toBe('ann@example.com');
  });

  it('draws with the owner seen last while a hung machine says nothing, and cuts its request off', async () => {
    const data: [BootBody, boolean][] = [];
    const signals: AbortSignal[] = [];
    loadBootstrap(
      hung(signals),
      { onData: (b, m) => data.push([b, m]), onSignedOut: () => undefined },
      {
        storage: memory({ 'hl-boot-owner': 'ann@example.com' }),
        waitMs: 1500,
        fetchMs: 20_000,
        retryMs: [1000],
      },
    );
    await vi.advanceTimersByTimeAsync(1499);
    expect(data).toEqual([]);
    await vi.advanceTimersByTimeAsync(1);
    expect(data).toEqual([[{ owner: 'ann@example.com' }, true]]);
    expect(signals[0]?.aborted).toBe(false);
    // The request is not held for ever: it is cut off and asked again after a pause.
    await vi.advanceTimersByTimeAsync(18_500);
    expect(signals[0]?.aborted).toBe(true);
    await vi.advanceTimersByTimeAsync(1000);
    expect(signals).toHaveLength(2);
    expect(data).toHaveLength(1);
  });

  it('takes the start data when the machine answers late', async () => {
    const data: [BootBody, boolean][] = [];
    let calls = 0;
    loadBootstrap(
      async () => {
        calls += 1;
        return calls === 1 ? new Response('', { status: 504 }) : json({ owner: 'ann@example.com' });
      },
      { onData: (b, m) => data.push([b, m]), onSignedOut: () => undefined },
      { storage: memory(), waitMs: 1500, retryMs: [3000] },
    );
    await vi.advanceTimersByTimeAsync(1500);
    expect(data).toEqual([[{ owner: '' }, true]]);
    await vi.advanceTimersByTimeAsync(1500);
    expect(data).toEqual([
      [{ owner: '' }, true],
      [{ owner: 'ann@example.com' }, false],
    ]);
    // Answered: nothing is asked again.
    await vi.advanceTimersByTimeAsync(60_000);
    expect(calls).toBe(2);
  });

  it('asks again with growing pauses', async () => {
    const at: number[] = [];
    loadBootstrap(
      async () => {
        at.push(Date.now());
        return new Response('', { status: 503 });
      },
      { onData: () => undefined, onSignedOut: () => undefined },
      { storage: null, retryMs: [1000, 2000, 5000] },
    );
    const start = Date.now();
    await vi.advanceTimersByTimeAsync(13_000);
    expect(at.map((t) => t - start)).toEqual([0, 1000, 3000, 8000, 13_000]);
  });

  it('hands a 401 or a redirect to sign-in over and draws nothing', async () => {
    const data: unknown[] = [];
    const out: number[] = [];
    loadBootstrap(
      async () => new Response('', { status: 401 }),
      {
        onData: (b) => data.push(b),
        onSignedOut: (res) => out.push(res.status),
      },
      { storage: null },
    );
    await vi.advanceTimersByTimeAsync(5000);
    expect(out).toEqual([401]);
    expect(data).toEqual([]);
  });

  it('stops: no draw, and the open request is cancelled', async () => {
    const data: unknown[] = [];
    const signals: AbortSignal[] = [];
    const stop = loadBootstrap(
      hung(signals),
      { onData: (b) => data.push(b), onSignedOut: () => undefined },
      {
        storage: null,
      },
    );
    await vi.advanceTimersByTimeAsync(100);
    stop();
    expect(signals[0]?.aborted).toBe(true);
    await vi.advanceTimersByTimeAsync(60_000);
    expect(data).toEqual([]);
    expect(signals).toHaveLength(1);
  });
});
