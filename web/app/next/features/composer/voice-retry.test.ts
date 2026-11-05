import { beforeEach, describe, expect, it, vi } from 'vitest';
import { OUTCOME, recognizeTake, RETRY, retryStore } from '@/next/features/composer/voice-retry';
import { failTake, recordTake, startTake, takeIn, takesStore } from '@/next/features/composer/voice-takes';
import { meStore } from '@/next/stores/me';
import type { MachineId } from '@/next/protocol/v2';

vi.mock('@/next/features/composer/voice-cloud', () => ({
  putCloudTake: vi.fn(async () => true),
  patchCloudTake: vi.fn(async () => true),
  deleteCloudTake: vi.fn(async () => true),
}));

const M = 'm' as MachineId;

const failed = (scope: string | null) => {
  const take = startTake(M, 'audio/webm', scope);
  recordTake(take.id, [new Blob(['speech'], { type: 'audio/webm' })]);
  failTake(take.id);
  return take.id;
};

const deferred = <T>() => {
  let resolve: (v: T) => void = () => {};
  let reject: (e: unknown) => void = () => {};
  const promise = new Promise<T>((ok, no) => {
    resolve = ok;
    reject = no;
  });
  return { promise, resolve, reject };
};

beforeEach(() => {
  meStore.set({ k: 'ready', me: { email: 'a@test' } } as never);
});

describe('a failed take recognized again (HRN-674)', () => {
  it('joins the request already running instead of doing nothing, and is busy until it ends', async () => {
    const id = failed('conv-1');
    const answer = deferred<string>();
    const run = vi.fn(() => answer.promise);
    const onSaid = vi.fn();
    const auto = recognizeTake(id, run, onSaid, String);
    const press = recognizeTake(id, run, onSaid, String);
    expect(run).toHaveBeenCalledTimes(1);
    expect(retryStore.get(id)).toEqual({ k: RETRY.busy });

    answer.resolve(' привет ');
    expect(await press).toEqual({ k: OUTCOME.said, text: 'привет' });
    expect(await auto).toEqual({ k: OUTCOME.said, text: 'привет' });
    expect(onSaid).toHaveBeenCalledTimes(1);
    expect(onSaid).toHaveBeenCalledWith('привет');
    expect(retryStore.get(id)).toBeUndefined();
    expect(takesStore.get(id)?.state).toBe('done');
  });

  it('keeps the take with the reason when the machine could not, and tries again on the next press', async () => {
    const id = failed('conv-1');
    const outcome = await recognizeTake(
      id,
      async () => {
        throw new Error('timeout');
      },
      vi.fn(),
      () => 'Машина не ответила вовремя',
    );
    expect(outcome).toEqual({ k: OUTCOME.failed, reason: 'Машина не ответила вовремя' });
    expect(retryStore.get(id)).toEqual({ k: RETRY.failed, reason: 'Машина не ответила вовремя' });
    expect(takesStore.get(id)?.state).toBe('failed');

    const again = vi.fn(async () => 'текст');
    await recognizeTake(id, again, vi.fn(), String);
    expect(again).toHaveBeenCalledTimes(1);
    expect(retryStore.get(id)).toBeUndefined();
  });

  it('drops a take that holds only silence', async () => {
    const id = failed('conv-1');
    expect(await recognizeTake(id, async () => '  ', vi.fn(), String)).toEqual({ k: OUTCOME.silence });
    expect(takesStore.has(id)).toBe(false);
  });
});

describe('where a failed take waits (HRN-674)', () => {
  it('only over the field it was recorded in', () => {
    const take = takesStore.get(failed('conv-1'));
    if (!take) throw new Error('no take');
    expect(takeIn(take, 'conv-1', 'new')).toBe(true);
    expect(takeIn(take, 'conv-2', 'new')).toBe(false);
    expect(takeIn(take, 'new', 'new')).toBe(false);
    expect(takeIn(take, null, 'new')).toBe(false);
  });

  it('a take of the start page over the start page only', () => {
    const take = takesStore.get(failed('new'));
    if (!take) throw new Error('no take');
    expect(takeIn(take, 'new', 'new')).toBe(true);
    expect(takeIn(take, 'conv-1', 'new')).toBe(false);
  });

  it('a take kept before takes had a field over the start page, not over every chat', () => {
    const take = takesStore.get(failed('conv-1'));
    if (!take) throw new Error('no take');
    const { scope: _scope, ...legacy } = take;
    expect(takeIn(legacy, 'new', 'new')).toBe(true);
    expect(takeIn(legacy, 'conv-1', 'new')).toBe(false);
  });

  it('a field without a scope records takes that wait nowhere (they are in «Записи»)', () => {
    const take = takesStore.get(failed(null));
    if (!take) throw new Error('no take');
    expect(takeIn(take, 'new', 'new')).toBe(false);
    expect(takeIn(take, 'conv-1', 'new')).toBe(false);
  });
});
