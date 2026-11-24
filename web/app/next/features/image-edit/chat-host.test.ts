import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { pinLocale } from '@/next/i18n';
import { chatImageHost } from './chat-host';
import type { IImageEdit } from './host';

beforeAll(() => pinLocale('en'));
afterEach(() => vi.unstubAllGlobals());

const host = chatImageHost({ machine: 'local', conv: 'tab-chat' });
const edit: IImageEdit = { from: { id: '/a.png', src: '/a' }, mode: 'text', prompt: 'night' };

describe('chat image provider', () => {
  it.each(['codex', 'antigravity'] as const)(
    'passes the selected %s provider to the existing image endpoint',
    async (provider) => {
      const fetcher = vi
        .fn()
        .mockResolvedValue(new Response(JSON.stringify({ id: 'job', state: 'running' })));
      vi.stubGlobal('fetch', fetcher);
      expect(await host.start({ ...edit, provider })).toBe('job');
      const request = fetcher.mock.calls[0][1] as RequestInit;
      expect(JSON.parse(String(request.body))).toMatchObject({
        provider,
        source: '/a.png',
        mode: 'text',
        conv: 'tab-chat',
      });
    },
  );

  it('keeps Codex as the default for older callers', async () => {
    const fetcher = vi.fn().mockResolvedValue(new Response(JSON.stringify({ id: 'job', state: 'running' })));
    vi.stubGlobal('fetch', fetcher);
    await host.start(edit);
    expect(JSON.parse(String(fetcher.mock.calls[0][1].body)).provider).toBe('codex');
  });

  it('refuses Antigravity background removal without sending a different provider', async () => {
    const fetcher = vi.fn();
    vi.stubGlobal('fetch', fetcher);
    await expect(host.start({ ...edit, provider: 'antigravity', mode: 'cut' })).rejects.toThrow(
      'transparent',
    );
    await expect(host.start({ ...edit, provider: 'higgsfield', mode: 'cut' })).rejects.toThrow('Higgsfield');
    expect(fetcher).not.toHaveBeenCalled();
  });

  it('sends Higgsfield with its model', async () => {
    const fetcher = vi.fn().mockResolvedValue(new Response(JSON.stringify({ id: 'job', state: 'running' })));
    vi.stubGlobal('fetch', fetcher);
    await host.start({ ...edit, provider: 'higgsfield', model: 'nano_banana_pro' });
    const body = JSON.parse(String(fetcher.mock.calls[0][1].body));
    expect(body.provider).toBe('higgsfield');
    expect(body.model).toBe('nano_banana_pro');
  });

  it('keeps provider provenance from the snapshot', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            id: 'job',
            state: 'done',
            provider: 'antigravity',
            images: [{ path: '/b.png' }],
          }),
        ),
      ),
    );
    const progress = await host.read('job', new AbortController().signal);
    expect(progress.versions[0]).toMatchObject({ id: '/b.png', provider: 'antigravity' });
  });
});
