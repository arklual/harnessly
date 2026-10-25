// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ARTIFACT_KIND, type IArtifactItem } from '@/next/features/artifacts/model';
import {
  AGAIN_MS,
  PARALLEL,
  requestMetas,
  resetMetaStore,
  retryMetas,
  setMetaFetch,
} from '@/next/features/artifacts/meta-store';

const item = (n: number, patch: Partial<IArtifactItem> = {}): IArtifactItem => ({
  url: `https://docs.example.com/a/doc-${n}`,
  kind: ARTIFACT_KIND.doc,
  key: null,
  label: null,
  fallback: 'Document',
  where: `docs.example.com/a/doc-${n}`,
  github: false,
  readable: true,
  at: n,
  ...patch,
});

const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status });
const meta = (title: string) => ({ kind: 'artifact', title, description: null });

beforeEach(() => {
  vi.useFakeTimers();
  localStorage.clear();
  resetMetaStore();
});
afterEach(() => {
  setMetaFetch(null);
  vi.useRealTimers();
});

const settle = () => vi.advanceTimersByTimeAsync(10);

describe('the meta store', () => {
  it('reads at most PARALLEL links at once and in the order the rows stand', async () => {
    const asked: string[] = [];
    const release: (() => void)[] = [];
    setMetaFetch(async (url) => {
      asked.push(decodeURIComponent(url.split('url=')[1].split('&')[0]));
      await new Promise<void>((resolve) => release.push(resolve));
      return json(meta('t'));
    });
    const items = Array.from({ length: 9 }, (_, i) => item(i));
    requestMetas('m1', items);
    await settle();
    expect(asked).toHaveLength(PARALLEL);
    expect(asked[0]).toBe(items[0].url);
    release.splice(0).forEach((go) => go());
    await settle();
    expect(asked).toHaveLength(9);
    expect(asked[8]).toBe(items[8].url);
  });

  it('does not read a link twice while the first answer is fresh, and asks for what is on its way only once', async () => {
    const fetcher = vi.fn(async () => json(meta('Plan')));
    setMetaFetch(fetcher);
    const items = [item(1)];
    requestMetas('m1', items);
    requestMetas('m1', items);
    await settle();
    requestMetas('m1', items);
    await settle();
    expect(fetcher).toHaveBeenCalledTimes(1);
  });

  it('asks the machine the page is on, with the link in the query', async () => {
    const fetcher = vi.fn(async () => json(meta('Plan')));
    setMetaFetch(fetcher);
    requestMetas('m-7', [item(1, { github: true })]);
    await settle();
    const url = (fetcher.mock.calls[0] as unknown as [string])[0];
    expect(url).toContain('/github/link-meta?url=');
    expect(url).toContain('machine=m-7');
  });

  it('retries a failed read once by itself, then leaves it to «Повторить»', async () => {
    const fetcher = vi.fn(async () => new Response('', { status: 502 }));
    setMetaFetch(fetcher);
    const items = [item(1)];
    requestMetas('m1', items);
    await settle();
    expect(fetcher).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(AGAIN_MS + 20);
    expect(fetcher).toHaveBeenCalledTimes(2);
    await vi.advanceTimersByTimeAsync(AGAIN_MS * 3);
    expect(fetcher).toHaveBeenCalledTimes(2);
    requestMetas('m1', items);
    await settle();
    expect(fetcher).toHaveBeenCalledTimes(2);

    fetcher.mockImplementation(async () => json(meta('Plan')));
    retryMetas('m1', items);
    await settle();
    expect(fetcher).toHaveBeenCalledTimes(3);
  });

  it('keeps a not-found as an answer for a while and asks it again on «Повторить»', async () => {
    const fetcher = vi.fn(async () => json({ error: 'Not found' }, 404));
    setMetaFetch(fetcher);
    const items = [item(1)];
    requestMetas('m1', items);
    await settle();
    requestMetas('m1', items);
    await settle();
    expect(fetcher).toHaveBeenCalledTimes(1);
    retryMetas('m1', items);
    await settle();
    expect(fetcher).toHaveBeenCalledTimes(2);
  });

  it('keeps what it read for the next load of the page', async () => {
    setMetaFetch(async () => json(meta('Plan')));
    requestMetas('m1', [item(1)]);
    await settle();
    await vi.advanceTimersByTimeAsync(1000);
    expect(localStorage.getItem('hx-next:artifacts:meta:v1')).toContain('Plan');
  });
});
