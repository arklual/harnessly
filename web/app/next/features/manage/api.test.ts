import { afterEach, describe, expect, it, vi } from 'vitest';
import { HubError, liveHub } from '@/next/features/manage/api';

afterEach(() => vi.unstubAllGlobals());

describe('the hub client', () => {
  it('reads a 404 as «organizations are off», not as a failure', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('', { status: 404 })));
    await expect(liveHub('https://hub.example').load()).rejects.toMatchObject({ kind: 'off' });
  });
  it('says a broken answer is a failure', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('', { status: 500 })));
    const err = await liveHub('https://hub.example')
      .load()
      .catch((e: unknown) => e);
    expect(err).toBeInstanceOf(HubError);
    expect((err as HubError).kind).toBe('failed');
  });
  it('posts the request and waits for the answer', async () => {
    const fetchMock = vi.fn().mockResolvedValue(Response.json({}));
    vi.stubGlobal('fetch', fetchMock);
    await liveHub('https://hub.example').askMore('please');
    expect(fetchMock.mock.calls[0]?.[0]).toBe('https://hub.example/api/me/limit-requests');
  });
});
