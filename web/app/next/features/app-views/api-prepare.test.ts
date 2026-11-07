import { beforeEach, describe, expect, it, vi } from 'vitest';

const runtimeJson = vi.fn();
vi.mock('@/next/core/net/runtime-json', async (real) => ({
  ...(await real<typeof import('@/next/core/net/runtime-json')>()),
  runtimeJson: (...a: unknown[]) => runtimeJson(...a),
}));

const { createViewSession, prepareViewSession } = await import('./api');

const ask = { app: 'acme.notes', view: 'main', target: 'app', conversation: 'c1' } as const;

describe('a view session asked for before the click', () => {
  beforeEach(() => runtimeJson.mockReset());

  it('is taken by the window that mounts next, once', async () => {
    runtimeJson.mockResolvedValueOnce({ session: 's1' }).mockResolvedValueOnce({ session: 's2' });
    prepareViewSession('local', ask);
    prepareViewSession('local', ask);
    expect(runtimeJson).toHaveBeenCalledTimes(1);
    await expect(createViewSession('local', ask)).resolves.toEqual({ session: 's1' });
    await expect(createViewSession('local', ask)).resolves.toEqual({ session: 's2' });
    expect(runtimeJson).toHaveBeenCalledTimes(2);
  });

  it('is not given to another chat', async () => {
    runtimeJson.mockResolvedValueOnce({ session: 's1' }).mockResolvedValueOnce({ session: 's2' });
    prepareViewSession('local', { ...ask, app: 'acme.other' });
    await expect(
      createViewSession('local', { ...ask, app: 'acme.other', conversation: 'c2' }),
    ).resolves.toEqual({ session: 's2' });
  });

  it('that failed is asked for again at the mount', async () => {
    const other = { ...ask, conversation: 'c3' };
    runtimeJson.mockRejectedValueOnce(new Error('down')).mockResolvedValueOnce({ session: 's2' });
    prepareViewSession('local', other);
    await expect(createViewSession('local', other)).resolves.toEqual({ session: 's2' });
  });
});
