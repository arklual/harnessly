// @vitest-environment jsdom
import { act, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { IGuestConfig } from '@/next/features/designly/guest/guest-config';
import { GUEST_PDF_MAX_MS, GUEST_PDF_MS, useGuestPdf } from '@/next/features/designly/guest/use-guest-pdf';

const config: IGuestConfig = {
  v: 1,
  token: 'pdf-token',
  title: 'Deck',
  rights: 'view',
  expiresAt: null,
  view: { v: 1, pdf: true },
  boards: [],
  filesBase: '/s/pdf-token/f/',
  commentsUrl: '',
  kind: 'slides',
  deck: { video: null, pdf: false },
  mediaBase: '/s/pdf-token/m/',
  seenUrl: '/s/pdf-token/seen',
};

const status = (code: number) => vi.fn().mockResolvedValue({ ok: code < 300, status: code, body: null });
const tick = async (ms: number) => {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms);
  });
};

let reload: ReturnType<typeof vi.fn>;
beforeEach(() => {
  vi.useFakeTimers();
  reload = vi.fn();
  vi.stubGlobal('location', { ...window.location, reload });
  window.sessionStorage.clear();
});
afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe('the PDF of a guest deck', () => {
  it('is asked for less and less often while it is not built, up to every 5 min', async () => {
    const fetcher = status(404);
    vi.stubGlobal('fetch', fetcher);
    renderHook(() => useGuestPdf(config));
    await tick(GUEST_PDF_MS);
    expect(fetcher).toHaveBeenCalledTimes(1);
    await tick(GUEST_PDF_MS);
    expect(fetcher).toHaveBeenCalledTimes(1);
    await tick(GUEST_PDF_MS);
    expect(fetcher).toHaveBeenCalledTimes(2);
    // 15 s, 30 s, 60 s, 120 s, 240 s, then 300 s for good: about 20 asks an hour at most, not 240
    await tick(60 * 60_000);
    expect(fetcher.mock.calls.length).toBeLessThan(20);
    const before = fetcher.mock.calls.length;
    await tick(GUEST_PDF_MAX_MS);
    expect(fetcher.mock.calls.length).toBe(before + 1);
  });

  it('shows up once it is there', async () => {
    vi.stubGlobal('fetch', status(206));
    const { result } = renderHook(() => useGuestPdf(config));
    expect(result.current).toBeNull();
    await tick(GUEST_PDF_MS);
    expect(result.current).toBe('/s/pdf-token/m/deck.pdf');
  });

  it('is not asked for from a hidden tab', async () => {
    const fetcher = status(404);
    vi.stubGlobal('fetch', fetcher);
    const hidden = vi.spyOn(document, 'hidden', 'get').mockReturnValue(true);
    renderHook(() => useGuestPdf(config));
    await tick(10 * 60_000);
    expect(fetcher).not.toHaveBeenCalled();
    hidden.mockRestore();
  });

  it('stops being asked for when the link is revoked, and the page loads once to say so', async () => {
    const fetcher = status(410);
    vi.stubGlobal('fetch', fetcher);
    renderHook(() => useGuestPdf(config));
    await tick(GUEST_PDF_MS);
    expect(reload).toHaveBeenCalledTimes(1);
    await tick(60 * 60_000);
    expect(fetcher).toHaveBeenCalledTimes(1);
  });

  it('stops being asked for when the link no longer gives it', async () => {
    const fetcher = status(403);
    vi.stubGlobal('fetch', fetcher);
    renderHook(() => useGuestPdf(config));
    await tick(60 * 60_000);
    expect(fetcher).toHaveBeenCalledTimes(1);
    expect(reload).not.toHaveBeenCalled();
  });
});
