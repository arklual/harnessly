import { describe, expect, it, vi } from 'vitest';
import { gateShare } from '@/next/features/app-views/sinopia/share-gate';

const result = (blocks: boolean) => ({
  app: 'harnessly.deck-lint',
  appName: 'Валидатор презентаций',
  check: 'rules',
  findings: [{ node: 'p:1', level: 'error', message: 'Слишком много слов: 80 из 40' }],
  blocks,
});

describe('gateShare', () => {
  it('lets the share go when no check blocks it', async () => {
    const proceed = vi.fn();
    const got = await gateShare(
      async () => ({ results: [result(false)], blocked: false, marks: [] }),
      proceed,
    );
    expect(got).toEqual({ ok: true });
    expect(proceed).toHaveBeenCalledOnce();
  });

  it('stops the share a check blocks and hands back what the blocking checks found', async () => {
    const proceed = vi.fn();
    const got = await gateShare(
      async () => ({
        results: [result(true), { ...result(false), check: 'other' }],
        blocked: true,
        marks: [],
      }),
      proceed,
    );
    expect(proceed).not.toHaveBeenCalled();
    expect(got).toMatchObject({ ok: false, results: [{ check: 'rules' }] });
  });

  it('shares nothing on a guess when the runtime does not answer', async () => {
    const proceed = vi.fn();
    const got = await gateShare(async () => Promise.reject(new Error('offline')), proceed);
    expect(proceed).not.toHaveBeenCalled();
    expect(got).toEqual({ ok: false, error: 'offline' });
  });
});
