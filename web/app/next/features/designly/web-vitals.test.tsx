// @vitest-environment jsdom
import { renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { reportWebVital, resetWebVitals, useHomeVitals, WEB_VITAL } from './web-vitals';

const sent = () =>
  vi
    .mocked(fetch)
    .mock.calls.map(([url, init]) => ({ url: String(url), body: JSON.parse(String(init?.body)) }));

describe('web vitals', () => {
  beforeEach(() => {
    resetWebVitals();
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response(null, { status: 204 })),
    );
    vi.stubGlobal(
      'requestAnimationFrame',
      (cb: FrameRequestCallback) => setTimeout(() => cb(0), 0) as unknown as number,
    );
    vi.stubGlobal('cancelAnimationFrame', (id: number) => clearTimeout(id));
  });
  afterEach(() => vi.unstubAllGlobals());

  it('posts a name and seconds to the cluster path', () => {
    reportWebVital(WEB_VITAL.lcp, 1.23456);
    expect(sent()).toEqual([
      { url: expect.stringMatching(/\/api\/v2\/web-vitals$/), body: { name: 'lcp', seconds: 1.235 } },
    ]);
  });

  it('sends no time that is not one', () => {
    reportWebVital(WEB_VITAL.lcp, 0);
    reportWebVital(WEB_VITAL.lcp, NaN);
    expect(fetch).not.toHaveBeenCalled();
  });

  it('reports the wait of a home that was loading, once', async () => {
    const { rerender } = renderHook(({ ready }) => useHomeVitals(ready), { initialProps: { ready: false } });
    rerender({ ready: true });
    await vi.waitFor(() => expect(sent().some((s) => s.body.name === 'sinopia_home_full')).toBe(true));
    rerender({ ready: false });
    rerender({ ready: true });
    await new Promise((r) => setTimeout(r, 20));
    expect(sent().filter((s) => s.body.name === 'sinopia_home_full')).toHaveLength(1);
  });

  it('does not count a home that mounted with its cards', async () => {
    renderHook(() => useHomeVitals(true));
    await new Promise((r) => setTimeout(r, 20));
    expect(fetch).not.toHaveBeenCalled();
  });
});
