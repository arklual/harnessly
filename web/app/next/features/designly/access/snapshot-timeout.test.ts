import { afterEach, expect, it, vi } from 'vitest';
import { createLink, type ILinkInput } from './resource-api';

const design = { kind: 'design', id: 'prototype' } as const;
const input: ILinkInput = {
  forWhom: 'Review',
  rights: 'view',
  ttl: '1d',
  options: null,
  version: 'pinned',
  pages: null,
};

afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

it('waits for static publication while the ordinary share request times out', async () => {
  vi.useFakeTimers();
  vi.stubGlobal(
    'fetch',
    vi.fn(
      (_path: string, { signal }: RequestInit) =>
        new Promise<Response>((resolve, reject) => {
          const timer = setTimeout(
            () =>
              resolve(
                Response.json({ link: { id: 'share', forWhom: 'Review' }, url: '/guest/design/snapshot' }),
              ),
            20_000,
          );
          signal?.addEventListener('abort', () => {
            clearTimeout(timer);
            reject(new DOMException('Aborted', 'AbortError'));
          });
        }),
    ),
  );
  const ordinary = expect(createLink(design, input)).rejects.toMatchObject({ code: 'network' });
  const captured = createLink(design, input, true);
  await vi.advanceTimersByTimeAsync(20_000);
  await ordinary;
  await expect(captured).resolves.toMatchObject({ url: '/guest/design/snapshot' });
});
